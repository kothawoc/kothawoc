package kothawoc

import (
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/cretz/bine/torutil/ed25519"

	"github.com/kothawoc/go-nntp"
	nntpclient "github.com/kothawoc/go-nntp/client"
	nntpserver "github.com/kothawoc/go-nntp/server"

	"github.com/kothawoc/kothawoc/internal/nntpbackend"
	"github.com/kothawoc/kothawoc/internal/torutils"
	"github.com/kothawoc/kothawoc/pkg/messages"
)

type Client struct {
	NNTPclient *nntpclient.Client
	Server     *nntpserver.Server
	be         *nntpbackend.NntpBackend
	deviceKey  ed25519.PrivateKey
	deviceId   string
	ConfigPath string
}

func NewClient(path string) *Client {

	nntpBackend, _ := nntpbackend.NewNNTPBackend(path)

	client := &Client{
		ConfigPath: path,
		be:         nntpBackend.NextBackend.(*nntpbackend.NntpBackend),
	}

	deviceKey, err := client.ConfigGetGetBytes("deviceKey")
	if err == sql.ErrNoRows {
		deviceKey = client.CreatePrivateKey()
		err = client.ConfigSet("deviceKey", deviceKey)
		if err != nil {
			log.Printf("Error: Cannot create device key: [%v]\n", err)
			return nil
		}
	}
	if err != nil {
		log.Printf("Error: Cannot get device key: [%v]\n", err)
		return nil
	}

	client.deviceKey = ed25519.PrivateKey(deviceKey)
	client.deviceId = torutils.EncodePublicKey(client.deviceKey.PublicKey())
	idGen.NodeName = client.deviceId

	s := nntpserver.NewServer(nntpBackend, idGen)

	client.Server = s

	go client.tcpServer(s)

	return client
}

func (c *Client) ConfigSet(key string, val interface{}) error {
	return c.be.DBs.ConfigSet(key, val)
}

//func (c *Client) ConfigGet(key string, val interface{}) error {
//	return c.be.DBs.ConfigGet(key, val)
//}

func (c *Client) ConfigGetInt64(key string) (int64, error) {
	return c.be.DBs.ConfigGetInt64(key)
}

func (c *Client) ConfigGetGetBytes(key string) ([]byte, error) {
	return c.be.DBs.ConfigGetGetBytes(key)
}

func (c *Client) ConfigGetString(key string) (string, error) {
	return c.be.DBs.ConfigGetString(key)
}

func (c *Client) CreateNewGroup(name, description string, posting nntp.PostingStatus) error {
	mail, err := messages.CreateNewsGroupMail(c.deviceKey, idGen, name, description, posting)
	log.Printf("New group mail err[%v]:=====================\n%s\n===================\n", err, mail)
	if err != nil {
		return err
	}

	return c.NNTPclient.Post(strings.NewReader(mail))
}

func (c *Client) Dial() {
	serverConn, clientConn := net.Pipe()

	// connect a net.Pipe end to the server session
	clientSession := nntpserver.ClientSession{
		"Id":       c.deviceId,
		"PubKey":   string(fmt.Sprintf("%x", c.deviceKey.PublicKey())),
		"ConnMode": nntpbackend.ConnModeLocal,
	}
	rwc := io.ReadWriteCloser(serverConn)
	go c.Server.Process(rwc, clientSession)

	client, _ := nntpclient.NewConn(clientConn)

	c.NNTPclient = client

	c.NNTPclient.Authenticate("test", "test")
}

func (c *Client) GetKey() ed25519.PrivateKey {
	return torutils.GetPrivateKey()
}

func (c *Client) CreatePrivateKey() ed25519.PrivateKey {
	return torutils.CreatePrivateKey()
}

type GenIdType struct {
	NodeName string
}

func (i GenIdType) GenID() string {
	randSpan := make([]byte, 20)

	rand.Read(randSpan)
	tstr := strings.ToLower(fmt.Sprintf("<%s-%s@%s>",
		strconv.FormatInt(time.Now().Unix(), 32),
		base32.StdEncoding.EncodeToString(randSpan),
		i.NodeName))
	return tstr
}

var idGen GenIdType

func (c *Client) tcpServer(s *nntpserver.Server) {
	a, err := net.ResolveTCPAddr("tcp", ":1119")
	log.Printf("Error resolving listener: %v", err)
	l, err := net.ListenTCP("tcp", a)
	log.Printf("Error setting up listener: %v", err)
	defer l.Close()

	for {
		conn, err := l.AcceptTCP()

		log.Printf("Error accepting connection: %v", err)
		clientSession := nntpserver.ClientSession{
			"Id":       c.deviceId,
			"PubKey":   string(fmt.Sprintf("%x", c.deviceKey.PublicKey())),
			"ConnMode": nntpbackend.ConnModeTcp,
		}

		log.Printf("clid stuff [%#v][%#v]", c.deviceId, idGen)
		go s.Process(conn, clientSession)
	}
}
