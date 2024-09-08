package nntpbackend

import (
	"bytes"
	"database/sql"
	"fmt"
	"log/slog"
	"net/mail"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cretz/bine/torutil/ed25519"
	vcard "github.com/emersion/go-vcard"
	_ "github.com/mattn/go-sqlite3"

	"github.com/kothawoc/go-nntp"
	nntpserver "github.com/kothawoc/go-nntp/server"
	"github.com/kothawoc/kothawoc/pkg/keytool"
	"github.com/kothawoc/kothawoc/pkg/messages"
	serr "github.com/kothawoc/kothawoc/pkg/serror"
)

type backendDbs struct {
	path                            string
	articles, config, groups, peers *sql.DB
	groupArticles                   map[string]*sql.DB
	groupArticlesName2Int           map[string]int64
	groupArticlesName2Hex           map[string]string
}

// the pubkey isn't known until after the first handshake, but we still
// want to insert the record.
const createPeersDB string = `
CREATE TABLE IF NOT EXISTS peers (
	id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	torid TEXT NOT NULL UNIQUE,
	pubkey TEXT NOT NULL,
	name TEXT NOT NULL
	);
`

const createConfigDB string = `
CREATE TABLE IF NOT EXISTS config (
	key TEXT NOT NULL UNIQUE,
	val BLOB
	);
`

const createGroupsDB string = `
CREATE TABLE IF NOT EXISTS groups (
	id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE
	);
INSERT INTO groups(id,name)
	VALUES(?,"DELETEME");
DELETE FROM groups WHERE name="DELETEME";
`

// TODO: rename messagehash to signature
const createArticlesDB string = `
CREATE TABLE IF NOT EXISTS articles (
	id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	messageid TEXT NOT NULL UNIQUE,
	signature TEXT NOT NULL,
	refs INTEGER NOT NULL DEFAULT 0
	);
INSERT INTO articles(id,messageid,signature,refs)
	VALUES(?,"DELETEME","1",0);
DELETE FROM articles WHERE messageID="DELETEME";
`

const createArticleIndexDB string = `
CREATE TABLE IF NOT EXISTS articles (
	id INTEGER NOT NULL,
	messageid TEXT NOT NULL UNIQUE
	);
CREATE TABLE IF NOT EXISTS subscriptions (
	peer TEXT NOT NULL UNIQUE,
	lastmsg INTEGER NOT NULL DEFAULT 0
    );
CREATE TABLE IF NOT EXISTS config (
	key TEXT NOT NULL UNIQUE,
	val BLOB
	);
CREATE TABLE IF NOT EXISTS perms (
	torid TEXT NOT NULL UNIQUE,
	read BOOLEAN DEFAULT FALSE,
	reply BOOLEAN DEFAULT FALSE,
	post BOOLEAN DEFAULT FALSE,
	cancel BOOLEAN DEFAULT FALSE,
	supersede BOOLEAN DEFAULT FALSE
	);
`

func openCreateDB(path, sqlQuery string) (*sql.DB, error) {

	t := time.Now().Unix()

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, serr.New(err)
	}

	if _, err := db.Exec(sqlQuery, t); err != nil {
		slog.Info("FAILED Create DB database query", "error", err, "path", path, "query", sqlQuery)
		return nil, serr.New(err)
	}

	return db, nil
}

func NewBackendDBs(path string) (*backendDbs, error) {

	dbs := &backendDbs{path: path}

	os.MkdirAll(path+"/groups", 0700)

	//db, err := sql.Open("sqlite3", path+"/articles.db")
	db, err := openCreateDB(path+"/articles.db", createArticlesDB)
	if err != nil {
		return nil, serr.New(err)
	}
	dbs.articles = db

	db, err = openCreateDB(path+"/config.db", createConfigDB)
	if err != nil {
		return nil, serr.New(err)
	}
	dbs.config = db

	db, err = openCreateDB(path+"/groups.db", createGroupsDB)
	if err != nil {
		return nil, serr.New(err)
	}
	dbs.groups = db

	db, err = openCreateDB(path+"/peers.db", createPeersDB)
	if err != nil {
		return nil, serr.New(err)
	}
	dbs.peers = db

	dbs.groupArticles = map[string]*sql.DB{}
	dbs.groupArticlesName2Int = map[string]int64{}
	dbs.groupArticlesName2Hex = map[string]string{}

	dbs.OpenGroups()

	return dbs, nil
}

