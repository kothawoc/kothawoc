package keytool

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"strings"
	"time"

	"github.com/cretz/bine/torutil"
	tor "github.com/cretz/bine/torutil/ed25519"
	ssh "golang.org/x/crypto/ssh"

	serr "github.com/kothawoc/kothawoc/pkg/serror"
)

type KeyType string

const (
	KtTorPublicKey  = KeyType("TorPublicKey")
	KtTorPrivateKey = KeyType("TorPrivateKey")
	KtPubliceKey    = KeyType("PublicKey")
	KtPrivateKey    = KeyType("PrivateKey")
	KtNoKey         = KeyType("")
)

var (
	ErrNoKey         error = errors.New("cannot perform action, requires key importing")
	ErrPublicKey     error = errors.New("cannot perform action with type: public key")
	ErrTorPublicKey  error = errors.New("cannot perform action with type: tor public key")
	ErrTorPrivateKey error = errors.New("cannot perform action with type: tor private key")

	ErrInvalidCertificate     error = errors.New("invalid certificate")
	ErrInvalidKey             error = errors.New("invalid key")
	ErrInvalidPublicKey       error = errors.New("invalid public key")
	ErrInvalidKeyType         error = errors.New("ssh key is not of type ed25519")
	ErrInvalidSshAutorizedKey error = errors.New("invalid ssh authorized_keys line")
	ErrInvalidSshPublicKey    error = errors.New("invalid ssh public key")
	ErrInvalidSshPrivateKey   error = errors.New("invalid ssh private key")
	ErrInvalidTorId           error = errors.New("invalid tor id")

	ErrFailedToGenerateCertificate error = errors.New("failed to generate certificate")
)

type EasyEdKey struct {
	edKey      ed25519.PrivateKey
	edPub      ed25519.PublicKey
	torPubKey  tor.PublicKey
	torPrivKey tor.PrivateKey
	keyType    KeyType
}

/*
	func main() {
		k := (&EasyEdKey{}).New()
		p := EasyEdKey{}
		log.Printf("\nNew Key: [%#v]\nnilkey: [%#v]\n", k, p)
		cert, err := k.CreateCert(true, "localhost", "", time.Duration(0))
		log.Printf("New Cert [%#v]:\n%s", err, cert)
	}
*/

func (e *EasyEdKey) Type() KeyType {
	return e.keyType
}

// leave valid from as "" if you want it to be now.
// set validFor to be 0 if you want it to be default a year
func (e *EasyEdKey) CreateCert(isCA bool, host string, validFrom string, validFor time.Duration) ([]byte, error) {

	switch e.keyType {
	case KtPrivateKey:
		break
	case KtPubliceKey:
		return nil, serr.New(ErrPublicKey)
	case KtTorPrivateKey:
		return nil, serr.New(ErrTorPrivateKey)
	case KtTorPublicKey:
		return nil, serr.New(ErrTorPublicKey)
	default:
		return nil, serr.New(ErrNoKey)
	}

	if validFor == 0 {
		validFor = time.Duration(365 * 24 * time.Hour)
	}

	// Copyright 2009 The Go Authors. All rights reserved.
	// Use of this source code is governed by a BSD-style
	// license that can be found in the LICENSE file.
	// Generate a self-signed X.509 certificate for a TLS server. Outputs to
	// 'cert.pem' and 'key.pem' and will overwrite existing files.

	keyUsage := x509.KeyUsageDigitalSignature
	var notBefore time.Time
	var err error
	if len(validFrom) == 0 {
		notBefore = time.Now()
	} else {
		notBefore, err = time.Parse("Jan 2 15:04:05 2006", validFrom)
		if err != nil {
			return nil, serr.Wrap(ErrFailedToGenerateCertificate, err)

		}
	}
	notAfter := notBefore.Add(validFor)
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, serr.Wrap(ErrFailedToGenerateCertificate, err)
	}
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Acme Co"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              keyUsage,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	hosts := strings.Split(host, ",")
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	if isCA {
		template.IsCA = true
		template.KeyUsage |= x509.KeyUsageCertSign
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, e.edKey.Public(), e.edKey)
	if err != nil {
		return nil, serr.Wrap(ErrFailedToGenerateCertificate, err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes}), nil
}

func (e *EasyEdKey) SetPublicKey(pk ed25519.PublicKey) {
	e.edPub = pk
	e.keyType = KtPubliceKey
}

func (e *EasyEdKey) SetPrivateKey(pk ed25519.PrivateKey) {
	e.edKey = pk
	e.keyType = KtPrivateKey
}

func (e *EasyEdKey) SetTorPublicKey(pk tor.PublicKey) {
	e.torPubKey = pk
	e.keyType = KtTorPublicKey
}
func (e *EasyEdKey) SetTorPrivateKey(pk tor.PrivateKey) {
	e.torPrivKey = pk
	e.keyType = KtTorPrivateKey
}

