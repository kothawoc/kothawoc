package nntpbackend

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
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

	//key, _ := dbs.ConfigGetGetBytes("deviceKey")

	key, _ := dbs.ConfigGetDeviceKey()
	tpk, _ := key.TorPrivKey()
	nDBs := peering.BackendDbs{
		Articles:      dbs.articles,
		Config:        dbs.config,
		Groups:        dbs.groups,
		Peers:         dbs.peers,
		GroupArticles: dbs.groupArticles,
	}

	peers, err := peering.NewPeers(dbs.peers, tc, tpk, nDBs)
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

type DBs struct {
	*backendDbs
}

type NntpBackend struct {
	ConfigPath string
	Peers      *peering.Peers
	DBs        *backendDbs
}

func (be *NntpBackend) ListGroups(session map[string]string) (<-chan *nntp.Group, error) {

	slog.Info("E ListGroups")

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

			slog.Info("Get grouplist", "id", id, "name", name)
			if err != nil {
				slog.Info("Error in grouplist", "error", err)
				return
			}
			if perms := be.DBs.GetPerms(session["Id"], name); perms != nil && !perms.Read {
				//	if !be.DBs.GetPerms(session["Id"], name).Read {
				continue
			}

			grp, err := be.GetGroup(session, name)

			if err != nil {

				slog.Info("Error 2 in grouplist", "error", err)
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
	slog.Info("E GetGroup", "id", session["Id"])

	if perms := be.DBs.GetPerms(session["Id"], groupName); perms != nil && !perms.Read {

		//	if !be.DBs.GetPerms(session["Id"], groupName).Read {
		return nil, nntpserver.ErrNoSuchGroup
	}

	if articles, ok := be.DBs.groupArticles[groupName]; ok {

		row := articles.QueryRow("SELECT val FROM config WHERE key=\"description\"")
		var description string
		err := row.Scan(&description)
		if err != nil {
			slog.Info("FAILED 1 Query get group description", "groupName", groupName, "description", description, "error", err)
			return nil, nntpserver.ErrNoSuchGroup
		}

		row = articles.QueryRow("SELECT val FROM config WHERE key=\"flags\"")
		var flags string
		err = row.Scan(&flags)
		if err != nil {
			slog.Info("FAILED 2 Query get group description", "groupName", groupName, "description", description, "error", err)
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
			slog.Info("FAIL GetGroup count scan", "groupName", groupName, "error", err)
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

		slog.Info("E GetGroup returning", "ret", ret)
		return ret, nil
	}

	slog.Info("E GetGroup not found", "groupName", groupName, "groupdb", be.DBs.groupArticles)
	return nil, nntpserver.ErrNoSuchGroup
}

func (be *NntpBackend) GetArticleWithNoGroup(session map[string]string, id string) (*nntp.Article, error) {

	slog.Info("E GetArticleWithNoGroup")

	return nil, nntpserver.ErrInvalidArticleNumber
}

/*
TODO: Check security and if the user is allowed to view the groups
*/
func (be *NntpBackend) GetArticle(session map[string]string, group *nntp.Group, grpMsgId string) (*nntp.Article, error) {

	slog.Info("GetArticle", "group", group, "grpMsgId", grpMsgId)

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

	slog.Info("GetArticle  SQL", "query", query)

	row := articles.QueryRow(query, grpMsgId)

	id := int64(0)
	messageid := ""
	signature := ""
	err := row.Scan(&id, &messageid, &signature)
	if err != nil {
		slog.Info("Failed to open article final row scan [%s] [%v]", grpMsgId, err)
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

	slog.Info("GetArticle return", "article", article, "error", err)

	return article, nil
}

func (be *NntpBackend) GetArticles(session map[string]string, group *nntp.Group, from, to int64) (<-chan nntpserver.NumberedArticle, error) {

	slog.Info("E GetArticles")
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

			slog.Info("Get Articles Scan", "id", id)
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
	slog.Info("E Authorized")
	return true
}

func (be *NntpBackend) Authenticate(session map[string]string, user, pass string) (nntpserver.Backend, error) {
	slog.Info("E Authenticate")
	return nil, nil
}

func (be *NntpBackend) AllowPost(session map[string]string) bool {
	slog.Info("E AllowPost")
	return true
}

func (be *NntpBackend) Post(session map[string]string, article *nntp.Article) error {
	slog.Info("E Post")

	msg := messages.NewMessageToolFromArticle(article)

	// if the connection is local, sign it.
	if session["ConnMode"] == ConnModeLocal || session["ConnMode"] == ConnModeTcp {
		sig := msg.Article.Header.Get(messages.SignatureHeader)
		if sig == "" {
			slog.Info("Signing new posted message")
			deviceKey, _ := be.DBs.ConfigGetGetBytes("deviceKey")
			if msg.Article.Header.Get("Date") == "" {
				msg.Article.Header.Set("Date", time.Now().UTC().Format(time.RFC1123Z))
			}

			kt := keytool.EasyEdKey{}
			kt.SetTorPrivateKey(ed25519.PrivateKey(deviceKey))

			msg.Sign(kt)
		}
	}
	slog.Info("Posting", "session", session, "msg", msg)

	// reject all non signed and verified articles.
	if !msg.Verify() {
		slog.Info("Error Posting, failed to verify message")
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
			slog.Info("ADDPATH LOC EMPTY", "connmode", session["ConnMode"], "path", path)
			path = torId + "!.POSTED"
		} else { // This shouldn't happen, but if it does at least we know about it.
			slog.Info("ADDPATH LOC FULL", "connmode", session["ConnMode"], "path", path)
			path = torId + "!.POSTED!" + path
		}
	} else {
		if path == "" {
			slog.Info("ADDPATH TOR EMPTY", "connmode", session["ConnMode"], "path", path)
			slog.Info("Error Path header should not be empty from a peer")
			return nntpserver.ErrPostingNotPermitted
		} else {

			slog.Info("ADDPATH TOR FULL", "connmode", session["ConnMode"], "path", path)
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
		Sendme:     be.Peers.Sendme,
	}

	if err := messages.CheckControl(msg, cmf, session); err != nil {

		slog.Info("ERROR POST Control message failed", "error", err)
		return nntpserver.ErrPostingFailed
	}

	slog.Info("SUCCESS POST Control message.")

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
			slog.Info("FAILED POST article find group", "id", id, "group", group, "error", err)

		}

		if id != 0 {
			slog.Info("Postable group!!", "group", group)
			postableGroups[group] = id
		}
	}

	if len(postableGroups) > 0 {
		slog.Info("Post try of", "messageId", article.Header.Get("Message-Id"))

		slog.Info("Post preamble: to post!!", "preamble", msg.Preamble)

		// check if it's a local connection before signing it
		// TODO check if local device to sign it.
		deviceKey, _ := be.DBs.ConfigGetGetBytes("deviceKey")

		kt := keytool.EasyEdKey{}
		kt.SetTorPrivateKey(ed25519.PrivateKey(deviceKey))

		msg.Sign(kt)
		//verified := msg.Verify()

		signature := article.Header.Get(messages.SignatureHeader)
		messageId := article.Header.Get("Message-Id")
		insert := `INSERT INTO articles(messageid,signature,refs) VALUES(?,?,?);`

		res, err := be.DBs.articles.Exec(insert, messageId, signature, len(postableGroups))
		if err != nil {
			slog.Info("Ouch abc Error insert article to do db stuff at", "error", err, "messageId", article.Header.Get("Message-Id"))
			return err
		} else {
			slog.Info("SUCCESS  insert article to do db stuff at", "error", err, "messageId", article.Header.Get("Message-Id"))

		}

		articleId, err := res.LastInsertId()
		if err != nil {
			slog.Info("Error getting inserted rowid to do db stuff at", "error", err)
			return err
		}

		slog.Info("Last inserted rowid to do db stuff at ", "articleId", articleId)

		err = os.WriteFile(be.ConfigPath+"/articles/"+signature, []byte(msg.RawMail()), 0600)

		if err != nil {
			slog.Info("Error writing file Ouch def Error insert article to do db stuff at", "error", err, "messageId", article.Header.Get("Message-Id"))
			return err
		}

		be.Peers.DistributeArticle(*msg)

		for group := range postableGroups {
			insert := "INSERT INTO articles(id,messageid) VALUES(?,?);"

			_, err = be.DBs.groupArticles[group].Exec(insert, articleId, messageId)
			if err != nil {
				slog.Info("Ouch def Error insert article to do db stuff at", "error", err, "messageId", article.Header.Get("Message-Id"))
				return err
			} else {
				slog.Info("SUCCESS  insert article to do db stuff at", "error", err, "messageId", article.Header.Get("Message-Id"))
			}

			row := be.DBs.articles.QueryRow("UPDATE articles SET refs=refs + 1 WHERE messageid=? RETURNING refs;", messageId)
			refs := int64(0)
			err = row.Scan(&refs)
			if err != nil {
				slog.Info("Ouch update refs def Error insert article to do db stuff at", "error", err, "messageId", article.Header.Get("Message-Id"))
				return err
			} else {
				slog.Info("SUCCESS update refs insert article to do db stuff at", "error", err, "messageId", article.Header.Get("Message-Id"))
			}

		}

		slog.Info("Post Success of", "messageid", article.Header.Get("Message-Id"))

		return nil
	}

	return nntpserver.ErrPostingFailed
}
