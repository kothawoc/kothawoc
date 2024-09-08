package kothawoc

import (
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"os"
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
	//deviceKey  ed25519.PrivateKey
	deviceKey  keytool.EasyEdKey
	deviceId   string
	ConfigPath string
	Tor        *torutils.TorCon
}

func init() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{AddSource: true}))
	slog.SetDefault(logger)
	slog.Info("Set logger")
}

func NewClient(path string, port int) (*Client, error) {

	tc := torutils.NewTorCon(path + "/data")
	nntpBackend, _ := nntpbackend.NewNNTPBackend(path, tc)

	client := &Client{
		ConfigPath: path,
		Tor:        tc,
		be:         nntpBackend.NextBackend.(*nntpbackend.NntpBackend),
	}

	tmpKey, err := client.ConfigGetGetBytes("deviceKey")
	myKey := keytool.EasyEdKey{}
	if errors.Is(err, sql.ErrNoRows) {
		myKey = client.CreatePrivateKey()
		pk, _ := myKey.TorPrivKey()
		//key.
		deviceKey := []byte(pk)
		err = client.ConfigSet("deviceKey", deviceKey)
		if err != nil {
			slog.Info("error", "error", err)
			return nil, serr.New(err)
		}
	}
	if err != nil {
		slog.Info("error", "error", err)
		return nil, serr.New(err)
	}

	//	client.deviceKey = ed25519.PrivateKey(deviceKey)
	myKey.SetTorPrivateKey(ed25519.PrivateKey(tmpKey))
	client.deviceKey = myKey

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
	//client.CreateNewGroup("peers", "Control group for peering messages.", nntp.PostingPermitted)
	//}()
	return client, nil
}

func (c *Client) DeviceKey() keytool.EasyEdKey {
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
func (c *Client) AddPeer(torId, myname string) error {
	mail, err := messages.CreatePeerGroup(c.deviceKey, idGen, "", myname, torId)
	//mail, err := messages.CreatePeeringMail(c.deviceKey, idGen, torId)
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
	pubkey, _ := c.deviceKey.TorPubKey()
	clientSession := nntpserver.ClientSession{
		"Id":       c.deviceId,
		"PubKey":   string(fmt.Sprintf("%x", pubkey)),
		"ConnMode": nntpbackend.ConnModeLocal,
	}
	rwc := io.ReadWriteCloser(serverConn)
	go c.Server.Process(rwc, clientSession)

	client, _ := nntpclient.NewConn(clientConn)

	c.NNTPclient = client

	c.NNTPclient.Authenticate("test", "test")
}

func (c *Client) GetKey() keytool.EasyEdKey {
	return c.deviceKey
}

func (c *Client) CreatePrivateKey() keytool.EasyEdKey {
	kt := keytool.EasyEdKey{}
	kt.GenerateKey()
	//key, _ := kt.TorPrivKey()
	return kt
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
	slog.Info("Resolving listener", "error", err)
	l, err := net.ListenTCP("tcp", a)
	if err != nil {
		l.Close()
		return serr.New(err)
	}
	slog.Info("Setting up listener", "error", err)
	defer l.Close()

	for {
		conn, err := l.AcceptTCP()

		slog.Info("Error accepting connection", "error", err)
		pubkey, _ := c.deviceKey.TorPubKey()
		clientSession := nntpserver.ClientSession{
			"Id":       c.deviceId,
			"PubKey":   string(fmt.Sprintf("%x", pubkey)),
			"ConnMode": nntpbackend.ConnModeTcp,
		}

		slog.Info("clid stuff [%#v][%#v]", c.deviceId, idGen)
		go s.Process(conn, clientSession)
	}
}

func (c *Client) torServer(tc *torutils.TorCon, s *nntpserver.Server) error {

	slog.Info("SERVER Starting", "torconn", tc)
	privKey, _ := c.deviceKey.TorPrivKey()
	onion, err := tc.Listen(80, privKey)
	if err != nil {
		return serr.New(err)
	}

	slog.Info("SERVER Listening", "onion", onion)
	//defer listenCancel()
	for {
		conn, err := onion.Accept()
		slog.Info("SERVER Accept", "onion", onion)
		if err != nil {
			slog.Info("SERVER ERROR Accept", "onion", onion, "error", err)
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
					slog.Info("failed to convert torID", "error", err)
					return false
				}

				row := c.be.Peers.Db.QueryRow("SELECT COUNT(*) FROM peers WHERE torid=?", torId)
				err = row.Scan(&match)
				if err != nil {
					slog.Info("Dodgy hacky auth FAILED for", "torid", torId, "error", err)
					return false

				}
				if match == 1 {
					slog.Info("Dodgy hacky auth accepted for", "torid", torId)
					return true
				}
				// if clientPubKey == getPeer {
				// return true
				// }
				//	clientPubKey = key
				return false
			}

			privkey, _ := c.deviceKey.TorPrivKey()
			authed, err := tc.ServerHandshake(conn, privkey, authCallback)
			slog.Info("SERVER AUTHed", "authed", authed, "error", err)
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
				slog.Info("failed to convert torID", "error", err)
				return
			}

			clientSession := nntpserver.ClientSession{
				"Id": torId,
				//"Id":       torutils.EncodePublicKey(clientPubKey),
				"PubKey":   string(fmt.Sprintf("%x", clientPubKey)),
				"ConnMode": nntpbackend.ConnModeTor,
			}

			slog.Info("tor connection stuff", "deviceid", c.deviceId, "idgen", idGen)
			s.Process(conn, clientSession)
			slog.Info("tor disconnection stuff", "deviceid", c.deviceId, "idgen", idGen)
			/*
				TODO: fix the client stuff
				2024/08/31 09:21:39 Error reading from client, dropping conn: EOF
				2024/08/31 09:21:39 tor disconnection stuff ["3rm3lavawfdngj6tspw2rrsfjcz4pxh3o7ltjxaugyhnauhir7ngvrad"][kothawoc.GenIdType{NodeName:"3rm3lavawfdngj6tspw2rrsfjcz4pxh3o7ltjxaugyhnauhir7ngvrad"}]
			*/

		}()
	}

}
