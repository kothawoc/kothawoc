package torutils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	//"github.com/cretz/bine/process/embedded/tor-0.4.7"
	"github.com/cretz/bine/tor"
	"github.com/cretz/bine/torutil/ed25519"

	"github.com/kothawoc/kothawoc/pkg/keytool"
	serr "github.com/kothawoc/kothawoc/pkg/serror"
)

func randomHexString(n int) string {
	rMesg := make([]byte, n)
	rand.Read(rMesg)
	//binary.LittleEndian.
	return hex.EncodeToString(rMesg)
}

func readConnUntilLF(conn net.Conn) (string, error) {
	var tbuf []byte = make([]byte, 1)
	rets := ""

	//log.Printf("RTL Started\n")
	for {
		n, err := conn.Read(tbuf)
		//log.Printf("RTLF: [%n][%d][%s]\n", n, tbuf[0], string(tbuf[0]))
		if err != nil {
			slog.Info("RTL error", "error", err, "size", n, "rets", rets)
			return rets, err
		}
		if tbuf[0] == '\n' {
			//log.Printf("RTL return %d, [%s]\n", len(rets), rets)
			return rets, nil
		} else {
			rets += string(tbuf)
		}
		if len(rets) > 1024 {
			return rets, serr.Errorf("READ CONN UNTIL LF OVERSIZE [%d]", len(rets))
		}
	}
}

var Tor *tor.Tor

type TorCon struct {
	t      *tor.Tor
	dialer *tor.Dialer
}

// C > {public key hex} {tor id} {random hex string 64 bytes long} {signature}\n
// S    - check if the tor ID is allowed to connect, drop connection if not
// S    - otherwise verify the signature, and send the next message.
// S    - create signature of: {server message} + " " + {client message}
// S > {public key hex} {tor id} {random hex string 64 bytes long} {special signature}\n
// C    - verify the server message, drop if it's not who you thought.
// C > {hex sign server message}\n
// S    - verify client signature, drop connection if falty
// S > {OK}\n

// WARNING TODO: make sure pubkey lengths are correct or it will panic,
// WARNING TODO: verify public keys match tor id hash.
func (t *TorCon) ClientHandshake(conn net.Conn, privateKey ed25519.PrivateKey, remoteAddr string) (ed25519.PublicKey, error) {
	// construct initial handshake
	hexPublicKey := hex.EncodeToString([]byte(privateKey.PublicKey()))
	//log.Printf("Client public key: size[%d] content[%s]", len([]byte(ed25519.PublicKey(privateKey))), []byte(ed25519.PublicKey(privateKey)))

	//torId := EncodePublicKey([]byte(privateKey.PublicKey()))

	myKey := keytool.EasyEdKey{}
	myKey.SetTorPrivateKey(ed25519.PrivateKey(privateKey))
	//torId, err := keytool.EncodePublicKey([]byte(privateKey.PublicKey()))
	torId, err := myKey.TorId()
	initialHandshake := hexPublicKey + " " + torId + " " + randomHexString(32)
	initialHandshake += " " + hex.EncodeToString(ed25519.Sign(privateKey, []byte(initialHandshake))) + "\n"
	//log.Printf("CLIENT HANDSHAKE SEND TO SERVER: ", initialHandshake)
	conn.Write([]byte(initialHandshake))

	// get response
	response, err := readConnUntilLF(conn)
	if err != nil {
		return nil, err
	}
	splitResponse := strings.Split(response, " ")
	serverPubKey, _ := hex.DecodeString(string(splitResponse[0]))
	serverSig, _ := hex.DecodeString(string(splitResponse[3]))
	serverMesg := strings.Join(splitResponse[:3], " ") + " " + initialHandshake[:len(initialHandshake)-1]

	//log.Printf("CLIENT SPECIAL MESSAGE VERSION [%s]\n", serverMesg)

	verified := ed25519.Verify(serverPubKey, []byte(serverMesg), serverSig)
	//log.Printf("CLIENT HANDSHAKE VERIFY RESPONSE [%v] RESPONSE: [%s]\n", verified, response)
	if !verified {
		return nil, fmt.Errorf("faied to verify server cert")
	}

	// sign server message so they can trust you.
	signedServerMesg := string(hex.EncodeToString(ed25519.Sign(privateKey, []byte(response)))) + "\n"
	//log.Printf("CLIENT HANDSHAKE SEND TO SIGNATURE SERVER: %s", signedServerMesg)
	conn.Write([]byte(signedServerMesg))

	// wait for OK
	response, err = readConnUntilLF(conn)
	//log.Printf("CLIENT HANDSHAKE OK/FAIL from server: [%s]\n", response)
	if response == "OK" {
		return serverPubKey, nil
	}
	return nil, serr.Errorf("Error: server refused connection.")
}

const (
	Ed25519privateKeySize int = ed25519.PrivateKeySize
	Ed25519publicKeySize  int = ed25519.PublicKeySize
	Ed25519signatureSize  int = ed25519.SignatureSize
)

