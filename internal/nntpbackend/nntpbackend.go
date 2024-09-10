package nntpbackend

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cretz/bine/torutil/ed25519"
	_ "github.com/mattn/go-sqlite3"

	"github.com/kothawoc/go-nntp"
	nntpserver "github.com/kothawoc/go-nntp/server"
	"github.com/kothawoc/kothawoc/internal/databases"
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

func NewNNTPBackend(path string, tc *torutils.TorCon, dbs *databases.BackendDbs) (*EmptyNntpBackend, error) {

	key, _ := dbs.ConfigGetDeviceKey()

	peers, err := peering.NewPeers(tc, key, dbs)
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
	*databases.BackendDbs
}

type NntpBackend struct {
	ConfigPath string
	Peers      *peering.Peers
	DBs        *databases.BackendDbs
}

func (be *NntpBackend) ListGroups(session map[string]string) (<-chan *nntp.Group, error) {

	slog.Info("E ListGroups")

	a, b := be.DBs.ListGroups(session)

	return a, b

}

func (be *NntpBackend) GetGroup(session map[string]string, groupName string) (*nntp.Group, error) {
	slog.Info("E GetGroup", "id", session["Id"])

	if perms := be.DBs.GetPerms(session["Id"], groupName); perms != nil && !perms.Read {

		//	if !be.DBs.GetPerms(session["Id"], groupName).Read {
		return nil, nntpserver.ErrNoSuchGroup
	}

	a, b := be.DBs.GetGroup(session, groupName)

	return a, b

}

func (be *NntpBackend) GetArticleWithNoGroup(session map[string]string, id string) (*nntp.Article, error) {

	slog.Info("E GetArticleWithNoGroup")

	ret, err := be.DBs.GetArticleById(id)

	return ret, err

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

	ret, err := be.DBs.GetArticleById(grpMsgId)

	return ret, err

}

func (be *NntpBackend) GetArticles(session map[string]string, group *nntp.Group, from, to int64) (<-chan nntpserver.NumberedArticle, error) {

	slog.Info("E GetArticles")
	if perms := be.DBs.GetPerms(session["Id"], group.Name); perms != nil && !perms.Read {
		//if !be.DBs.GetPerms(session["Id"], group.Name).Read {
		return nil, nntpserver.ErrInvalidArticleNumber
	}

	list, err := be.DBs.ListArticles(session, group.Name, from, to)

	retChan := make(chan nntpserver.NumberedArticle, 10)

	go func() {
		for id := range list {
			//		err := row.Scan(&id)

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
			deviceKey, _ := be.DBs.ConfigGetBytes("deviceKey")
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

	deviceKey, _ := be.DBs.ConfigGetBytes("deviceKey")

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

		/*
			row := be.DBs.groups.QueryRow("SELECT id,name FROM groups WHERE name=?;", group)

			var name string
			var id int64
			err := row.Scan(&id, &name)
		*/
		id, err := be.DBs.GetGroupNumber(group)

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
		//	deviceKey, _ := be.DBs.ConfigGetBytes("deviceKey")

		//	kt := keytool.EasyEdKey{}
		//	kt.SetTorPrivateKey(ed25519.PrivateKey(deviceKey))

		//kt, _ := be.DBs.ConfigGetDeviceKey()

		//msg.Sign(kt)

		//verified := msg.Verify()

		articleId, err := be.DBs.StoreArticle(msg)
		if err != nil {
			return err
		}

		be.Peers.DistributeArticle(*msg)

		for group := range postableGroups {

			err := be.DBs.AddArticleToGroup(group, article.Header.Get("Message-Id"), articleId)
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
