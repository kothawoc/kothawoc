package peering

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/cretz/bine/torutil/ed25519"

	nntpclient "github.com/kothawoc/go-nntp/client"
	"github.com/kothawoc/kothawoc/internal/databases"
	"github.com/kothawoc/kothawoc/internal/torutils"
	"github.com/kothawoc/kothawoc/pkg/keytool"
	"github.com/kothawoc/kothawoc/pkg/messages"
	serr "github.com/kothawoc/kothawoc/pkg/serror"
)

/*
CREATE TABLE IF NOT EXISTS peers (

	id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	torid TEXT NOT NULL UNIQUE,
	pubkey TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL
	);
*/

type PeeringCommand string

const (
	CmdConnect      = PeeringCommand("Connect")
	CmdAddPeer      = PeeringCommand("AddPeer")
	CmdRemovePeer   = PeeringCommand("RemovePeer")
	CmdDistribute   = PeeringCommand("Distribute")
	CmdExit         = PeeringCommand("Exit")
	CmdWorkerExited = PeeringCommand("WorkerExited")
	CmdSendme       = PeeringCommand("Sendme")
)

type PeeringMessage struct {
	Cmd  PeeringCommand
	Args []interface{}
}

type Peer struct {
	Id        int
	Tc        *torutils.TorCon
	Dbs       databases.BackendDbs
	Conn      net.Conn
	PeerTorId string
	MyTorId   string
	MyKey     keytool.EasyEdKey
	PeerKey   keytool.EasyEdKey
	GroupName string
	Client    *nntpclient.Client
	ParentCmd chan PeeringMessage
	Cmd       chan PeeringMessage
}

func NewPeer(tc *torutils.TorCon, parent chan PeeringMessage, myKey, peerKey keytool.EasyEdKey, dbs databases.BackendDbs) (*Peer, error) {

	myTorId, _ := myKey.TorId()
	peerTorId, _ := peerKey.TorId()

	groupName := myTorId + ".peers." + peerTorId

	Peer := &Peer{
		Tc:        tc,
		GroupName: groupName,
		Dbs:       dbs,
		ParentCmd: parent,
		MyKey:     myKey,
		PeerKey:   peerKey,
		MyTorId:   myTorId,
		PeerTorId: peerTorId,
		Cmd:       make(chan PeeringMessage, 10),
	}
	go Peer.Worker()

	return Peer, nil
}

func (p *Peer) Worker() {
	go func() {
		for {
			time.Sleep(time.Second * 5)
			// only send a refresh if it's not busy
			if len(p.Cmd) == 0 {
				p.Cmd <- PeeringMessage{}
			}
		}
	}()
	defer close(p.Cmd)

	gDB := p.Dbs.GroupArticles[p.GroupName]

	for {
		select {
		case cmd := <-p.Cmd:
			//log.Printf("Spam Command Loop: [%#v]", cmd)
			switch cmd.Cmd {
			case CmdConnect:
				p.Connect()

			case CmdDistribute:
				msg := cmd.Args[0].(messages.MessageTool)
				// TODO: filter mail to see if we should actually post it?
				if p.Client != nil {
					//time.Sleep(time.Second * 30)
					if p.Client != nil {
						err := p.Client.Post(strings.NewReader(msg.RawMail()))
						if err != nil {

							slog.Info("CLIENT POST Error cannot send attempting reconnect", "torid", p.PeerTorId)
							p.Conn.Close()
							p.Conn = nil
						}
					} else {

						slog.Info("CLIENT Error cannot send not connect", "torid", p.PeerTorId)
					}
				}

			case CmdExit:
				p.ParentCmd <- PeeringMessage{
					Cmd:  CmdWorkerExited,
					Args: []interface{}{p.PeerTorId},
				}
				p.Conn.Close()
				return

			case CmdSendme:
				/*

					func (p *Peers) Sendme(peerid, list, options string) error {
						err := make(chan error)
						p.Cmd <- PeeringMessage{
							Cmd:  CmdSendme,
							Args: []interface{}{peerid, list, options, err},

						Content: []byte("ControlMessages: " + cMsgs + "\r\nFeed: " + strings.Join(feed, ",")),
				*/
				peerid := cmd.Args[0].(string)
				list := strings.Split(cmd.Args[1].(string), "\r\n")
				opts := strings.Split(cmd.Args[2].(string), "\r\n")
				err := cmd.Args[3].(chan error)

				cm := ""
				feed := ""
				for _, i := range opts {
					sOpt := strings.Split(i, ": ")
					switch sOpt[0] {
					case "ControlMessages":
						cm = sOpt[1]
					case "Feed":
						feed = sOpt[1]
					}
				}
				query := "UPDATE OR INSERT config(key,val) VALUES(?,?) WHERE key=?"
				gDB.Exec(query, "ControlMessages", cm)
				gDB.Exec(query, "Feed", feed)
				gDB.Exec("DELETE FROM subscriptions;")
				/*

					CREATE TABLE IF NOT EXISTS subscriptions (
						group TEXT NOT NULL UNIQUE
					    );

				*/

				for _, i := range list {
					query := "INSERT INTO subscriptions(group) VALUES(?);"
					gDB.Exec(query, i)
					slog.Info("INSERT INTO", "item", i, "query", query)

				}

				slog.Info("debug:", "peerid", peerid, "list", list, "opts", opts)

				close(err)
				//p.Conn[torid].Cmd <- cmd
			}

			//	case cmd := <-Peer.ParentCmd:
			//		fmt.Println("Received int:", cmd)
			//		for _, peer := range p.Conns {
			//			peer.Msg <- cmd
			//		}
			//case cmd := <-Peer.Msg:
			//switch cmd {
			//case "Connect":
			//}
		}
		p.Connect()
		p.SendMessages()

	}
}

