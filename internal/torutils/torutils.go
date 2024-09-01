package torutils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	//"github.com/cretz/bine/process/embedded/tor-0.4.7"
	"github.com/cretz/bine/tor"
	"github.com/cretz/bine/torutil/ed25519"
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
			log.Printf("RTL error [%v] size[%d], [%s]\n", err, n, rets)
			return rets, err
		}
		if tbuf[0] == '\n' {
			//log.Printf("RTL return %d, [%s]\n", len(rets), rets)
			return rets, nil
		} else {
			rets += string(tbuf)
		}
		if len(rets) > 1024 {
			return rets, fmt.Errorf("READ CONN UNTIL LF OVERSIZE [%d]", len(rets))
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
	torId := EncodePublicKey([]byte(privateKey.PublicKey()))
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
	if verified == false {
		return nil, fmt.Errorf("Error: faied to verify server cert.")
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
	return nil, fmt.Errorf("Error: server refused connection.")
}

const (
	Ed25519privateKeySize int = ed25519.PrivateKeySize
	Ed25519publicKeySize  int = ed25519.PublicKeySize
	Ed25519signatureSize  int = ed25519.SignatureSize
)

/*

https://github.com/akamensky/golang-upgrade-tcp-to-tls/blob/master/client.go

func generatePrivateKey() (*ecdsa.PrivateKey, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return privateKey, nil
}

func generateSelfSignedCertificate(privateKey *ecdsa.PrivateKey) (*x509.Certificate, error) {
	// Create a certificate template
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(0, 0, 365), // 1-year validity
		Subject:      pkix.Name{Organization: []string{"Your Organization"}},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
	}

	// Sign the certificate with the private key
	certBytes, err := x509.CreateCertificate(rand.Reader, template, privateKey)
	if err != nil {
		return nil, err
	}

	// Parse the generated certificate
	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, err
	}
	return cert, nil
}
func encode(privateKey *ecdsa.PrivateKey, publicKey *ecdsa.PublicKey) (string, string) {
	x509Encoded, _ := x509.MarshalECPrivateKey(privateKey)
	pemEncoded := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: x509Encoded})

	x509EncodedPub, _ := x509.MarshalPKIXPublicKey(publicKey)
	pemEncodedPub := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: x509EncodedPub})

	return string(pemEncoded), string(pemEncodedPub)
}

func decode(pemEncoded string, pemEncodedPub string) (*ecdsa.PrivateKey, *ecdsa.PublicKey) {
	block, _ := pem.Decode([]byte(pemEncoded))
	x509Encoded := block.Bytes
	privateKey, _ := x509.ParseECPrivateKey(x509Encoded)

	blockPub, _ := pem.Decode([]byte(pemEncodedPub))
	x509EncodedPub := blockPub.Bytes
	genericPublicKey, _ := x509.ParsePKIXPublicKey(x509EncodedPub)
	publicKey := genericPublicKey.(*ecdsa.PublicKey)

	return privateKey, publicKey
}

func test() {
	privateKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	publicKey := &privateKey.PublicKey

	encPriv, encPub := encode(privateKey, publicKey)

	fmt.Println(encPriv)
	fmt.Println(encPub)

	priv2, pub2 := decode(encPriv, encPub)

	if !reflect.DeepEqual(privateKey, priv2) {
		fmt.Println("Private keys do not match.")
	}
	if !reflect.DeepEqual(publicKey, pub2) {
		fmt.Println("Public keys do not match.")
	}
}
func secondmain() {
	// Assume privateKey and cert are generated using the above functions
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{cert.Raw},
			PrivateKEY:  privateKey.Der(),
		}},
	}
	http.ListenAndServeTLS(":8443", cert.Raw, privateKey.Der(), nil)
}

*/

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
		return nil, fmt.Errorf("Error, handshake has wrong number of arguments.")
	}
	clientPubKey, _ := hex.DecodeString(string(splitRequest[0]))
	//fmt.Printf("SERVER PUBKEYRECV: size[%d], hex[%s] decoded[%v]\n", len(splitRequest[0]), splitRequest[0], clientPubKey)
	clientTorId := string(splitRequest[1])
	if clientTorId != EncodePublicKey(clientPubKey) {
		return nil, fmt.Errorf("Error: client TorId and pubkey don't match.")
	}
	// randomData, _ := hex.DecodeString(string(splitRequest[2]))
	clientSig, _ := hex.DecodeString(string(splitRequest[3]))
	clientMesg := strings.Join(splitRequest[:3], " ")
	// check that the claimed client tor id matches the public key
	if authCallback(clientPubKey) == false {
		log.Printf("SERVER HANDSHAKE AUTH CALLBACK FAILED [%s]\n", clientMesg)
		return nil, fmt.Errorf("Error: client TorId refused by callback.")
	}
	verified := ed25519.Verify(ed25519.PublicKey(clientPubKey), []byte(clientMesg), clientSig)
	if verified == false {
		log.Printf("SERVER HANDSHAKE AUTH SIGNATURE FAILED [%s]\n", clientMesg)
		return nil, fmt.Errorf("Error: failed to verify client cert.")
	}

	// send response to client
	hexPublicKey := hex.EncodeToString([]byte(privateKey.PublicKey()))
	torId := EncodePublicKey([]byte(privateKey.PrivateKey()))
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
		return nil, err
	}
	clientSig, _ = hex.DecodeString(clientRequest)
	verified = ed25519.Verify(clientPubKey, []byte(initialHandshake[:len(initialHandshake)-1]), clientSig)
	if verified {
		conn.Write([]byte("OK\n"))
		log.Printf("Error: faied to verify server cert.")
		return clientPubKey, nil
	}
	return nil, fmt.Errorf("Error: failed to verify server cert.")
}

func (t *TorCon) Listen(torPort, localPort int, privateKey ed25519.PrivateKey) (*tor.OnionService, error) {

	// Wait at most a few minutes to publish the service
	listenCtx, _ := context.WithTimeout(context.Background(), 3*time.Minute)
	// Create an onion service to listen on 8080 but show as 80
	// localKey := getPrivateKey()
	onion, err := t.t.Listen(listenCtx, &tor.ListenConf{Version3: true, LocalPort: localPort, Key: privateKey, RemotePorts: []int{torPort}})
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
	conn, err := t.dialer.Dial(proto, remote)
	return conn, err
}

func NewTorCon(datadir string) *TorCon {

	//	log.Printf("pklen size [%d] [%s]", len(key.PublicKey()), key.PublicKey())

	//panic("poop")
	//t, err := tor.Start(nil, &tor.StartConf{DataDir: datadir, ProcessCreator: tor047.NewCreator()})
	t, err := tor.Start(nil, &tor.StartConf{DataDir: datadir})
	if err != nil {
		//panic(err)
		log.Printf("Tor start Error: [%v]", err)
		return nil
	}

	tc := &TorCon{
		t: t,
	}

	<-time.After(time.Second)

	//	log.Printf("Started dialing up")

	dialCtx, _ := context.WithTimeout(context.Background(), time.Minute)

	// Make connection
	dialer, err := tc.t.Dialer(dialCtx, nil)
	if err != nil {
		log.Printf("Error Dialer setup: [%v]", err)
		return nil
	}

	tc.dialer = dialer

	return tc

}
