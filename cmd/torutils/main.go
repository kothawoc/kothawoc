package main

import (
	"fmt"
	"os"

	"github.com/cretz/bine/torutil/ed25519"

	"github.com/kothawoc/kothawoc/internal/torutils"
	"github.com/kothawoc/kothawoc/pkg/keytool"
)

func authCB(s ed25519.PublicKey) bool {
	return true
}

func server(tc *torutils.TorCon, key ed25519.PrivateKey, addrChan chan<- string) {
	onion, _ := tc.Listen(80, key)
	addrChan <- onion.ID
	close(addrChan)

	fmt.Printf("SERVER Listening: [%v]\n", onion)
	//defer listenCancel()
	for {
		conn, err := onion.Accept()
		fmt.Printf("SERVER Accept: [%v]\n", onion)
		if err != nil {
			fmt.Printf("SERVER ERROR Accept: [%v] [%v]\n", onion, err)
			continue
		}

		authed, err := tc.ServerHandshake(conn, key, authCB)
		fmt.Printf("SERVER AUTHed: [%v] [%v]\n", authed, err)
		if err != nil {
			continue
		}
		if authed == nil {
			conn.Close()
			continue
		}

		var buf []byte = make([]byte, 1024)
		n, _ := conn.Read(buf)
		fmt.Printf("SERVER received from client: [%s]\n", buf[:n])

		buf = append([]byte("Ponged: "), buf[:n]...)
		conn.Write(buf)

		conn.Close()
	}

}

func main() {

	//key := torutils.CreatePrivateKey()
	kt := keytool.EasyEdKey{}
	kt.GenerateKey()
	key, _ := kt.TorPrivKey()
	//	torutils.Main()
	tc := torutils.NewTorCon(os.Getenv("PWD") + "/data/tor-data")

	fmt.Printf("TC Tor connected: [%v]\n", tc)

	addrChan := make(chan string)
	go server(tc, key, addrChan)
	address := <-addrChan

	fmt.Printf("Starting Client\n")
	for {

		fmt.Printf("CLIENT Dialing\n")
		conn, err := tc.Dial("tcp", address+".onion:80")

		fmt.Printf("CLIENT Dialing response [%v][%v]\n", conn, err)
		if err != nil {
			fmt.Printf("Error Dialer connect: [%v]\n", err)
			return
		}

		authed, err := tc.ClientHandshake(conn, kt, address)
		fmt.Printf("CLIENT Authed response [%v][%v]\n", authed, err)
		if err != nil {
			fmt.Printf("CLIENT Error Dialer connect: [%v]\n", err)
			return
		}
		if authed == nil {
			conn.Close()
			fmt.Printf("CLIENT: Failed to handshake.\n")
			continue
		}

		conn.Write([]byte("ping"))
		var buf []byte = make([]byte, 1024)
		n, _ := conn.Read(buf)
		fmt.Printf("Got reply from server: [%s]\n", buf[:n])

		conn.Close()

	}

}
