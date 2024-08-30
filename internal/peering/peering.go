package peering

import (
	"database/sql"
	"fmt"
	"log"
	"net"

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
	CmdConnect    = PeeringCommand("Connect")
	CmdAddPeer    = PeeringCommand("AddPeer")
	CmdRemovePeer = PeeringCommand("RemovePeer")
	CmdDistribute = PeeringCommand("Distribute")
	CmdExit       = PeeringCommand("Exit")
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
	Name      string
	ParentCmd chan PeeringMessage
	Cmd       chan PeeringMessage
}

func NewPeer(tc *torutils.TorCon, parent chan PeeringMessage, torId string, db *sql.DB) (*Peer, error) {
	Peer := &Peer{
		Tc:        tc,
		TorId:     torId,
		ParentCmd: parent,
		Cmd:       make(chan PeeringMessage, 10),
	}
	go Peer.Worker()
	/*
		go func() {
			//	var conn net.Conn

			conn.Write([]byte("ping"))
			var buf []byte = make([]byte, 1024)
			n, _ := conn.Read(buf)
			fmt.Printf("Got reply from server: [%s]\n", buf[:n])

			conn.Close()
		}()
	*/

	return Peer, nil
}

func (p *Peer) Worker() {
	for {
		select {
		case cmd := <-p.Cmd:
			switch cmd.Cmd {
			case CmdConnect:
				p.Connect()

			case CmdDistribute:
				//			msg := cmd.Args[0].(messages.MessageTool)

			case CmdExit:
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
	}
}

func (p *Peer) Connect() {

	fmt.Printf("CLIENT Dialing\n")
	conn, err := p.Tc.Dial("tcp", p.TorId+".onion:80")
	//defer conn.Close()

	fmt.Printf("CLIENT Dialing response [%v][%v]\n", conn, err)
	if err != nil {
		fmt.Printf("Error Dialer connect: [%v]\n", err)
		return
		//return nil, err
	}

	authed, err := p.Tc.ClientHandshake(conn, torutils.GetPrivateKey(), p.TorId)
	fmt.Printf("CLIENT Authed response [%v][%v]\n", authed, err)
	if err != nil {
		fmt.Printf("CLIENT Error Dialer connect: [%v]\n", err)
		return
		//return nil, err
	}
	if authed == false {
		conn.Close()
		fmt.Printf("CLIENT: Failed to handshake.\n")
		return
		//return nil, errors.New("Failed hanshake, signature didn't match.")
	}
	p.Conn = conn
}

type Peers struct {
	Conns map[string]*Peer
	Tc    *torutils.TorCon
	Db    *sql.DB
	Cmd   chan PeeringMessage
	Exit  chan interface{}
}

func NewPeers(db *sql.DB, tc *torutils.TorCon) (*Peers, error) {
	Peers := &Peers{
		Conns: make(map[string]*Peer),
		Cmd:   make(chan PeeringMessage, 10),
		Exit:  make(chan interface{}),
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
					log.Printf("peerlist [%d][%s][%s][%s]", id, torid, pubkey, name)
					conn, _ := NewPeer(p.Tc, p.Cmd, torid, p.Db)
					p.Conns[torid] = conn
					p.Conns[torid].Cmd <- cmd
					// dialup torid

				}
				rows.Close()
			case CmdDistribute:
				for _, peer := range p.Conns {
					peer.Cmd <- cmd
				}

			case CmdAddPeer:
				var id int
				var pubkey, name string
				torid := cmd.Args[0].(string)

				log.Printf("Adding peer [%d][%s][%s][%s]", id, torid, pubkey, name)
				conn, _ := NewPeer(p.Tc, p.Cmd, torid, p.Db)
				p.Conns[torid] = conn
				cmd.Cmd = CmdConnect
				p.Conns[torid].Cmd <- cmd

			}
		case <-p.Exit:
			return
		}
	}
}

func (p *Peers) AddPeer(torId string) error {
	p.Cmd <- PeeringMessage{
		Cmd:  CmdAddPeer,
		Args: []interface{}{torId},
	}

	return nil
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
