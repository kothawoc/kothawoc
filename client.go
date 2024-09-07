package kothawoc

import (
	"database/sql"
	"encoding/base32"
	"fmt"
	"io"
	"log"
	"math/rand"
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
	"github.com/kothawoc/kothawoc/pkg/keytool"
	"github.com/kothawoc/kothawoc/pkg/messages"
	serr "github.com/kothawoc/kothawoc/pkg/serror"
)

type Client struct {
	NNTPclient *nntpclient.Client
	Server     *nntpserver.Server
	be         *nntpbackend.NntpBackend
	deviceKey  ed25519.PrivateKey
	deviceId   string
	ConfigPath string
	Tor        *torutils.TorCon
}

func NewClient(path string, port int) (*Client, error) {

	tc := torutils.NewTorCon(path + "/data")
	nntpBackend, _ := nntpbackend.NewNNTPBackend(path, tc)

	client := &Client{
		ConfigPath: path,
		Tor:        tc,
		be:         nntpBackend.NextBackend.(*nntpbackend.NntpBackend),
	}

	deviceKey, err := client.ConfigGetGetBytes("deviceKey")
	if err == sql.ErrNoRows {
		deviceKey = client.CreatePrivateKey()
		err = client.ConfigSet("deviceKey", deviceKey)
		if err != nil {
			log.Print(serr.Errorf("Error: %q", err))
			return nil, serr.New(err)
		}
	}
	if err != nil {
		log.Print(serr.Errorf("Error: %q", err))
		return nil, serr.New(err)
	}

	client.deviceKey = ed25519.PrivateKey(deviceKey)

	myKey := keytool.EasyEdKey{}
	myKey.SetTorPrivateKey(ed25519.PrivateKey(deviceKey))
	torId, err := myKey.TorId()
	if err != nil {
		return nil, serr.New(err)
	}

	client.deviceId = torId
	idGen.NodeName = client.deviceId

	s := nntpserver.NewServer(nntpBackend, idGen)

	client.Server = s

	go client.tcpServer(s, port)
	go client.torServer(tc, s)

	//go func() {
	client.Dial()
	client.CreateNewGroup("peers", "Control group for peering messages.", nntp.PostingPermitted)
	//}()
	return client, nil
}

func (c *Client) DeviceKey() ed25519.PrivateKey {
	return c.deviceKey
}
func (c *Client) DeviceId() string {
	return c.deviceId
}

func (c *Client) ConfigSet(key string, val interface{}) error {
	return serr.New(c.be.DBs.ConfigSet(key, val))
}

//func (c *Client) ConfigGet(key string, val interface{}) error {
//	return c.be.DBs.ConfigGet(key, val)
//}

func (c *Client) ConfigGetInt64(key string) (int64, error) {
	a, err := c.be.DBs.ConfigGetInt64(key)
	return a, serr.New(err)
}

func (c *Client) ConfigGetGetBytes(key string) ([]byte, error) {
	return c.be.DBs.ConfigGetGetBytes(key)
}

func (c *Client) ConfigGetString(key string) (string, error) {
	a, err := c.be.DBs.ConfigGetString(key)
	return a, serr.New(err)
}

func (c *Client) CreateNewGroup(name, description string, posting nntp.PostingStatus) error {
	// TODO,
	mail, err := messages.CreateNewsGroupMail(c.deviceKey, idGen, name, description, nil, posting)
	//log.Printf("New group mail err[%v]:=====================\n%s\n===================\n", err, mail)
	if err != nil {
		return serr.New(err)
	}

	return serr.New(c.NNTPclient.Post(strings.NewReader(mail)))
}

// func CreatePeeringMail(key ed25519.PrivateKey, idgen nntpserver.IdGenerator, name string) (string, error) {
func (c *Client) AddPeer(torId string) error {
	mail, err := messages.CreatePeeringMail(c.deviceKey, idGen, torId)
	//log.Printf("New peering mail err[%v]:=====================\n%s\n===================\n", err, mail)
	if err != nil {
		return serr.New(err)
	}

	return serr.New(c.NNTPclient.Post(strings.NewReader(mail)))
}

