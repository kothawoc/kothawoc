package nntpbackend

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/mail"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cretz/bine/torutil/ed25519"
	_ "github.com/mattn/go-sqlite3"

	"github.com/kothawoc/go-nntp"
	nntpserver "github.com/kothawoc/go-nntp/server"
	"github.com/kothawoc/kothawoc/internal/peering"
	"github.com/kothawoc/kothawoc/internal/torutils"
	"github.com/kothawoc/kothawoc/pkg/keytool"
	"github.com/kothawoc/kothawoc/pkg/messages"
	serr "github.com/kothawoc/kothawoc/pkg/serror"
)

const (
	ConnModeTor   string = "TOR"
	ConnModeTcp   string = "TCP"
	ConnModeLocal string = "LOCAL"
)

/*
// The Backend that provides the things and does the stuff.

	type Backend interface {
		// gets a list of NNTP newsgroups.
		ListGroups() (<-chan *nntp.Group, error)
		GetGroup(name string) (*nntp.Group, error)
		// DONE: Add a way for Article Downloading without group select
		// if not to implement DO: return nil, ErrNoGroupSelected
		GetArticleWithNoGroup(id string) (*nntp.Article, error)
		GetArticle(group *nntp.Group, id string) (*nntp.Article, error)
		// old: GetArticles(group *nntp.Group, from, to int64) ([]NumberedArticle, error)
		// channels are more suitable for large scale
		GetArticles(group *nntp.Group, from, to int64) (<-chan NumberedArticle, error)
		Authorized() bool
		// Authenticate and optionally swap out the backend for this session.
		// You may return nil to continue using the same backend.
		Authenticate(user, pass string) (Backend, error)
		AllowPost() bool
		Post(article *nntp.Article) error
	}
*/

func NewNNTPBackend(path string, tc *torutils.TorCon) (*EmptyNntpBackend, error) {

	os.MkdirAll(fmt.Sprintf("%s/articles", path), 0700)
	dbs, err := NewBackendDBs(path)

	if err != nil {
		return nil, serr.New(err)
	}
	//	dbs.NewGroup("alt.misc.test", "Alt misc test group", "y")
	//	dbs.NewGroup("misc.test", "Alt misc test group", "y")
	//	dbs.NewGroup("alt.test", "Alt misc test group", "y")

	//	np := NewPeers()
	//	cmf := messages.ControMesasgeFunctions{
	//		NewGroup: be.DBs.NewGroup,
	//		AddPeer:  np.AddPeer,

	key, _ := dbs.ConfigGetGetBytes("deviceKey")

	peers, err := peering.NewPeers(dbs.peers, tc, ed25519.PrivateKey(key))
	if err != nil {
		return nil, serr.New(err)
	}
	go peers.Connect()

	nextBackend := &NntpBackend{
		ConfigPath: path,
		Peers:      peers,
		DBs:        dbs,
	}

	return &EmptyNntpBackend{
		ConfigPath:  path,
		Peers:       peers,
		DBs:         dbs,
		NextBackend: nextBackend,
	}, nil

}

type NntpBackend struct {
	ConfigPath string
	Peers      *peering.Peers
	DBs        *backendDbs
}

func (be *NntpBackend) ListGroups(session map[string]string) (<-chan *nntp.Group, error) {

	log.Print(serr.Errorf("E ListGroups"))

	retChan := make(chan *nntp.Group)

	row, err := be.DBs.groups.Query("SELECT id, name FROM groups;")
	if err != nil {
		return nil, serr.New(err)
	}
	id := int64(0)
	name := ""
	go func() {
		for row.Next() {
			err := row.Scan(&id, &name)

			log.Print(serr.Errorf("Get grouplist [%d][%s]", id, name))
			if err != nil {
				log.Print(serr.Errorf("Error in grouplist [%v]", err))
				return
			}
			if perms := be.DBs.GetPerms(session["Id"], name); perms != nil && !perms.Read {
				//	if !be.DBs.GetPerms(session["Id"], name).Read {
				continue
			}

			grp, err := be.GetGroup(session, name)

			if err != nil {

				log.Print(serr.Errorf("Error 2 in grouplist [%v]", err))
				return
				//	return nil, err
			}

			retChan <- grp

		}
		close(retChan)
	}()

	return retChan, nil
}