func (e *EasyEdKey) SetTorId(id string) error {
	pubKey, err := torutil.PublicKeyFromV3OnionServiceID(id)
	if err != nil {
		return serr.Wrap(ErrInvalidTorId, err)
	}
	e.keyType = KtTorPublicKey
	e.torPubKey = pubKey
	return nil
}

func (e *EasyEdKey) SetSshPublicKey(pubKey ssh.PublicKey) error {
	// Extract the ed25519 public key from the parsed ssh.PublicKey
	ed25519PubKey, ok := pubKey.(ssh.CryptoPublicKey)
	if !ok {
		return serr.New(ErrInvalidKeyType)
	}

	// Convert the ed25519 public key to the crypto/ed25519.PublicKey type
	cryptoPubKey, ok := ed25519PubKey.CryptoPublicKey().(ed25519.PublicKey)
	if !ok {
		return serr.New(ErrInvalidKeyType)
	}
	e.edPub = cryptoPubKey
	e.keyType = KtPubliceKey
	return nil
}

func (e *EasyEdKey) SetSshAuthorizedKey(authorizedKeys []byte) error {
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorizedKeys))
	if err != nil {
		return serr.Wrap(ErrInvalidSshAutorizedKey, err)
	}
	err = e.SetSshPublicKey(pubKey)

	if err != nil {
		return serr.Wrap(ErrInvalidSshAutorizedKey, err)
	}
	return nil
}

func (e *EasyEdKey) TorId() (string, error) {
	switch e.keyType {
	case KtPrivateKey:
		torKey := tor.FromCryptoPrivateKey(e.edKey)
		return torutil.OnionServiceIDFromV3PublicKey(torKey.PublicKey()), nil
	case KtPubliceKey:
		return torutil.OnionServiceIDFromPublicKey(e.edPub), nil
	case KtTorPrivateKey:
		return torutil.OnionServiceIDFromPrivateKey(e.torPrivKey), nil
	case KtTorPublicKey:
		return torutil.OnionServiceIDFromV3PublicKey(e.torPubKey), nil
	default:
		return "", serr.New(ErrNoKey)
	}
}

func (e *EasyEdKey) TorPrivKey() (tor.PrivateKey, error) {
	switch e.keyType {
	case KtPrivateKey:
		return tor.FromCryptoPrivateKey(e.edKey).PrivateKey(), nil
	case KtPubliceKey:
		return nil, serr.New(ErrPublicKey)
	case KtTorPrivateKey:
		return e.torPrivKey, nil
	case KtTorPublicKey:
		return nil, serr.New(ErrTorPublicKey)
	default:
		return nil, serr.New(ErrNoKey)
	}
}

func (e *EasyEdKey) TorPubKey() (tor.PublicKey, error) {
	switch e.keyType {
	case KtPrivateKey:
		return tor.FromCryptoPrivateKey(e.edKey).PrivateKey().PublicKey(), nil
	case KtPubliceKey:
		return tor.FromCryptoPublicKey(e.edPub), nil
	case KtTorPrivateKey:
		return e.torPrivKey.PublicKey(), nil
	case KtTorPublicKey:
		return e.torPubKey, nil
	default:
		return nil, serr.New(ErrNoKey)
	}
}

func (e *EasyEdKey) PubKey() (ed25519.PublicKey, error) {
	switch e.keyType {
	case KtPrivateKey:
		return e.edKey.Public().(ed25519.PublicKey), nil
	case KtPubliceKey:
		return e.edPub, nil
	case KtTorPrivateKey:
		return nil, serr.New(ErrTorPrivateKey)
	case KtTorPublicKey:
		return nil, serr.New(ErrTorPublicKey)
	default:
		return nil, serr.New(ErrNoKey)
	}
}

func (e *EasyEdKey) SshPubKey() (ssh.PublicKey, error) {
	switch e.keyType {
	case KtPrivateKey:
		a, err := ssh.NewPublicKey(e.edKey.Public())
		if err != nil {
			return nil, serr.Wrap(ErrInvalidKey, err)
		}
		return a, nil
	case KtPubliceKey:
		a, err := ssh.NewPublicKey(e.edPub)
		if err != nil {
			return nil, serr.Wrap(ErrInvalidPublicKey, err)
		}
		return a, nil
	case KtTorPrivateKey:
		return nil, serr.New(ErrTorPrivateKey)
	case KtTorPublicKey:
		return nil, serr.New(ErrTorPublicKey)
	default:
		return nil, serr.New(ErrNoKey)
	}
}

func (e *EasyEdKey) SshAuthorizedKey() (string, error) {
	pubKey, err := e.SshPubKey()
	if err != nil {
		return "", serr.Wrap(ErrInvalidSshPublicKey, err)
	}
	return string(ssh.MarshalAuthorizedKey(pubKey)), nil
}

