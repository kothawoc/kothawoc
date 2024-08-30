package messages

import (
	"log"
	"net/textproto"
	"strings"
	"time"

	"github.com/cretz/bine/torutil/ed25519"

	"github.com/kothawoc/go-nntp"
	nntpserver "github.com/kothawoc/go-nntp/server"

	//"github.com/kothawoc/kothawoc/internal/messages"
	"github.com/kothawoc/kothawoc/internal/torutils"
)

/*
RFC 5537           Netnews Architecture and Protocols      November 2009

5.2.1.1.  newgroup Control Message Example

	A newgroup control message requesting creation of the moderated
	newsgroup example.admin.info.

	      From: "example.* Administrator" <admin@noc.example>
	      Newsgroups: example.admin.info
	      Date: 27 Feb 2002 12:50:22 +0200
	      Subject: cmsg newgroup example.admin.info moderated
	      Approved: admin@noc.example
	      Control: newgroup example.admin.info moderated
	      Message-ID: <ng-example.admin.info-20020227@noc.example>
	      MIME-Version: 1.0
	      Content-Type: multipart/mixed; boundary="nxtprt"
	      Content-Transfer-Encoding: 8bit

	      This is a MIME control message.
	      --nxtprt
	      Content-Type: application/news-groupinfo; charset=us-ascii

	      For your newsgroups file:
	      example.admin.info      About the example.* groups (Moderated)

	      --nxtprt
	      Content-Type: text/plain; charset=us-ascii

	      A moderated newsgroup for announcements about new newsgroups in
	      the example.* hierarchy.

	      --nxtprt--

Allbery & Lindsey           Standards Track                    [Page 37] - [Page 38]
*/

type ControMesasgeFunctions struct {
	NewGroup func(name, description, flags string) error
	AddPeer  func(name string) error
}

// func CheckControl(msg *messages.MessageTool, newGroup func(name, description, flags string) error) bool {
func CheckControl(msg *MessageTool, cmf ControMesasgeFunctions) bool {

	if ctrl := msg.Article.Header.Get("Control"); ctrl != "" {
		splitCtl := strings.Split(ctrl, " ")
		switch splitCtl[0] {

		//	RFC 5537
		//	5. Control Messages ...............................................35
		//	5.1. Authentication and Authorization ..........................35
		//	5.2. Group Control Messages ....................................36
		//		 5.2.1. The newgroup Control Message .......................36
		//				5.2.1.1. newgroup Control Message Example ..........37
		//		 5.2.2. The rmgroup Control Message ........................38
		//		 5.2.3. The checkgroups Control Message ....................38
		//	5.3. The cancel Control Message ................................40
		// rfc defined messages
		case "cancel": // RFC 5537 - 5.3. The cancel Control Message
			log.Printf("Cancel\n")
		case "newsgroup": // RFC 5537 - 5.2.1. The newgroup Control Message
			// TODO: LOLz people can create any newsgroup name they wish, so long as it's
			// one "word", lile "<ID>.Y0URMØ7#3r.w0z.ar.#4m$t3r!.`/tmp/andnoexploitsfoundhere`.fun"
			splitGroup := strings.Split(splitCtl[1], ".")
			if msg.Article.Header.Get("From") == splitGroup[0] {
				// the from header is the owner of the group, so allow it
				flags := ""
				flaglen := 0
				if len(splitCtl) == 3 {
					flags = splitCtl[2]
					flaglen = 1
				}
				description := ""
				for _, h := range msg.Parts {
					if h.Header.Get("Content-Type") == "application/news-groupinfo;charset=UTF-8" {
						// TODO FIXME: this is going to explode when handed a dodgy message
						data := strings.Split(strings.Split(string(h.Content), "\n")[1], " ")
						subslice := data[2 : len(data)-flaglen]
						description = strings.Join(subslice, " ")
					}
				}
				//	description := strings.Join(splitCtl[2:len(splitCtl)-1], "\n")[1]
				//be.DBs.NewGroup(splitCtl[1], description, splitCtl[len(splitCtl)])
				cmf.NewGroup(splitCtl[1], description, flags)
			}
			//dbs.NewGroup("alt.misc.test", "Alt misc test group", "y")
			//dbs.NewGroup("misc.test", "Alt misc test group", "y")
			//dbs.NewGroup("alt.test", "Alt misc test group", "y")
		case "rmgroup": // RFC 5537 - 5.2.2. The rmgroup Control Message

			// custom messages
		case "Subscribe":
		case "UnSubscribe":
		case "AddPeer":
			cmf.AddPeer(splitCtl[1])

		case "RemovePeer":
		case "SetPerms":
		}
	}

	return false
}

