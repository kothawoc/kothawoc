package main

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	nntpserver "github.com/kothawoc/go-nntp/server"

	"github.com/kothawoc/kothawoc/internal/nntpbackend"
)

// see https://github.com/maxymania/go-nntp/tree/master/server

var debug bool = true

type GenIdType struct{}

func (GenIdType) GenID() string {

	randSpan := make([]byte, 20)

	rand.Read(randSpan)
	tstr := strings.ToLower(fmt.Sprintf("<%s-%s@%s>",
		strconv.FormatInt(time.Now().Unix(), 32),
		base32.StdEncoding.EncodeToString(randSpan),
		"cows"))
	return tstr
}

var idGen GenIdType

func main() {
	a, err := net.ResolveTCPAddr("tcp", ":1119")
	log.Printf("Error resolving listener: %v", err)
	l, err := net.ListenTCP("tcp", a)
	log.Printf("Error setting up listener: %v", err)
	defer l.Close()

	nntpBackend, _ := nntpbackend.NewNNTPBackend("./data")

	s := nntpserver.NewServer(nntpBackend, idGen)

	for {
		c, err := l.AcceptTCP()

		log.Printf("Error accepting connection: %v", err)
		session := nntpserver.ClientSession{}
		session["abc"] = "def"
		session["hij"] = "jkl"
		go s.Process(c, session)
	}
}
