package messages

import (
	"bytes"
	"log"
	"net/textproto"
	"strings"
	"time"

	"github.com/cretz/bine/torutil/ed25519"
	vcard "github.com/emersion/go-vcard"

	//"github.com/kothawoc/kothawoc/internal/messages"
	"github.com/kothawoc/go-nntp"
	nntpserver "github.com/kothawoc/go-nntp/server"
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
	NewGroup   func(name, description string, card vcard.Card) error
	AddPeer    func(name string) error
	RemovePeer func(name string) error
	Cancel     func(from, messageid, newsgroups string, cmf ControMesasgeFunctions) error
}

// func CheckControl(msg *messages.MessageTool, newGroup func(name, description, flags string) error) bool {
func CheckControl(msg *MessageTool, cmf ControMesasgeFunctions) error {

	log.Printf("CHECK CONTROL MESSAGE: [%s]", msg)
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

			return cmf.Cancel(msg.Article.Header.Get("From"), splitCtl[1], msg.Article.Header.Get("Newsgroups"), cmf)

		case "newsgroup": // RFC 5537 - 5.2.1. The newgroup Control Message
			// TODO: LOLz people can create any newsgroup name they wish, so long as it's
			// one "word", lile "<ID>.Y0URMØ7#3r.w0z.ar.#4m$t3r!.`/tmp/andnoexploitsfoundhere`.fun"
			splitGroup := strings.Split(splitCtl[1], ".")
			var card vcard.Card
			if msg.Article.Header.Get("From") == splitGroup[0] {
				// the from header is the owner of the group, so allow it
				//flags := ""
				flaglen := 0
				if len(splitCtl) == 3 {
					//flags = splitCtl[2]
					flaglen = 1
				}
				description := ""
				for _, h := range msg.Parts {
					switch h.Header.Get("Content-Type") {
					case "application/news-groupinfo;charset=UTF-8":
						// TODO FIXME: this is going to explode when handed a dodgy message
						data := strings.Split(strings.Split(string(h.Content), "\n")[1], " ")
						subslice := data[2 : len(data)-flaglen]
						description = strings.Join(subslice, " ")
					case "text/x-vcard;charset=UTF-8":
						dec := vcard.NewDecoder(bytes.NewReader(h.Content))
						card, _ = dec.Decode()
						log.Printf("card: [%#v]", card)
					}

				}
				//	description := strings.Join(splitCtl[2:len(splitCtl)-1], "\n")[1]
				//be.DBs.NewGroup(splitCtl[1], description, splitCtl[len(splitCtl)])
				return cmf.NewGroup(splitCtl[1], description, card)

			}
			//dbs.NewGroup("alt.misc.test", "Alt misc test group", "y")
			//dbs.NewGroup("misc.test", "Alt misc test group", "y")
			//dbs.NewGroup("alt.test", "Alt misc test group", "y")
		case "rmgroup": // RFC 5537 - 5.2.2. The rmgroup Control Message

			// custom messages
		case "Subscribe":
		case "UnSubscribe":
		case "AddPeer":
			return cmf.AddPeer(splitCtl[1])

		case "RemovePeer":
		case "SetPerms":
		default:
			log.Printf("ERROR CONTROL MESSAGE: [%s]", msg)
		}
	}

	return nil
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

func CreateNewsGroupMail(key ed25519.PrivateKey, idgen nntpserver.IdGenerator, fullname, description string, card vcard.Card, posting nntp.PostingStatus) (string, error) {

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
	// *cough* clean off the initial id if it's there.
	names := strings.Split(fullname, ownerID+".")
	name := names[0]
	if len(names) > 1 {
		name = names[1]
	}

	parts := []MimePart{
		{
			Header:  textproto.MIMEHeader{"Content-Type": []string{"application/news-groupinfo;charset=UTF-8"}},
			Content: []byte("For your newsgroups file:\r\n" + ownerID + "." + name + " " + description + modStr),
		},
		{
			Header:  textproto.MIMEHeader{"Content-Type": []string{"text/plain;charset=UTF-8"}},
			Content: []byte("This is a system control message to create the newsgroup " + ownerID + "." + name + ".\r\n"),
		},
	}
	if card != nil {

		buf := &bytes.Buffer{}
		enc := vcard.NewEncoder(buf)
		err := enc.Encode(card)
		if err != nil {
			return "", err
		}
		//fmt.Println("Form submitted ", buf.String())
		parts = append(parts, MimePart{
			Header:  textproto.MIMEHeader{"Content-Type": []string{"text/x-vcard;charset=UTF-8"}},
			Content: buf.Bytes(),
		})

	}

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
		Parts:    parts,
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