func (dbs *backendDbs) OpenGroups() error {

	groupsQuery := `SELECT 
		id, name
		FROM groups;`

	rows, err := dbs.groups.Query(groupsQuery)
	if err != nil {
		return serr.New(err)
	}

	id := int64(0)
	name := ""

	slog.Info("EOpening groups do db stuff at ")
	for rows.Next() {
		err := rows.Scan(&id, &name)

		slog.Info("Open grouplist", "id", id, "name", name)
		if err != nil {
			return serr.New(err)
		}

		db, err := sql.Open("sqlite3", fmt.Sprintf("%s/groups/%x.db", dbs.path, id))
		if err != nil {
			slog.Info("Error OpenGroup o do db stuff at]", "db", db, "error", err)
			return serr.New(err)
		}

		dbs.groupArticles[name] = db
		dbs.groupArticlesName2Int[name] = id
		dbs.groupArticlesName2Hex[name] = strconv.FormatInt(id, 16)

	}
	return nil

}

type PermissionsGroupT struct {
	Read, Reply, Post, Cancel, Supersede bool
}

func (dbs *backendDbs) GetPerms(torid, group string) *PermissionsGroupT {
	slog.Info("E GetPerms", "torid", torid, "group", group)

	p := &PermissionsGroupT{}

	gs := strings.Split(group, ".")[0]
	if gs == torid {
		slog.Info("E GetPerms HERE BE GOD", "torid", torid, "group", group)
		return &PermissionsGroupT{
			Read:      true,
			Reply:     true,
			Post:      true,
			Cancel:    true,
			Supersede: true,
		}
	}

	row := dbs.groups.QueryRow("SELECT id FROM groups;")
	id := int64(0)
	err := row.Scan(&id)
	if err != nil {
		slog.Info("E GetPerms failgroup", "torid", torid, "group", group, "error", err)
		return nil
	}

	if _, found := dbs.groupArticles[group]; !found {
		return p
	}
	row = dbs.groupArticles[group].QueryRow("SELECT read,reply,post,cancel,supersede FROM perms WHERE torid=?;", torid)

	err = row.Scan(&p.Read, &p.Reply, &p.Post, &p.Cancel, &p.Supersede)
	if err != nil && err == sql.ErrNoRows {
		slog.Info("E GetPerms fail get other siht", "torid", torid, "group", group, "error", err)
		row = dbs.groupArticles[group].QueryRow("SELECT read,reply,post,cancel,supersede FROM perms WHERE torid=?;", "group")
		err = row.Scan(&p.Read, &p.Reply, &p.Post, &p.Cancel, &p.Supersede)
		if err == nil {
			return p
		}
		return nil
	}

	return p
}

func (dbs *backendDbs) NewGroup(name, description string, card vcard.Card) error {

	res, err := dbs.groups.Exec("INSERT INTO groups(name) VALUES(?);", name)
	if err != nil {
		slog.Info("Error NewGroup INSERT to do db stuff at insert", "error", err)
		return serr.New(err)
	}

	groupId, err := res.LastInsertId()
	if err != nil {
		slog.Info("Error getting inserted rowid to do db stuff at last id", "error", err)
		return serr.New(err)
	}

	slog.Info("Last inserted rowid to do db stuff at", "groupid", groupId)

	db, err := sql.Open("sqlite3", fmt.Sprintf("%s/groups/%x.db", dbs.path, groupId))
	if err != nil {
		slog.Info("Error opening NewGroup database id", "name", name, "error", err)
		return serr.New(err)
	}

	if msg, err := db.Exec(createArticleIndexDB); err != nil {
		slog.Info("FAILED Create article index DB database query", "path", dbs.path, "error", err, "msg", msg, "createArticleIndexDB", createArticleIndexDB)
		return serr.New(err)
	}

	if msg, err := db.Exec("INSERT OR REPLACE INTO config (key, val) VALUES (?, ?)", "description", description); err != nil {
		slog.Info("FAILED Upserting group config value", "name", name, "description", description, "error", err, "msg", msg)
		return serr.New(err)
	}
	if msg, err := db.Exec("INSERT OR REPLACE INTO config (key, val) VALUES (?, ?)", "flags", "flags"); err != nil {
		slog.Info("FAILED Upserting group config value", "name", name, "description", description, "error", err, "msg", msg)
		return serr.New(err)
	}

	for _, v := range card["X-KW-PERMS"] {
		torid := v.Value
		read := v.Params.Get("READ") == "true"
		reply := v.Params.Get("REPLY") == "true"
		post := v.Params.Get("POST") == "true"
		cancel := v.Params.Get("CANCEL") == "true"
		supersede := v.Params.Get("SUPERSEDE") == "true"

		if msg, err := db.Exec("INSERT OR REPLACE INTO perms (torid,read,reply,post,cancel,supersede) VALUES (?,?,?,?,?,?)", torid, read, reply, post, cancel, supersede); err != nil {
			slog.Info("FAILED Upserting group config value", "name", name, "description", description, "error", err, "msh", msg)
			return serr.New(err)
		}
	}

	dbs.groupArticles[name] = db
	dbs.groupArticlesName2Int[name] = groupId
	dbs.groupArticlesName2Hex[name] = strconv.FormatInt(groupId, 16)

	return nil
}