// func CreatePeeringMail(key ed25519.PrivateKey, idgen nntpserver.IdGenerator, name string) (string, error) {
func (c *Client) Post(mail *messages.MessageTool) error {
	mail.Article.Header.Set("Message-id", idGen.GenID())
	signedMail, err := mail.Sign(c.deviceKey)
	//log.Printf("New peering mail err[%v]:=====================\n%s\n===================\n", err, mail)
	if err != nil {
		return serr.New(err)
	}

	return serr.New(c.NNTPclient.Post(strings.NewReader(signedMail)))
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
	return c.deviceKey
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

func (c *Client) tcpServer(s *nntpserver.Server, port int) error {
	a, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return serr.New(err)
	}
	log.Print(serr.Errorf("Error resolving listener: %v", serr.New(err)))
	l, err := net.ListenTCP("tcp", a)
	if err != nil {
		l.Close()
		return serr.New(err)
	}
	log.Printf("Error setting up listener: %v", serr.New(err))
	defer l.Close()

	for {
		conn, err := l.AcceptTCP()

		log.Print(serr.Errorf("Error accepting connection: %v", serr.New(err)))
		clientSession := nntpserver.ClientSession{
			"Id":       c.deviceId,
			"PubKey":   string(fmt.Sprintf("%x", c.deviceKey.PublicKey())),
			"ConnMode": nntpbackend.ConnModeTcp,
		}

		log.Print(serr.Errorf("clid stuff [%#v][%#v]", c.deviceId, idGen))
		go s.Process(conn, clientSession)
	}
}

func (c *Client) torServer(tc *torutils.TorCon, s *nntpserver.Server) error {

	log.Print(serr.Errorf("SERVER Starting: [%v]", tc))
	onion, err := tc.Listen(80, c.deviceKey)
	if err != nil {
		return serr.New(err)
	}

	log.Print(serr.Errorf("SERVER Listening: [%v]", onion))
	//defer listenCancel()
	for {
		conn, err := onion.Accept()
		log.Print(serr.Errorf("SERVER Accept: [%v]\n", onion))
		if err != nil {
			log.Print(serr.Errorf("SERVER ERROR Accept: [%v] [%v]\n", onion, err))
			continue
		}
		go func() {
			defer conn.Close()

			var clientPubKey ed25519.PublicKey
			authCallback := func(key ed25519.PublicKey) bool {
				clientPubKey = key
				match := int64(0)

				kt := keytool.EasyEdKey{}
				kt.SetTorPublicKey(clientPubKey)
				torId, err := kt.TorId()
				if err != nil {
					log.Print(serr.Errorf("failed to convert torID: [%v]", err))
					return false
				}

				row := c.be.Peers.Db.QueryRow("SELECT COUNT(*) FROM peers WHERE torid=?", torId)
				err = row.Scan(&match)
				if err != nil {
					log.Print(serr.Errorf("Dodgy hacky auth FAILED for [%s]", torId))
					return false

				}
				if match == 1 {
					log.Print(serr.Errorf("Dodgy hacky auth accepted for [%s]", torId))
					return true
				}
				// if clientPubKey == getPeer {
				// return true
				// }
				//	clientPubKey = key
				return false
			}

			authed, err := tc.ServerHandshake(conn, c.deviceKey, authCallback)
			log.Print(serr.Errorf("SERVER AUTHed: [%v] [%v]", authed, err))
			if err != nil {
				conn.Close()
				return
			}
			if authed == nil {
				conn.Close()
				return
			}

			kt := keytool.EasyEdKey{}
			kt.SetTorPublicKey(clientPubKey)
			torId, err := kt.TorId()
			if err != nil {
				log.Print(serr.Errorf("failed to convert torID: [%v]", err))
				return
			}

			clientSession := nntpserver.ClientSession{
				"Id": torId,
				//"Id":       torutils.EncodePublicKey(clientPubKey),
				"PubKey":   string(fmt.Sprintf("%x", clientPubKey)),
				"ConnMode": nntpbackend.ConnModeTor,
			}

			log.Print(serr.Errorf("tor connection stuff [%#v][%#v]", c.deviceId, idGen))
			s.Process(conn, clientSession)
			log.Print(serr.Errorf("tor disconnection stuff [%#v][%#v]", c.deviceId, idGen))
			/*
				TODO: fix the client stuff
				2024/08/31 09:21:39 Error reading from client, dropping conn: EOF
				2024/08/31 09:21:39 tor disconnection stuff ["3rm3lavawfdngj6tspw2rrsfjcz4pxh3o7ltjxaugyhnauhir7ngvrad"][kothawoc.GenIdType{NodeName:"3rm3lavawfdngj6tspw2rrsfjcz4pxh3o7ltjxaugyhnauhir7ngvrad"}]
			*/

		}()
	}

}
