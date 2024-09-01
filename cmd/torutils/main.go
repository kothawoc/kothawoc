package main

import (
	"fmt"
	"os"
	"time"

	"github.com/cretz/bine/torutil/ed25519"

	"github.com/kothawoc/kothawoc/internal/torutils"
)

func authCB(s ed25519.PublicKey) bool {
	return true
}

func server(tc *torutils.TorCon, key ed25519.PrivateKey) {
	onion, _ := tc.Listen(80, 9980, key)

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

	key := torutils.CreatePrivateKey()
	//	torutils.Main()
	tc := torutils.NewTorCon(os.Getenv("PWD") + "/data/tor-data")

	fmt.Printf("TC Tor connected: [%v]\n", tc)

	go server(tc, key)

	<-time.After(time.Second * 10)

	fmt.Printf("Starting Client\n")
	for {

		fmt.Printf("CLIENT Dialing\n")
		conn, err := tc.Dial("tcp", "addxb2stt45nwbcv64aglgpz5r65m4ljgzp7mdelrt3oxhej3uykmdid.onion:80")

		fmt.Printf("CLIENT Dialing response [%v][%v]\n", conn, err)
		if err != nil {
			fmt.Printf("Error Dialer connect: [%v]\n", err)
			return
		}

		authed, err := tc.ClientHandshake(conn, key, "addxb2stt45nwbcv64aglgpz5r65m4ljgzp7mdelrt3oxhej3uykmdid")
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