func (be *NntpBackend) GetGroup(session map[string]string, groupName string) (*nntp.Group, error) {
	log.Print(serr.Errorf("E GetGroup", session["Id"]))

	if perms := be.DBs.GetPerms(session["Id"], groupName); perms != nil && !perms.Read {

		//	if !be.DBs.GetPerms(session["Id"], groupName).Read {
		return nil, nntpserver.ErrNoSuchGroup
	}

	if articles, ok := be.DBs.groupArticles[groupName]; ok {

		row := articles.QueryRow("SELECT val FROM config WHERE key=\"description\"")
		var description string
		err := row.Scan(&description)
		if err != nil {
			log.Print(serr.Errorf("FAILED 1 Query get group description [%s][%s][%#v]", groupName, description, err))
			return nil, nntpserver.ErrNoSuchGroup
		}

		row = articles.QueryRow("SELECT val FROM config WHERE key=\"flags\"")
		var flags string
		err = row.Scan(&flags)
		if err != nil {
			log.Print(serr.Errorf("FAILED 2 Query get group description [%s][%s][%#v]", groupName, description, err))
			return nil, nntpserver.ErrNoSuchGroup
		}
		posting := nntp.PostingPermitted
		if flags == "n" {
			posting = nntp.PostingNotPermitted
		}
		if flags == "m" {
			posting = nntp.PostingModerated
		}

		var high, low, count int64
		row = articles.QueryRow(`SELECT
  			COALESCE((SELECT id FROM articles ORDER BY id DESC LIMIT 1), 0) AS high,
    		COALESCE((SELECT id FROM articles ORDER BY id ASC LIMIT 1), 0) AS low,
    		COALESCE((SELECT COUNT(id) FROM articles), 0) AS count;`)
		err = row.Scan(&high, &low, &count)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Print(serr.Errorf("FAIL GetGroup count scan [%s][%#v]", groupName, err))
			return nil, err
		}

		ret := &nntp.Group{
			Name:        groupName,
			Description: description,
			Count:       count,
			Low:         low,
			High:        high,
			Posting:     posting,
		}

		log.Print(serr.Errorf("E GetGroup returning [%#v]", ret))
		return ret, nil
	}

	log.Print(serr.Errorf("E GetGroup not found [%s][%#v]", groupName, be.DBs.groupArticles))
	return nil, nntpserver.ErrNoSuchGroup
}

func (be *NntpBackend) GetArticleWithNoGroup(session map[string]string, id string) (*nntp.Article, error) {

	log.Print(serr.Errorf("E GetArticleWithNoGroup"))

	return nil, nntpserver.ErrInvalidArticleNumber
}

/*
TODO: Check security and if the user is allowed to view the groups
*/
func (be *NntpBackend) GetArticle(session map[string]string, group *nntp.Group, grpMsgId string) (*nntp.Article, error) {

	log.Print(serr.Errorf("GetArticle [%v] [%s]", group, grpMsgId))

	if !be.DBs.GetPerms(session["Id"], group.Name).Read {
		return nil, nntpserver.ErrInvalidArticleNumber
	}
	// TODO, check the actual articles DB config to see
	//       if the user can view the article.
	// and it is actually in the db
	//articles := be.DBs.groupArticles[group.Name]
	articles := be.DBs.articles

	query := ""
	// if the id is an int, get the message id
	if _, err := strconv.ParseInt(grpMsgId, 10, 64); err == nil {
		query = "SELECT id, messageid, signature FROM articles WHERE id=?"
	} else {
		query = "SELECT id, messageid, signature FROM articles WHERE messageid=?"
	}

	log.Print(serr.Errorf("GetArticle  SQL[%s]", query))

	row := articles.QueryRow(query, grpMsgId)

	id := int64(0)
	messageid := ""
	signature := ""
	err := row.Scan(&id, &messageid, &signature)
	if err != nil {
		log.Print(serr.Errorf("Failed to open article final row scan [%s] [%v]", grpMsgId, err))
		return nil, nntpserver.ErrInvalidArticleNumber
	}

	message, err := os.ReadFile(be.ConfigPath + "/articles/" + signature)
	if err != nil {
		return nil, err
	}
	body := (strings.SplitN(string(message), "\r\n\r\n", 2))[1]
	msg, err := mail.ReadMessage(bytes.NewReader(message))
	if err != nil {
		return nil, err
	}

	article := &nntp.Article{
		Header: textproto.MIMEHeader(msg.Header),
		Body:   msg.Body,
		Bytes:  len([]byte(body)),
		Lines:  strings.Count(body, "\n"),
	}

	log.Print(serr.Errorf("GetArticle return [%v] [%v]", article, err))

	return article, nil
}

