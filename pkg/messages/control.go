package messages

import (
	"bytes"
	"log/slog"
	"net/textproto"
	"strings"
	"time"

	vcard "github.com/emersion/go-vcard"

	//"github.com/kothawoc/kothawoc/internal/messages"
	"github.com/kothawoc/go-nntp"
	nntpserver "github.com/kothawoc/go-nntp/server"
	"github.com/kothawoc/kothawoc/pkg/keytool"
	serr "github.com/kothawoc/kothawoc/pkg/serror"
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
func CheckControl(msg *MessageTool, cmf ControMesasgeFunctions, session map[string]string) error {

	slog.Info("CHECK CONTROL MESSAGE", "msg", msg)
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
			slog.Info("Cancel")

			return serr.New(cmf.Cancel(msg.Article.Header.Get("From"), splitCtl[1], msg.Article.Header.Get("Newsgroups"), cmf))

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
						slog.Info("card: [%#v]", "card", card)
					}

				}

				// if this is a peering group,
				if len(splitGroup) == 3 &&
					splitGroup[0] == session["Id"] &&
					splitGroup[1] == "peers" {
					err := cmf.AddPeer(splitGroup[2])
					if err != nil {
						return serr.New(err)
					}
				}
				return serr.New(cmf.NewGroup(splitCtl[1], description, card))

			}

		case "rmgroup": // RFC 5537 - 5.2.2. The rmgroup Control Message

			// custom messages
		case "checkgroups": // rfc5337 5.2.3.
		case "sendme": // rfc5337 5.5 but barstardised to have groups instread of message-ids
		default:
			slog.Info("ERROR CONTROL MESSAGE", "msg", msg)
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

func CreateNewsGroupMail(myKey keytool.EasyEdKey, idgen nntpserver.IdGenerator, fullname, description string, card vcard.Card, posting nntp.PostingStatus) (string, error) {

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

	ownerID, _ := myKey.TorId()

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
	}).Sign(myKey)
}

func CreatePeerGroup(myKey keytool.EasyEdKey, idgen nntpserver.IdGenerator, lang, myname, peerId string) (string, error) {
	card := vcard.Card{}
	card.SetValue(vcard.FieldNickname, myname)
	card.SetValue(vcard.FieldLanguage, lang)

	card.Add("X-KW-PERMS", &vcard.Field{
		Value:  "group",
		Params: vcard.Params{},
	})

	card.Add("X-KW-PERMS", &vcard.Field{
		Value: peerId,
		Params: vcard.Params{
			"read":  {"true"},
			"reply": {"true"},
			"post":  {"true"},
		},
	})

	vcard.ToV4(card)
	myId, err := myKey.TorId()
	if err != nil {
		return "", serr.New(err)
	}
	groupName := myId + ".peers." + peerId

	msg, err := CreateNewsGroupMail(myKey,
		idgen, groupName, "peer group", card, nntp.PostingPermitted)

	return msg, serr.New(err)
}

/*
# Subscriptions & Grouplists

When a peer connects, they send a checkgroups control messaage to their peer, and
records where in the message queue they are.
"application/news-checkgroups"

*** WARNING BREAKING RFC ***
The peer then sends a "sendme" control message to their peers group, of which groups
they would like the be forwarded.

*** WARNING BREAKING RFC ***
"application/newsfeed" section, with the following
in a feed config section, there is a feed preferences, which should
contain:
AllControlMessages: true/false
Feed: <tor_id>,<tor_id>,<tor_id>,<tor_Id>,.....

the feed host is the main host you want your data to go to, the other hosts are your other hosts if they are offline.


*/

func CreateCheckgroups(myKey keytool.EasyEdKey, idgen nntpserver.IdGenerator, peerId string, newsgroups [][2]string, cmsgs bool, feed []string) (string, error) {

	ownerID, _ := myKey.TorId()

	msgContent := ""
	for _, i := range newsgroups {
		msgContent += i[0] + "\t" + i[1] + "\r\n"
	}

	cMsgs := "false"
	if cmsgs {
		cMsgs = "true"
	}

	parts := []MimePart{
		{
			Header:  textproto.MIMEHeader{"Content-Type": []string{"application/news-checkgroups;charset=UTF-8"}},
			Content: []byte(msgContent),
		},
		{
			Header:  textproto.MIMEHeader{"Content-Type": []string{"application/newsfeed;charset=UTF-8"}},
			Content: []byte("ControlMessages: " + cMsgs + "\r\nFeed: " + strings.Join(feed, ",")),
		},
		{
			Header:  textproto.MIMEHeader{"Content-Type": []string{"text/plain;charset=UTF-8"}},
			Content: []byte("This is a system control message to checkgroups from " + ownerID + ".\r\n"),
		},
	}

	return (&MessageTool{
		Article: &nntp.Article{
			Header: textproto.MIMEHeader{
				"Subject":                   {"cmsg checkgroups " + peerId},
				"Control":                   {"checkgroups " + peerId},
				"Message-Id":                {idgen.GenID()},
				"Date":                      {time.Now().UTC().Format(time.RFC1123Z)},
				"Newsgroups":                {peerId + "." + ownerID},
				"Content-Type":              {"multipart/mixed; boundary=\"nxtprt\""},
				"Content-Transfer-Encoding": {"8bit"},
			},
		},
		Preamble: "This is a MIME control message.",
		Parts:    parts,
	}).Sign(myKey)
}

func CreateSendme(myKey keytool.EasyEdKey, idgen nntpserver.IdGenerator, peerId string, newsgroups []string) (string, error) {

	ownerID, _ := myKey.TorId()

	msgContent := strings.Join(newsgroups, "\r\n")
	parts := []MimePart{
		{
			Header:  textproto.MIMEHeader{"Content-Type": []string{"application/newsfeed;charset=UTF-8"}},
			Content: []byte(msgContent),
		},
		{
			Header:  textproto.MIMEHeader{"Content-Type": []string{"text/plain;charset=UTF-8"}},
			Content: []byte("This is a system control message to request groups from " + ownerID + ".\r\n"),
		},
	}

	return (&MessageTool{
		Article: &nntp.Article{
			Header: textproto.MIMEHeader{
				"Subject":                   {"cmsg sendme " + peerId},
				"Control":                   {"semdme " + peerId},
				"Message-Id":                {idgen.GenID()},
				"Date":                      {time.Now().UTC().Format(time.RFC1123Z)},
				"Newsgroups":                {peerId + "." + ownerID},
				"Content-Type":              {"multipart/mixed; boundary=\"nxtprt\""},
				"Content-Transfer-Encoding": {"8bit"},
			},
		},
		Preamble: "This is a MIME control message.",
		Parts:    parts,
	}).Sign(myKey)
}
