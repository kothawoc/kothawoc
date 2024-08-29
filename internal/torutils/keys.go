package torutils

import (
	"bytes"
	"encoding/base32"
	"encoding/hex"
	"log"
	"os"
	"strings"

	"github.com/cretz/bine/torutil/ed25519"
	"golang.org/x/crypto/sha3"
)

func CreatePrivateKey() ed25519.PrivateKey {

	var returnKey ed25519.PrivateKey
	bpk, _ := ed25519.GenerateKey(nil)
	returnKey = bpk.PrivateKey()

	return returnKey
}

func GetPrivateKey() ed25519.PrivateKey {

	var keyFile = "serviceinfo.json"

	var privateKey string

	var returnKey ed25519.PrivateKey

	if data, err := os.ReadFile(keyFile); err != nil {

		bpk, _ := ed25519.GenerateKey(nil)
		returnKey = bpk.PrivateKey()
		privateKey = hex.EncodeToString(returnKey)

		if err := os.WriteFile(keyFile, []byte(privateKey), 0600); err != nil {
			log.Panicf("Failed to save service info: %v", err)
		}

	} else {

		privateKeyBytes, _ := hex.DecodeString(string(data))
		returnKey = ed25519.PrivateKey(privateKeyBytes)
		//pk := returnKey.Public()

		//log.Printf("prvkey is: [%#v]\nPubkey is [%#v]\n", returnKey, (pk))
	}

	return returnKey
}

func EncodePublicKey(publicKey ed25519.PublicKey) string {

	// checksum = H(".onion checksum" || pubkey || version)
	var checksumBytes bytes.Buffer
	checksumBytes.Write([]byte(".onion checksum"))
	checksumBytes.Write([]byte(publicKey))
	checksumBytes.Write([]byte{0x03})
	checksum := sha3.Sum256(checksumBytes.Bytes())

	// onion_address = base32(pubkey || checksum || version)
	var onionAddressBytes bytes.Buffer
	onionAddressBytes.Write([]byte(publicKey))
	onionAddressBytes.Write([]byte(checksum[:2]))
	onionAddressBytes.Write([]byte{0x03})
	onionAddress := base32.StdEncoding.EncodeToString(onionAddressBytes.Bytes())

	return strings.ToLower(onionAddress)

}

func Sign(privateKey ed25519.PrivateKey, data []byte) []byte {
	return ed25519.Sign(privateKey, data)
}

func Verify(publicKey ed25519.PublicKey, signature, data []byte) bool {
	return ed25519.Verify(publicKey, data, signature)
}