func (be *NntpBackend) GetArticles(session map[string]string, group *nntp.Group, from, to int64) (<-chan nntpserver.NumberedArticle, error) {

	log.Print(serr.Errorf("E GetArticles"))
	if perms := be.DBs.GetPerms(session["Id"], group.Name); perms != nil && !perms.Read {
		//if !be.DBs.GetPerms(session["Id"], group.Name).Read {
		return nil, nntpserver.ErrInvalidArticleNumber
	}
	retChan := make(chan nntpserver.NumberedArticle, 10)

	if from > to {
		a := from
		to = from
		from = a
	}

	row, err := be.DBs.groupArticles[group.Name].Query("SELECT id FROM articles WHERE id>=? and id<=?;", from, to)
	if err != nil {
		return nil, err
	}
	id := int64(0)
	go func() {
		for row.Next() {
			err := row.Scan(&id)

			log.Print(serr.Errorf("Get Articles Scan [%d]", id))
			if err != nil {
				return // nil, err
			}

			article, err := be.GetArticle(session, group, fmt.Sprintf("%d", id))

			if err != nil {
				return //nil, err
			}

			retChan <- nntpserver.NumberedArticle{
				Article: article,
				Num:     id,
			}

		}
		close(retChan)
	}()

	return retChan, nil
}

func (be *NntpBackend) Authorized(session map[string]string) bool {
	log.Print(serr.Errorf("E Authorized"))
	return true
}

func (be *NntpBackend) Authenticate(session map[string]string, user, pass string) (nntpserver.Backend, error) {
	log.Print(serr.Errorf("E Authenticate"))
	return nil, nil
}

func (be *NntpBackend) AllowPost(session map[string]string) bool {
	log.Print(serr.Errorf("E AllowPost"))
	return true
}