func (p *Peer) SendMessages() {

	if p.Conn == nil {
		return
	}

	gDB := p.Dbs.GroupArticles[p.GroupName]
	if gDB == nil {
		return
	}
	slog.Info("gdb val", "gDB", gDB)

	row := gDB.QueryRow("SELECT val FROM config WHERE key=?", "LastMessage")
	lastMessage := int64(0)
	err := row.Scan(&lastMessage)
	if err != nil {
		slog.Info("Failed to find last sent message", "sqlErr", err)
		return
	}

	//gDB.Exec(query, "ControlMessages", cm)
	//gDB.Exec(query, "Feed", feed)

	rows, err := p.Dbs.Articles.Query("SELECT * FROM articles WHERE id>?", lastMessage)
	defer rows.Close()

	for rows.Next() {
		var id int
		var torid, pubkey, name string
		err := rows.Scan(&id, &torid, &pubkey, &name)

		/*
			CREATE TABLE IF NOT EXISTS articles (
				id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
				messageid TEXT NOT NULL UNIQUE,
				signature TEXT NOT NULL,
				refs INTEGER NOT NULL DEFAULT 0
				);
		*/
		if err != nil {
			continue
			//		return nil, err
		}
		slog.Info("peerlist", "id", id, "torid", torid, "pubkey", pubkey, "name", name)
		// NewPeer(tc *torutils.TorCon, parent chan PeeringMessage, db *sql.DB, myKey keytool.EasyEdKey, peerKey keytool.EasyEdKey, dbs BackendDbs) (*Peer, error)
		// dialup torid

	}
	rows.Close()
}

func (p *Peer) Connect() {
	//
	if p.Conn != nil {
		return
	}

	slog.Info("CLIENT Dialing", "torid", p.PeerTorId)
	conn, err := p.Tc.Dial("tcp", p.PeerTorId+".onion:80")
	//defer conn.Close()

	slog.Info("CLIENT Dialing response", "conn", conn, "error", err)
	if err != nil {
		time.Sleep(time.Second * 5)
		slog.Info("Error Dialer connect: try again.", "error", err)
		//p.Cmd <- cmd
		return
		//return nil, err
	}

	authed, err := p.Tc.ClientHandshake(conn, p.MyKey, p.PeerTorId)
	slog.Info("CLIENT Authed response", "authed", authed, "error", err)
	if err != nil {
		slog.Info("CLIENT Error Dialer connect", "error", err)
		return
		//return nil, err
	}
	if authed == nil {
		conn.Close()
		slog.Info("CLIENT: Failed to handshake.")
		return
		//return nil, errors.New("Failed hanshake, signature didn't match.")
	}

	c, _ := nntpclient.NewConn(conn)
	p.Client = c

	p.Conn = conn
	c.Authenticate("user", "password")
}

type Peers struct {
	Conns map[string]*Peer
	MyKey keytool.EasyEdKey
	Key   ed25519.PrivateKey
	Tc    *torutils.TorCon
	DBs   databases.BackendDbs
	Cmd   chan PeeringMessage
	Exit  chan interface{}
}

func NewPeers(tc *torutils.TorCon, myKey keytool.EasyEdKey, DBs *databases.BackendDbs) (*Peers, error) {
	Peers := &Peers{
		Conns: make(map[string]*Peer),
		Cmd:   make(chan PeeringMessage, 10),
		Exit:  make(chan interface{}),
		MyKey: myKey,
		Tc:    tc,
		DBs:   DBs,
	}

	/*
	   id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	   torid TEXT NOT NULL UNIQUE,
	   pubkey TEXT NOT NULL UNIQUE,
	   name TEXT NOT NULL
	*/

	go Peers.Worker()

	return Peers, nil
}

