package peering

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/cretz/bine/torutil/ed25519"

	nntpclient "github.com/kothawoc/go-nntp/client"
	"github.com/kothawoc/kothawoc/internal/torutils"
	"github.com/kothawoc/kothawoc/pkg/messages"
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
)

type PeeringMessage struct {
	Cmd  PeeringCommand
	Args []interface{}
}

type Peer struct {
	Id        int
	Tc        *torutils.TorCon
	Conn      net.Conn
	TorId     string
	PubKey    string
	Key       ed25519.PrivateKey
	Name      string
	Client    *nntpclient.Client
	ParentCmd chan PeeringMessage
	Cmd       chan PeeringMessage
}

func NewPeer(tc *torutils.TorCon, parent chan PeeringMessage, torId string, db *sql.DB, key ed25519.PrivateKey) (*Peer, error) {
	Peer := &Peer{
		Tc:        tc,
		TorId:     torId,
		ParentCmd: parent,
		Key:       key,
		Cmd:       make(chan PeeringMessage, 10),
	}
	go Peer.Worker()

	return Peer, nil
}

func (p *Peer) Worker() {
	//time.Sleep((time.Second * 30))
	go func() {
		for {
			time.Sleep(time.Second * 5)
			p.Cmd <- PeeringMessage{}
		}
	}()
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

							slog.Info("CLIENT POST Error cannot send attempting reconnect", "torid", p.TorId)
							p.Conn.Close()
							p.Conn = nil
						}
					} else {

						slog.Info("CLIENT Error cannot send not connect", "torid", p.TorId)
					}
				}

			case CmdExit:
				p.ParentCmd <- PeeringMessage{
					Cmd:  CmdWorkerExited,
					Args: []interface{}{p.TorId},
				}
				p.Conn.Close()
				return
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

	}
}

func (p *Peer) Connect() {
	//
	if p.Conn != nil {
		return
	}

	slog.Info("CLIENT Dialing", "torid", p.TorId)
	conn, err := p.Tc.Dial("tcp", p.TorId+".onion:80")
	//defer conn.Close()

	slog.Info("CLIENT Dialing response", "conn", conn, "error", err)
	if err != nil {
		time.Sleep(time.Second * 5)
		slog.Info("Error Dialer connect: try again.", "error", err)
		//p.Cmd <- cmd
		return
		//return nil, err
	}

	authed, err := p.Tc.ClientHandshake(conn, p.Key, p.TorId)
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
	Key   ed25519.PrivateKey
	Tc    *torutils.TorCon
	Db    *sql.DB
	Cmd   chan PeeringMessage
	Exit  chan interface{}
}

func NewPeers(db *sql.DB, tc *torutils.TorCon, key ed25519.PrivateKey) (*Peers, error) {
	Peers := &Peers{
		Conns: make(map[string]*Peer),
		Cmd:   make(chan PeeringMessage, 10),
		Exit:  make(chan interface{}),
		Key:   key,
		Tc:    tc,
		Db:    db,
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

				rows, err := p.Db.Query("SELECT id,torid,pubkey,name FROM peers;")
				if err != nil {

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
					conn, _ := NewPeer(p.Tc, p.Cmd, torid, p.Db, p.Key)
					p.Conns[torid] = conn
					p.Conns[torid].Cmd <- cmd
					// dialup torid

				}
				rows.Close()
			case CmdDistribute:
				for _, peer := range p.Conns {
					peer.Cmd <- cmd
				}

			case CmdRemovePeer:

				torid := cmd.Args[0].(string)
				p.Conns[torid].Cmd <- cmd
				delete(p.Conns, torid)
				res, err := p.Db.Exec("DELETE FROM peers WHERE torid=?;", torid)
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

				row := p.Db.QueryRow("SELECT id,torid,pubkey,name FROM peers WHERE torid=?;", torid)
				err := row.Scan(&id, &torid, &pubkey, &name)
				slog.Info("ADDING PEER", "torid", torid, "id", id, "error", err)
				if err != sql.ErrNoRows {
					errChan <- fmt.Errorf("Peer already exists %s=%s", "torid", torid)
					continue
				}

				slog.Info("Adding peer", "id", id, "torid", torid, "pubkey", pubkey, "name", name)
				conn, err := NewPeer(p.Tc, p.Cmd, torid, p.Db, p.Key)

				slog.Info("ERROR ADDPEER", "error", err)
				if err != nil {
					errChan <- err
					continue
				}
				res, err := p.Db.Exec("INSERT INTO peers(torid,pubkey,name) VALUES(?,\"tmp\",\"\");", torid)
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
			}
		case <-p.Exit:
			return
		}
	}
}

func (p *Peers) AddPeer(torId string) error {
	err := make(chan error)
	p.Cmd <- PeeringMessage{
		Cmd:  CmdAddPeer,
		Args: []interface{}{torId, err},
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
		Cmd: CmdConnect,
	}

	return <-err
}