func (dbs *backendDbs) GetArticleBySignature(signature string) (*nntp.Article, error) {

	message, err := os.ReadFile(dbs.path + "/articles/" + signature)
	if err != nil {
		slog.Info("GetArticleBySignature", "signature", signature, "error", err)
		return nil, serr.New(err)
	}
	body := (strings.SplitN(string(message), "\r\n\r\n", 2))[1]
	msg, err := mail.ReadMessage(bytes.NewReader(message))
	if err != nil {
		slog.Info("GetArticleBySignature", "signature", signature, "error", err)
		return nil, serr.New(err)
	}

	article := &nntp.Article{
		Header: textproto.MIMEHeader(msg.Header),
		Body:   msg.Body,
		Bytes:  len([]byte(body)),
		Lines:  strings.Count(body, "\n") + 1,
	}
	return article, nil
}

func (dbs *backendDbs) GetArticleById(msgId string) (*nntp.Article, error) {

	slog.Info("GetArticleById", "msgId", msgId)

	row := dbs.articles.QueryRow("SELECT id, messageid, signature FROM articles WHERE messageid=?", msgId)

	id := int64(0)
	messageid := ""
	signature := ""
	err := row.Scan(&id, &messageid, &signature)
	if err != nil {
		slog.Info("GetArticleById Failed to open article final row scan", "msgId", msgId, "error", err)
		return nil, serr.New(nntpserver.ErrInvalidArticleNumber)
	}

	article, err := dbs.GetArticleBySignature(signature)
	if err != nil {
		slog.Info("GetArticleById Failed to get article by signature", "msgId", msgId, "signature", signature, "error", err)
		return nil, serr.New(nntpserver.ErrInvalidArticleNumber)
	}

	slog.Info("GetArticle By Id return", "article", article, "error", err)

	return article, nil
}
func containsStr(elems []string, v string) bool {
	for _, s := range elems {
		if v == s {
			return true
		}
	}
	return false
}
func (dbs *backendDbs) CancelMessage(from, msgId, newsgroups string, cmf messages.ControMesasgeFunctions) error {
	// get a message by the id
	// check it's valid
	// if it is, loop through the newsgroups and delete them from the index
	// remove the message
	article, err := dbs.GetArticleById(msgId)
	if err != nil {
		slog.Info("CancelMessage [%v] ERROR GetArticleById[%v]", msgId, err)
		return serr.New(err)
	}

	if article.Header.Get("From") != from {
		slog.Info("CancelMessage [%v] ERROR from doesn't match article.", "from", from)
		return serr.Errorf("Cancel message from doesn't match article cancelMsg[%v] article[%v]", from, article.Header.Get("From"))
	}

	signature := article.Header.Get(messages.SignatureHeader)
	msgGroups := strings.Split(article.Header.Get("Newsgroups"), ",")
	delGroups := strings.Split(newsgroups, ",")

	slog.Info("CancelMessage", "msgGroups", msgGroups)

	for _, grp := range delGroups {
		// if the message is actually in the group that they want to delete
		if containsStr(msgGroups, grp) {
			// delete message

			cm := article.Header.Get("Control")
			splitGrp := strings.Split(grp, ".")
			switch splitGrp[1] {
			case "peers":
				peerId := strings.Split(cm, " ")[1]

				err := cmf.RemovePeer(peerId)
				if err != nil {

					slog.Info("CancelMessage: Failed to remove peer", "error", err, "peerId", peerId)
					return serr.New(err)
				}
			}

			scm := strings.Split(cm, " ")
			if scm[0] == "newsgroup" {
				// delete newsgroup!!

			}

			_, err = dbs.groupArticles[grp].Exec("DELETE FROM articles WHERE messageid=?;", msgId)
			if err != nil {
				slog.Info("CancelMessage: Ouch def Error insert article to do db stuff at", "error", err, "msgId", msgId)
				return serr.New(err)
			} else {
				slog.Info("CancelMessage: SUCCESS  insert article to do db stuff at", "error", err, "msgId", msgId)
			}

			row := dbs.articles.QueryRow("UPDATE articles SET refs=refs - 1 WHERE messageid=? RETURNING refs;", msgId)
			refs := int64(0)
			err = row.Scan(&refs)
			if err != nil {
				slog.Info("CancelMessage: Ouch update refs def Error insert article to do db stuff at", "error", err, "msgId", msgId)
				return serr.New(err)
			} else {
				slog.Info("CancelMessage: SUCCESS update refs insert article to do db stuff at", "error", err, "msgId", msgId)
			}

			if refs == 0 {
				// delete the article off disc
				err := os.Remove(dbs.path + "/articles/" + signature)
				if err != nil {
					slog.Info("CancelMessage", "Error", err, "msgId", msgId, "signature", signature)
					return serr.New(err)
				}

				_, err = dbs.articles.Exec("DELETE articles WHERE messageid=?;", msgId)
				if err != nil {
					slog.Info("CancelMessage: Delete from main DB Error", "Error", err, "msgId", msgId, "signature", signature)
					return serr.New(err)
				} else {
					slog.Info("CancelMessage: SUCCESS from main DB", "Error", err, "msgId", msgId, "signature", signature)
				}

			}
			//if  delGroups
		}
	}
	return nil
}