func (p *Peers) Worker() {
	for {
		select {
		case cmd := <-p.Cmd:
			switch cmd.Cmd {
			case CmdConnect:
				errChan := cmd.Args[0].(chan error)

				rows, err := p.DBs.Peers.Query("SELECT id,torid,pubkey,name FROM peers;")
				if err != nil {
					errChan <- serr.New(err)
					close(errChan)
					rows.Close()
					continue
					//	return nil, err
				}
				for rows.Next() {
					var id int
					var torid, pubkey, name string
					err := rows.Scan(&id, &torid, &pubkey, &name)
					if err != nil {
						continue
						//		return nil, err
					}
					slog.Info("peerlist", "id", id, "torid", torid, "pubkey", pubkey, "name", name)
					// NewPeer(tc *torutils.TorCon, parent chan PeeringMessage, db *sql.DB, myKey keytool.EasyEdKey, peerKey keytool.EasyEdKey, dbs BackendDbs) (*Peer, error)
					peerKey := keytool.EasyEdKey{}
					peerKey.SetTorId(torid)
					conn, _ := NewPeer(p.Tc, p.Cmd, p.MyKey, peerKey, p.DBs)
					p.Conns[torid] = conn
					p.Conns[torid].Cmd <- cmd
					// dialup torid

				}
				rows.Close()
				close(errChan)
			case CmdDistribute:
				for _, peer := range p.Conns {
					peer.Cmd <- cmd
				}

			case CmdRemovePeer:

				torid := cmd.Args[0].(string)
				p.Conns[torid].Cmd <- cmd
				delete(p.Conns, torid)
				res, err := p.DBs.Peers.Exec("DELETE FROM peers WHERE torid=?;", torid)
				slog.Info("TRY REMOVE PEER DELETE", "error", err, "res", res)
				//if err != nil {
				//	errChan <- err
				//	continue
				//}

			case CmdAddPeer:
				var id int
				var pubkey, name string
				torid := cmd.Args[0].(string)
				errChan := cmd.Args[1].(chan error)

				row := p.DBs.Peers.QueryRow("SELECT id,torid,pubkey,name FROM peers WHERE torid=?;", torid)
				err := row.Scan(&id, &torid, &pubkey, &name)
				slog.Info("ADDING PEER", "torid", torid, "id", id, "error", err)
				if err == nil {
					errChan <- serr.Wrap(fmt.Errorf("Peer already exists %s=%s", "torid", torid), err)
					continue
				} else {
					if !errors.Is(err, sql.ErrNoRows) {
						errChan <- serr.Wrap(fmt.Errorf("Peer Add error %s=%s", "torid", torid), err)
						continue
					}
				}

				myTorId, _ := p.MyKey.TorId()
				gDB := p.DBs.GroupArticles[myTorId+".peers."+torid]
				query := "UPDATE OR INSERT config(key,val) VALUES(?,?) WHERE key=?;"
				gDB.Exec(query, "ControlMessages", "true")
				gDB.Exec(query, "Feed", torid)
				gDB.Exec(query, "LastMessage", 0)

				slog.Info("Adding peer", "id", id, "torid", torid, "pubkey", pubkey, "name", name)

				// NewPeer(tc *torutils.TorCon, parent chan PeeringMessage, db *sql.DB, myKey keytool.EasyEdKey, peerKey keytool.EasyEdKey, dbs BackendDbs) (*Peer, error)
				peerKey := keytool.EasyEdKey{}
				peerKey.SetTorId(torid)
				conn, err := NewPeer(p.Tc, p.Cmd, p.MyKey, peerKey, p.DBs)

				slog.Info("ERROR ADDPEER", "error", err)
				if err != nil {
					errChan <- err
					continue
				}
				res, err := p.DBs.Peers.Exec("INSERT INTO peers(torid,pubkey,name) VALUES(?,\"tmp\",\"\");", torid)
				slog.Info("ERROR ADDPEER INSERT", "error", err, "res", res)
				if err != nil {
					errChan <- err
					continue
				}
				p.Conns[torid] = conn
				cmd.Cmd = CmdConnect
				p.Conns[torid].Cmd <- cmd
				errChan <- nil
				close(errChan)
			case CmdSendme:
				torid := cmd.Args[0].(string)
				p.Conns[torid].Cmd <- cmd
			}

		case <-p.Exit:
			return
		}
	}
}

func (p *Peers) AddPeer(torId string, pDBs map[string]*sql.DB) error {
	a := p.DBs
	a.GroupArticles = pDBs
	p.DBs = a
	err := make(chan error)
	p.Cmd <- PeeringMessage{
		Cmd:  CmdAddPeer,
		Args: []interface{}{torId, pDBs, err},
	}

	return <-err
}

func (p *Peers) RemovePeer(torId string) error {

	p.Cmd <- PeeringMessage{
		Cmd:  CmdRemovePeer,
		Args: []interface{}{torId},
	}

	return nil
}

func (p *Peers) DistributeArticle(msg messages.MessageTool) error {

	p.Cmd <- PeeringMessage{
		Cmd:  CmdDistribute,
		Args: []interface{}{msg},
	}

	return nil
}

func (p *Peers) Connect() error {
	err := make(chan error)
	p.Cmd <- PeeringMessage{
		Cmd:  CmdConnect,
		Args: []interface{}{err},
	}

	return <-err
}

func (p *Peers) Sendme(peerid, list, options string) error {
	err := make(chan error)
	p.Cmd <- PeeringMessage{
		Cmd:  CmdSendme,
		Args: []interface{}{peerid, list, options, err},
	}

	return <-err
}