func (t *TorCon) ServerHandshake(conn net.Conn, privateKey ed25519.PrivateKey, authCallback func(clientPubKey ed25519.PublicKey) bool) (ed25519.PublicKey, error) {
	// construct initial handshake

	// get initial client request
	clientRequest, err := readConnUntilLF(conn)
	//log.Printf("SERVER HANDSHAKE RECEIVED FROM CLIENT: [%s]\n", clientRequest)
	if err != nil {
		return nil, err
	}
	splitRequest := strings.Split(clientRequest, " ")
	if len(splitRequest) != 4 {
		return nil, serr.Errorf("Error, handshake has wrong number of arguments.")
	}
	clientPubKey, _ := hex.DecodeString(string(splitRequest[0]))
	//fmt.Printf("SERVER PUBKEYRECV: size[%d], hex[%s] decoded[%v]\n", len(splitRequest[0]), splitRequest[0], clientPubKey)
	clientTorId := string(splitRequest[1])

	cliKey := keytool.EasyEdKey{}
	cliKey.SetTorPublicKey(clientPubKey)
	keyTorId, _ := cliKey.TorId()
	if clientTorId != keyTorId {
		return nil, serr.Errorf("Error: client TorId and pubkey don't match.")
	}
	// randomData, _ := hex.DecodeString(string(splitRequest[2]))
	clientSig, _ := hex.DecodeString(string(splitRequest[3]))
	clientMesg := strings.Join(splitRequest[:3], " ")
	// check that the claimed client tor id matches the public key
	if !authCallback(clientPubKey) {
		slog.Info("SERVER HANDSHAKE AUTH CALLBACK FAILED", "clientMesg", clientMesg)
		return nil, serr.Errorf("Error: client TorId refused by callback.")
	}
	verified := ed25519.Verify(ed25519.PublicKey(clientPubKey), []byte(clientMesg), clientSig)
	if !verified {
		slog.Info("SERVER HANDSHAKE AUTH SIGNATURE FAILED", "clientMesg", clientMesg)
		return nil, serr.Errorf("Error: failed to verify client cert.")
	}

	// send response to client
	hexPublicKey := hex.EncodeToString([]byte(privateKey.PublicKey()))

	myKey := keytool.EasyEdKey{}
	myKey.SetTorPrivateKey(privateKey.PrivateKey())
	torId, _ := cliKey.TorId()
	//torId := EncodePublicKey([]byte(privateKey.PrivateKey()))
	initialHandshake := hexPublicKey + " " + torId + " " + randomHexString(32)
	serverSpecialMesg := initialHandshake + " " + clientRequest
	//log.Printf("SERVER SPECIAL MESSAGE VERSION [%s]\n", serverSpecialMesg)
	specialSignature := hex.EncodeToString(ed25519.Sign(privateKey, []byte(serverSpecialMesg)))
	initialHandshake += " " + string(specialSignature) + "\n"

	//log.Printf("SERVER HANDSHAKE SEND RESPONSE TO CLIENT: %s\n", initialHandshake)
	conn.Write([]byte(initialHandshake))

	// get final signature from client
	clientRequest, err = readConnUntilLF(conn)
	//log.Printf("SERVER HANDSHAKE RESPONSE FROM CLIENT: [%s]\n", clientRequest)
	if err != nil {
		return nil, serr.New(err)
	}
	clientSig, _ = hex.DecodeString(clientRequest)
	verified = ed25519.Verify(clientPubKey, []byte(initialHandshake[:len(initialHandshake)-1]), clientSig)
	if verified {
		conn.Write([]byte("OK\n"))
		slog.Info("Error: faied to verify server cert.")
		return clientPubKey, nil
	}
	return nil, serr.Errorf("Error: failed to verify server cert.")
}

func (t *TorCon) Listen(torPort int, privateKey ed25519.PrivateKey) (*tor.OnionService, error) {

	// Wait at most a few minutes to publish the service
	listenCtx, _ := context.WithTimeout(context.Background(), 3*time.Minute)
	// Create an onion service to listen on 8080 but show as 80
	// localKey := getPrivateKey()
	onion, err := t.t.Listen(listenCtx, &tor.ListenConf{Version3: true, Key: privateKey, RemotePorts: []int{torPort}})
	//onion, err := t.t.Listen(listenCtx, &tor.ListenConf{Version3: true, LocalPort: localPort, Key: privateKey, RemotePorts: []int{torPort}})
	//onion, err := t.t.Listen(listenCtx, &tor.ListenConf{LocalPort: localPort, Key: privateKey, RemotePorts: []int{torPort}})
	// onion.
	if err != nil {
		return nil, err
	}
	//defer onion.Close()

	//	log.Printf("Listening\n", onion.ID)
	return onion, nil
}

func (t *TorCon) Dial(proto, remote string) (net.Conn, error) {
	if t.dialer != nil {
		conn, err := t.dialer.Dial(proto, remote)
		return conn, serr.New(err)
	} else {
		return nil, serr.New(net.ErrClosed)
	}
}

func NewTorCon(datadir string) *TorCon {

	//	log.Printf("pklen size [%d] [%s]", len(key.PublicKey()), key.PublicKey())

	//panic("poop")
	//t, err := tor.Start(nil, &tor.StartConf{DataDir: datadir, ProcessCreator: tor047.NewCreator()})
	t, err := tor.Start(context.Background(), &tor.StartConf{DataDir: datadir})
	if err != nil {
		//panic(err)
		slog.Info("Tor start Error", "error", err)
		return nil
	}

	tc := &TorCon{
		t: t,
	}

	//	<-time.After(time.Second)

	//	log.Printf("Started dialing up")

	go func() {
		time.Sleep(time.Second * 3)
		dialCtx, _ := context.WithTimeout(context.Background(), time.Minute)

		// Make connection
		dialer, err := tc.t.Dialer(dialCtx, nil)
		if err != nil {
			slog.Info("Error Dialer setup", "error", err)
			//return nil
			return
		}

		tc.dialer = dialer
	}()

	return tc

}

/*
func Sign(privateKey ed25519.PrivateKey, data []byte) []byte {
	return ed25519.Sign(privateKey, data)
}

/*
func Verify(publicKey ed25519.PublicKey, signature, data []byte) bool {
	return ed25519.Verify(publicKey, data, signature)
}
*/