func (dbs *backendDbs) OpenArticlesDB(id int) (*sql.DB, error) {

	db, err := sql.Open("sqlite3", fmt.Sprintf("%s/groups/%x.db", dbs.path, id))
	if err != nil {
		slog.Info("FAILED Open OpenArticleDB Failed", "id", id, "error", err)
		return nil, serr.New(err)
	}

	if msg, err := db.Exec(createArticlesDB); err != nil {
		slog.Info("FAILED Create DB OpenArticleDB QUERY", "id", id, "error", err, "msg", msg)
		return db, serr.New(err)
	} else {
		slog.Info("SUCCESS Create DB OpenArticleDB QUERY", "id", id, "error", err, "msg", msg)
	}

	slog.Info("OpenArticleDB SUCCESS", "id", id)

	return db, nil
}

func (dbs *backendDbs) ConfigSet(key string, val interface{}) error {
	slog.Info("Attempting to uupsert key[%#v] val[%#v]", key, val)
	if msg, err := dbs.config.Exec("INSERT OR REPLACE INTO config (key, val) VALUES (?, ?)", key, val); err != nil {
		slog.Info("FAILED Upserting config value", "path", dbs.path, "error", err, "msg", msg, "query", createArticleIndexDB)
		return serr.New(err)
	}
	return nil
}

func (dbs *backendDbs) ConfigGetInt64(key string) (int64, error) {
	rows := dbs.config.QueryRow("SELECT val FROM config WHERE key=?", key)
	val := int64(0)
	if err := rows.Scan(&val); err != nil {
		return val, serr.New(err)
	}
	return val, nil
}

func (dbs *backendDbs) ConfigGetGetBytes(key string) ([]byte, error) {
	rows := dbs.config.QueryRow("SELECT val FROM config WHERE key=?", key)
	val := []byte{}
	if err := rows.Scan(&val); err != nil {
		return val, serr.New(err)
	}
	return val, nil
}

func (dbs *backendDbs) ConfigGetString(key string) (string, error) {
	rows := dbs.config.QueryRow("SELECT val FROM config WHERE key=?", key)
	val := string("")
	if err := rows.Scan(&val); err != nil {
		return val, serr.New(err)
	}
	return val, nil
}

func (dbs *backendDbs) ConfigGetDeviceKey() (keytool.EasyEdKey, error) {
	tmpKey, err := dbs.ConfigGetGetBytes("deviceKey")
	myKey := keytool.EasyEdKey{}
	if err != nil {
		slog.Info("error", "error", err)
		return myKey, serr.New(err)
	}
	myKey.SetTorPrivateKey(ed25519.PrivateKey(tmpKey))
	return myKey, nil
}