/*
// PostingStatus type for groups.
type PostingStatus byte

// PostingStatus values.
const (

	Unknown             = PostingStatus(0)
	PostingPermitted    = PostingStatus('y')
	PostingNotPermitted = PostingStatus('n')
	PostingModerated    = PostingStatus('m')

)
*/

func CreateNewsGroupMail(key ed25519.PrivateKey, idgen nntpserver.IdGenerator, name, description string, posting nntp.PostingStatus) (string, error) {

	// Subject: cmsg newgroup example.admin.info moderated
	// Control: newgroup example.admin.info moderated
	var modStr string = ""
	switch posting {
	case nntp.PostingPermitted:
		modStr = " moderated"
	case nntp.PostingNotPermitted:
	case nntp.PostingModerated:
	default:
	}

	ownerID := torutils.EncodePublicKey(key.PublicKey())

	return (&MessageTool{
		Article: &nntp.Article{
			Header: textproto.MIMEHeader{
				"Subject":                   {"cmsg newsgroup " + ownerID + "." + name + modStr},
				"Control":                   {"newsgroup " + ownerID + "." + name + modStr},
				"Message-Id":                {idgen.GenID()},
				"Date":                      {time.Now().UTC().Format(time.RFC1123Z)},
				"Newsgroups":                {ownerID + "." + name},
				"Content-Type":              {"multipart/mixed; boundary=\"nxtprt\""},
				"Content-Transfer-Encoding": {"8bit"},
			},
		},
		Preamble: "This is a MIME control message.",
		Parts: []MimePart{
			{
				Header:  textproto.MIMEHeader{"Content-Type": []string{"application/news-groupinfo;charset=UTF-8"}},
				Content: []byte("For your newsgroups file:\r\n" + ownerID + "." + name + " " + description + modStr),
			},
			{
				Header:  textproto.MIMEHeader{"Content-Type": []string{"text/plain;charset=UTF-8"}},
				Content: []byte("This is a system control message to create the newsgroup " + ownerID + "." + name + ".\r\n"),
			},
		},
	}).Sign(key)
}

func CreatePeeringMail(key ed25519.PrivateKey, idgen nntpserver.IdGenerator, name string) (string, error) {

	// Subject: cmsg newgroup example.admin.info moderated
	// Control: newgroup example.admin.info moderated

	ownerID := torutils.EncodePublicKey(key.PublicKey())

	return (&MessageTool{
		Article: &nntp.Article{
			Header: textproto.MIMEHeader{
				"Subject":                   {"AddPeer " + name},
				"Control":                   {"AddPeer " + name},
				"Message-Id":                {idgen.GenID()},
				"Date":                      {time.Now().UTC().Format(time.RFC1123Z)},
				"Newsgroups":                {ownerID + ".peers"},
				"Content-Type":              {"multipart/mixed; boundary=\"nxtprt\""},
				"Content-Transfer-Encoding": {"8bit"},
			},
		},
		Preamble: "This is a MIME control message.",
		Parts: []MimePart{
			{
				Header:  textproto.MIMEHeader{"Content-Type": []string{"application/news-groupinfo;charset=UTF-8"}},
				Content: []byte("For your newsgroups file:\r\nAddPeer " + name),
			},
			{
				Header:  textproto.MIMEHeader{"Content-Type": []string{"text/plain;charset=UTF-8"}},
				Content: []byte("This is a system control message to add the peer " + name + ".\r\n"),
			},
		},
	}).Sign(key)
}