func (e *EasyEdKey) SshPrivateKey() (string, error) {

	switch e.keyType {
	case KtPrivateKey:
		block, err := ssh.MarshalPrivateKey(e.edKey, "comment")
		if err != nil {
			return "", serr.New(err)
		}
		return string(pem.EncodeToMemory(block)), nil
	case KtPubliceKey:
		return "", serr.New(ErrPublicKey)
	case KtTorPrivateKey:
		return "", serr.New(ErrTorPrivateKey)
	case KtTorPublicKey:
		return "", serr.New(ErrTorPublicKey)
	default:
		return "", serr.New(ErrNoKey)
	}
}

func (e *EasyEdKey) New() EasyEdKey {
	e.GenerateKey()
	return *e
}

func (e *EasyEdKey) GenerateKey() error {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return serr.New(err)
	}
	e.edKey = priv
	e.keyType = KtPrivateKey
	return nil
}

func (e *EasyEdKey) MarshalPrivateKey() (string, error) {

	switch e.keyType {
	case KtPrivateKey:
		b, err := x509.MarshalPKCS8PrivateKey(e.edKey)
		if err != nil {
			return "", serr.Wrap(ErrInvalidKey, err)
		}

		return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b})), nil

	case KtPubliceKey:
		return "", serr.New(ErrPublicKey)
	case KtTorPrivateKey:
		return "", serr.New(ErrTorPrivateKey)
	case KtTorPublicKey:
		return "", serr.New(ErrTorPublicKey)
	default:
		return "", serr.New(ErrNoKey)
	}
}

func (e *EasyEdKey) MarshalPublicKey() (string, error) {

	switch e.keyType {
	case KtPrivateKey:
		b, err := x509.MarshalPKIXPublicKey(e.edKey.Public())
		if err != nil {
			return "", serr.Wrap(ErrInvalidPublicKey, err)
		}

		return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: b})), nil

	case KtPubliceKey:
		b, err := x509.MarshalPKIXPublicKey(e.edPub)
		if err != nil {
			return "", serr.Wrap(ErrInvalidPublicKey, err)
		}

		return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: b})), nil

	case KtTorPrivateKey:
		return "", serr.New(ErrTorPrivateKey)
	case KtTorPublicKey:
		return "", serr.New(ErrTorPublicKey)
	default:
		return "", serr.New(ErrNoKey)
	}
}

func (e *EasyEdKey) ParseCertificate(der []byte) error {

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return serr.Wrap(ErrInvalidCertificate, err)
	}

	pub, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok {
		return serr.New(ErrInvalidCertificate)
	}
	e.edPub = pub
	e.keyType = KtPubliceKey

	return nil
}

func (e *EasyEdKey) ParseKey(der []byte) error {

	key, err := x509.ParseECPrivateKey(der)
	if err != nil {
		return serr.Wrap(ErrInvalidKey, err)
	}

	var ok bool
	priv, _ := key.ECDH() //.(ed25519.PrivateKey)
	var privInt interface{} = priv
	e.edKey, ok = privInt.(ed25519.PrivateKey)
	if !ok {
		return serr.New(ErrInvalidKey)
	}
	e.keyType = KtPrivateKey

	return nil
}

func (e *EasyEdKey) ParsePublicKey(der []byte) error {

	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return serr.Wrap(ErrInvalidPublicKey, err)
	}

	var ok bool
	pub, ok := key.(ed25519.PublicKey)
	if !ok {
		return serr.New(ErrInvalidPublicKey)
	}
	e.edPub = pub
	e.keyType = KtPubliceKey

	return nil
}

func (e *EasyEdKey) TorSign(data []byte) ([]byte, error) {

	switch e.keyType {
	case KtPrivateKey:
		key, err := e.TorPrivKey()
		if err != nil {
			return key, serr.New(err)
		}
		return tor.Sign(key, data), nil
	case KtPubliceKey:
		return nil, serr.New(ErrPublicKey)
	case KtTorPrivateKey:
		key, err := e.TorPrivKey()
		if err != nil {
			return key, serr.New(err)
		}
		return tor.Sign(key, data), nil
	case KtTorPublicKey:
		return nil, serr.New(ErrTorPublicKey)
	default:
		return nil, serr.New(ErrNoKey)
	}
}

func (e *EasyEdKey) TorVerify(signature, data []byte) (bool, error) {

	switch e.keyType {
	case KtPrivateKey:
		key, err := e.TorPubKey()
		if err != nil {
			return false, serr.New(err)
		}
		return tor.Verify(key, data, signature), nil
	case KtPubliceKey:
		key, err := e.TorPubKey()
		if err != nil {
			return false, serr.New(err)
		}
		return tor.Verify(key, data, signature), nil
	case KtTorPrivateKey:
		key, err := e.TorPubKey()
		if err != nil {
			return false, serr.New(err)
		}
		return tor.Verify(key, data, signature), nil
	case KtTorPublicKey:
		key, err := e.TorPubKey()
		if err != nil {
			return false, serr.New(err)
		}
		return tor.Verify(key, data, signature), nil
	default:
		return false, serr.New(ErrNoKey)
	}
}