func (be *NntpBackend) Post(session map[string]string, article *nntp.Article) error {
	log.Print(serr.Errorf("E Post"))

	msg := messages.NewMessageToolFromArticle(article)

	// if the connection is local, sign it.
	if session["ConnMode"] == ConnModeLocal || session["ConnMode"] == ConnModeTcp {
		sig := msg.Article.Header.Get(messages.SignatureHeader)
		if sig == "" {
			log.Print(serr.Errorf("Signing new posted message"))
			deviceKey, _ := be.DBs.ConfigGetGetBytes("deviceKey")
			if msg.Article.Header.Get("Date") == "" {
				msg.Article.Header.Set("Date", time.Now().UTC().Format(time.RFC1123Z))
			}
			msg.Sign(deviceKey)
		}
	}
	log.Print(serr.Errorf("##################################################################################\nGot post [%#v]\n#################################[%s]", session, msg))

	// reject all non signed and verified articles.
	if !msg.Verify() {
		log.Print(serr.Errorf("Error Posting, failed to verify message"))
		return nntpserver.ErrPostingNotPermitted
	}

	deviceKey, _ := be.DBs.ConfigGetGetBytes("deviceKey")

	//	torId := torutils.EncodePublicKey(ed25519.PrivateKey(deviceKey).PublicKey())
	myKey := keytool.EasyEdKey{}
	myKey.SetTorPrivateKey(ed25519.PrivateKey(deviceKey))
	torId, err := myKey.TorId()
	if err != nil {
		return nntpserver.ErrPostingFailed
	}

	path := msg.Article.Header.Get("Path")
	if session["ConnMode"] == ConnModeTcp ||
		session["ConnMode"] == ConnModeLocal {
		if path == "" {
			log.Print(serr.Errorf("ADDPATH LOC EMPTY md[%s] path[%s]", session["ConnMode"], path))
			path = torId + "!.POSTED"
		} else { // This shouldn't happen, but if it does at least we know about it.
			log.Print(serr.Errorf("ADDPATH LOC FULL md[%s] path[%s]", session["ConnMode"], path))
			path = torId + "!.POSTED!" + path
		}
	} else {
		if path == "" {
			log.Print(serr.Errorf("ADDPATH TOR EMPTY md[%s] path[%s]", session["ConnMode"], path))
			log.Print(serr.Errorf("Error Path header should not be empty from a peer"))
			return nntpserver.ErrPostingNotPermitted
		} else {

			log.Print(serr.Errorf("ADDPATH TOR FULL md[%s] path[%s]", session["ConnMode"], path))
			path = torId + "!" + path
		}
	}
	msg.Article.Header.Set("Path", path)

	//np, _ := NewPeers(be.DBs.peers,be.)
	cmf := messages.ControMesasgeFunctions{
		NewGroup:   be.DBs.NewGroup,
		AddPeer:    be.Peers.AddPeer,
		RemovePeer: be.Peers.RemovePeer,
		Cancel:     be.DBs.CancelMessage,
	}

	if err := messages.CheckControl(msg, cmf); err != nil {

		log.Print(serr.Errorf("ERROR POST Control message failed[%#v]", err))
		return nntpserver.ErrPostingFailed
	}

	log.Print(serr.Errorf("SUCCESS POST Control message."))

	//	if ctrl := msg.Article.Header.Get("Control"); ctrl != "" {
	//		checkControl(msg)
	//
	//	}

	splitGroups := strings.Split(article.Header.Get("Newsgroups"), ",")
	postableGroups := map[string]int64{}

	for _, group := range splitGroups {
		group := strings.TrimSpace(group)
		if post := be.DBs.GetPerms(session["Id"], group); post != nil && !post.Post {
			continue
		}
		row := be.DBs.groups.QueryRow("SELECT id,name FROM groups WHERE name=?;", group)

		var name string
		var id int64
		err := row.Scan(&id, &name)

		if err != nil {
			log.Print(serr.Errorf("FAILED POST article find group [%d][%s][%#v]", id, group, err))

		}

		if id != 0 {
			log.Print(serr.Errorf("Postable group!! %s", group))
			postableGroups[group] = id
		}
	}

	if len(postableGroups) > 0 {
		log.Print(serr.Errorf("Post try of [%v]", article.Header.Get("Message-Id")))

		log.Print(serr.Errorf("Post preamble: [%s] to post!!", msg.Preamble))

		// check if it's a local connection before signing it
		// TODO check if local device to sign it.
		deviceKey, _ := be.DBs.ConfigGetGetBytes("deviceKey")
		msg.Sign(deviceKey)
		//verified := msg.Verify()

		signature := article.Header.Get(messages.SignatureHeader)
		messageId := article.Header.Get("Message-Id")
		insert := `INSERT INTO articles(messageid,signature,refs) VALUES(?,?,?);`

		res, err := be.DBs.articles.Exec(insert, messageId, signature, len(postableGroups))
		if err != nil {
			log.Print(serr.Errorf("Ouch abc Error insert article to do db stuff at [%v] [%s]", err, article.Header.Get("Message-Id")))
			return err
		} else {
			log.Print(serr.Errorf("SUCCESS  insert article to do db stuff at [%v] [%s]", err, article.Header.Get("Message-Id")))

		}

		articleId, err := res.LastInsertId()
		if err != nil {
			log.Print(serr.Errorf("Error getting inserted rowid to do db stuff at [%v]", err))
			return err
		}

		log.Print(serr.Errorf("Last inserted rowid to do db stuff at [%v]", articleId))

		err = os.WriteFile(be.ConfigPath+"/articles/"+signature, []byte(msg.RawMail()), 0600)

		if err != nil {
			log.Print(serr.Errorf("Error writing file Ouch def Error insert article to do db stuff at [%v] [%s]", err, article.Header.Get("Message-Id")))
			return err
		}

		be.Peers.DistributeArticle(*msg)

		for group := range postableGroups {
			insert := "INSERT INTO articles(id,messageid) VALUES(?,?);"

			_, err = be.DBs.groupArticles[group].Exec(insert, articleId, messageId)
			if err != nil {
				log.Print(serr.Errorf("Ouch def Error insert article to do db stuff at [%v] [%s]", err, article.Header.Get("Message-Id")))
				return err
			} else {
				log.Print(serr.Errorf("SUCCESS  insert article to do db stuff at [%v] [%s]", err, article.Header.Get("Message-Id")))
			}

			row := be.DBs.articles.QueryRow("UPDATE articles SET refs=refs + 1 WHERE messageid=? RETURNING refs;", messageId)
			refs := int64(0)
			err = row.Scan(&refs)
			if err != nil {
				log.Print(serr.Errorf("Ouch update refs def Error insert article to do db stuff at [%v] [%s]", err, article.Header.Get("Message-Id")))
				return err
			} else {
				log.Print(serr.Errorf("SUCCESS update refs insert article to do db stuff at [%v] [%s]", err, article.Header.Get("Message-Id")))
			}

		}

		log.Print(serr.Errorf("Post Success of [%v]", article.Header.Get("Message-Id")))

		return nil
	}

	return nntpserver.ErrPostingFailed
}
