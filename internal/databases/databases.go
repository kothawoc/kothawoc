package databases

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
	vcard "github.com/emersion/go-vcard"
	_ "github.com/mattn/go-sqlite3"

	"github.com/kothawoc/go-nntp"
	nntpserver "github.com/kothawoc/go-nntp/server"
	"github.com/kothawoc/kothawoc/pkg/keytool"
	"github.com/kothawoc/kothawoc/pkg/messages"
	serr "github.com/kothawoc/kothawoc/pkg/serror"
)

type BackendDbs struct {
	Cmd                             chan DatabaseMessage
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
	groupname TEXT NOT NULL UNIQUE
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

func NewBackendDbs(path string) (*BackendDbs, error) {

	dbs := &BackendDbs{path: path}

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

	dbs.openGroups()

	dbs.Cmd = make(chan DatabaseMessage, 10)
	go dbs.DbServer()

	return dbs, nil
}

type DatabaseCommand string
type DatabaseMessage struct {
	Cmd  DatabaseCommand
	Args []interface{}
}

func (dbs *BackendDbs) DbServer() error {
	for {
		cmd := <-dbs.Cmd
		slog.Info("DB SERVER", "cmd", cmd.Cmd, "args", cmd.Args)
		switch cmd.Cmd {
		case CmdGetPerms: // Args: []interface{}{torid, group, ret},
			ret := cmd.Args[2].(chan []interface{})
			ret <- []interface{}{dbs.getPerms(cmd.Args[0].(string), cmd.Args[1].(string))}
			close(ret)

		case CmdNewGroup: // Args: []interface{}{name, description, card, ret},
			ret := cmd.Args[3].(chan []interface{})
			a := dbs.newGroup(cmd.Args[0].(string), cmd.Args[1].(string), cmd.Args[2].(vcard.Card))
			ret <- []interface{}{a}
			close(ret)

		case CmdGetArticleBySignature: // Args: []interface{}{signature, ret},
			ret := cmd.Args[1].(chan []interface{})
			a, b := dbs.getArticleBySignature(cmd.Args[0].(string))
			ret <- []interface{}{a, b}
			close(ret)

		case CmdGetArticleById: // Args: []interface{}{signature, ret},
			ret := cmd.Args[1].(chan []interface{})
			a, b := dbs.getArticleById(cmd.Args[0].(string))
			ret <- []interface{}{a, b}
			close(ret)

		case CmdCancelMessage: // Args: []interface{}{from, msgId, newsgroups, cmf, ret},
			ret := cmd.Args[4].(chan []interface{})
			a := dbs.cancelMessage(cmd.Args[0].(string), cmd.Args[1].(string), cmd.Args[2].(string), cmd.Args[3].(messages.ControMesasgeFunctions))
			ret <- []interface{}{a}
			close(ret)

		case CmdConfigSet: // Args: []interface{}{key, val, ret},
			ret := cmd.Args[2].(chan []interface{})
			a := dbs.configSet(cmd.Args[0].(string), cmd.Args[1])
			ret <- []interface{}{a}
			close(ret)

		case CmdConfigGetString: // Args: []interface{}{key, ret},
			ret := cmd.Args[1].(chan []interface{})
			a, b := dbs.configGetString(cmd.Args[0].(string))
			ret <- []interface{}{a, b}
			close(ret)

		case CmdConfigGetInt64: // Args: []interface{}{key, ret},
			ret := cmd.Args[1].(chan []interface{})
			a, b := dbs.configGetInt64(cmd.Args[0].(string))
			ret <- []interface{}{a, b}
			close(ret)

		case CmdConfigGetBytes: // Args: []interface{}{key, ret},
			ret := cmd.Args[1].(chan []interface{})
			a, b := dbs.configGetBytes(cmd.Args[0].(string))
			ret <- []interface{}{a, b}
			close(ret)

		case CmdConfigGetDeviceKey: // Args: []interface{}{ret},
			ret := cmd.Args[0].(chan []interface{})
			a, b := dbs.configGetDeviceKey()
			ret <- []interface{}{a, b}
			close(ret)

		case CmdListGroups: // Args: []interface{}{session,ret},
			ret := cmd.Args[1].(chan []interface{})
			a, b := dbs.listGroups(cmd.Args[0].(map[string]string))
			ret <- []interface{}{a, b}
			close(ret)

		case CmdGetGroup: // Args: []interface{}{session, groupName, ret},
			ret := cmd.Args[2].(chan []interface{})
			a, b := dbs.getGroup(cmd.Args[0].(map[string]string), cmd.Args[1].(string))
			ret <- []interface{}{a, b}
			close(ret)

		case CmdListArticles: // Args: []interface{}{session, group, from, to, ret},
			ret := cmd.Args[4].(chan []interface{})
			a, b := dbs.listArticles(cmd.Args[0].(map[string]string), cmd.Args[1].(string), cmd.Args[2].(int64), cmd.Args[3].(int64))
			ret <- []interface{}{a, b}
			close(ret)

		case CmdGetGroupNumber: // Args: []interface{}{group, ret},
			ret := cmd.Args[1].(chan []interface{})
			a, b := dbs.getGroupNumber(cmd.Args[0].(string))
			ret <- []interface{}{a, b}
			close(ret)

		case CmdStoreArticle: // Args: []interface{}{msg, ret},
			ret := cmd.Args[1].(chan []interface{})
			a, b := dbs.storeArticle(cmd.Args[0].(*messages.MessageTool))
			ret <- []interface{}{a, b}
			close(ret)

		case CmdAddArticleToGroup: // Args: []interface{}{group, messageId, articleId, ret},
			ret := cmd.Args[3].(chan []interface{})
			a := dbs.addArticleToGroup(cmd.Args[0].(string), cmd.Args[1].(string), cmd.Args[2].(int64))
			ret <- []interface{}{a}
			close(ret)

		case CmdAddPeer: // Args: []interface{}{group, messageId, articleId, ret},
			ret := cmd.Args[1].(chan []interface{})
			a := dbs.addPeer(cmd.Args[0].(string))
			ret <- []interface{}{a}
			close(ret)

		case CmdGetPeerList: // Args: []interface{}{ret},
			ret := cmd.Args[0].(chan []interface{})
			a, b := dbs.getPeerList()
			ret <- []interface{}{a, b}
			close(ret)

		case CmdGroupConfigSet: // Args: []interface{}{group, key, val, ret},
			ret := cmd.Args[3].(chan []interface{})
			a := dbs.groupConfigSet(cmd.Args[0].(string), cmd.Args[1].(string), cmd.Args[2])
			ret <- []interface{}{a}
			close(ret)

		case CmdGroupConfigGetInt64: // Args: []interface{}{group, key, ret},
			ret := cmd.Args[2].(chan []interface{})
			a, b := dbs.groupConfigGetInt64(cmd.Args[0].(string), cmd.Args[1].(string))
			ret <- []interface{}{a, b}
			close(ret)

		case CmdGroupUpdateSubscriptions: // Args: []interface{}{group, list, ret},
			ret := cmd.Args[2].(chan []interface{})
			a := dbs.groupUpdateSubscriptions(cmd.Args[0].(string), cmd.Args[1].([]string))
			ret <- []interface{}{a}
			close(ret)

		case CmdGetNextArticle: // Args: []interface{}{lastMessage, ret},
			ret := cmd.Args[1].(chan []interface{})
			a, b := dbs.getNextArticle(cmd.Args[0].(int64))
			ret <- []interface{}{a, b}
			close(ret)

		}
	}
}

func (dbs *BackendDbs) openGroups() error {

	rows, err := dbs.groups.Query("SELECT id,name FROM groups;")
	if err != nil {
		return serr.New(err)
	}
	defer rows.Close()

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

const CmdGetPerms = DatabaseCommand("GetPerms")

func (dbs *BackendDbs) GetPerms(torid, group string) *PermissionsGroupT {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdGetPerms,
		Args: []interface{}{torid, group, ret},
	}
	rv := <-ret
	return rv[0].(*PermissionsGroupT)
}

func (dbs *BackendDbs) getPerms(torid, group string) *PermissionsGroupT {
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

const CmdNewGroup = DatabaseCommand("NewGroup")

func (dbs *BackendDbs) NewGroup(name, description string, card vcard.Card) error {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdNewGroup,
		Args: []interface{}{name, description, card, ret},
	}

	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return err
	}
	return nil
}

func (dbs *BackendDbs) newGroup(name, description string, card vcard.Card) error {

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

const CmdGetArticleBySignature = DatabaseCommand("GetArticleBySignature")

func (dbs *BackendDbs) GetArticleBySignature(signature string) (*nntp.Article, error) {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdGetArticleBySignature,
		Args: []interface{}{signature, ret},
	}

	res := <-ret
	err, ok := res[1].(error)
	if !ok {
		return res[0].(*nntp.Article), err
	}
	return res[0].(*nntp.Article), nil
}

func (dbs *BackendDbs) getArticleBySignature(signature string) (*nntp.Article, error) {

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

const CmdGetArticleById = DatabaseCommand("GetArticleById")

func (dbs *BackendDbs) GetArticleById(msgId string) (*nntp.Article, error) {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdGetArticleById,
		Args: []interface{}{msgId, ret},
	}

	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return res[0].(*nntp.Article), err
	}

	return res[0].(*nntp.Article), nil
}

func (dbs *BackendDbs) getArticleById(msgId string) (*nntp.Article, error) {

	slog.Info("GetArticleById", "msgId", msgId)
	query := ""
	// if the id is an int, get the message id
	if _, err := strconv.ParseInt(msgId, 10, 64); err == nil {
		query = "SELECT id, messageid, signature FROM articles WHERE id=?"
	} else {
		query = "SELECT id, messageid, signature FROM articles WHERE messageid=?"
	}
	row := dbs.articles.QueryRow(query, msgId)

	id := int64(0)
	messageid := ""
	signature := ""
	err := row.Scan(&id, &messageid, &signature)
	if err != nil {
		slog.Info("GetArticleById Failed to open article final row scan", "msgId", msgId, "error", err)
		return nil, serr.New(nntpserver.ErrInvalidArticleNumber)
	}

	article, err := dbs.getArticleBySignature(signature)
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

const CmdCancelMessage = DatabaseCommand("CancelMessage")

func (dbs *BackendDbs) CancelMessage(from, msgId, newsgroups string, cmf messages.ControMesasgeFunctions) error {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdCancelMessage,
		Args: []interface{}{from, msgId, newsgroups, cmf, ret},
	}

	res := <-ret

	err, ok := res[0].(error)
	if !ok {
		return err
	}

	return nil
}

func (dbs *BackendDbs) cancelMessage(from, msgId, newsgroups string, cmf messages.ControMesasgeFunctions) error {
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

/*
func (dbs *BackendDbs) openArticlesDB(id int) (*sql.DB, error) {

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
*/

const CmdConfigSet = DatabaseCommand("ConfigSet")

func (dbs *BackendDbs) ConfigSet(key string, val interface{}) error {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdConfigSet,
		Args: []interface{}{key, val, ret},
	}

	res := <-ret

	err, ok := res[0].(error)
	if !ok {
		return err
	}

	return nil
}

func (dbs *BackendDbs) configSet(key string, val interface{}) error {
	slog.Info("Attempting to uupsert key[%#v] val[%#v]", key, val)
	if msg, err := dbs.config.Exec("INSERT OR REPLACE INTO config (key, val) VALUES (?, ?)", key, val); err != nil {
		slog.Info("FAILED Upserting config value", "path", dbs.path, "error", err, "msg", msg, "query", createArticleIndexDB)
		return serr.New(err)
	}
	return nil
}

const CmdConfigGetInt64 = DatabaseCommand("ConfigGetInt64")

func (dbs *BackendDbs) ConfigGetInt64(key string) (int64, error) {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdConfigGetInt64,
		Args: []interface{}{key, ret},
	}
	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return res[0].(int64), err
	}

	return res[0].(int64), nil
}
func (dbs *BackendDbs) configGetInt64(key string) (int64, error) {
	rows := dbs.config.QueryRow("SELECT val FROM config WHERE key=?", key)
	val := int64(0)
	if err := rows.Scan(&val); err != nil {
		return val, serr.New(err)
	}
	return val, nil
}

const CmdConfigGetBytes = DatabaseCommand("ConfigGetBytes")

func (dbs *BackendDbs) ConfigGetBytes(key string) ([]byte, error) {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdConfigGetBytes,
		Args: []interface{}{key, ret},
	}
	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return res[0].([]byte), err
	}

	return res[0].([]byte), nil
}
func (dbs *BackendDbs) configGetBytes(key string) ([]byte, error) {
	rows := dbs.config.QueryRow("SELECT val FROM config WHERE key=?", key)
	val := []byte{}
	if err := rows.Scan(&val); err != nil {
		return val, serr.New(err)
	}
	return val, nil
}

const CmdConfigGetString = DatabaseCommand("ConfigGetString")

func (dbs *BackendDbs) ConfigGetString(key string) (string, error) {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdConfigGetString,
		Args: []interface{}{key, ret},
	}
	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return res[0].(string), err
	}

	return res[0].(string), nil
}
func (dbs *BackendDbs) configGetString(key string) (string, error) {
	rows := dbs.config.QueryRow("SELECT val FROM config WHERE key=?", key)
	val := string("")
	if err := rows.Scan(&val); err != nil {
		return val, serr.New(err)
	}
	return val, nil
}

const CmdConfigGetDeviceKey = DatabaseCommand("ConfigGetDeviceKey")

func (dbs *BackendDbs) ConfigGetDeviceKey() (keytool.EasyEdKey, error) {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdConfigGetDeviceKey,
		Args: []interface{}{ret},
	}
	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return res[0].(keytool.EasyEdKey), err
	}

	return res[0].(keytool.EasyEdKey), nil
}
func (dbs *BackendDbs) configGetDeviceKey() (keytool.EasyEdKey, error) {
	tmpKey, err := dbs.configGetBytes("deviceKey")
	myKey := keytool.EasyEdKey{}
	if err != nil {
		slog.Info("error", "error", err)
		return myKey, serr.New(err)
	}
	myKey.SetTorPrivateKey(ed25519.PrivateKey(tmpKey))
	return myKey, nil
}

const CmdListGroups = DatabaseCommand("ListGroups")

func (dbs *BackendDbs) ListGroups(session map[string]string) (<-chan *nntp.Group, error) {

	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdListGroups,
		Args: []interface{}{session, ret},
	}
	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return res[0].(<-chan *nntp.Group), err
	}

	return res[0].(<-chan *nntp.Group), nil
}

func (dbs *BackendDbs) listGroups(session map[string]string) (<-chan *nntp.Group, error) {

	retChan := make(chan *nntp.Group)

	row, err := dbs.groups.Query("SELECT id, name FROM groups;")
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
			if perms := dbs.getPerms(session["Id"], name); perms != nil && !perms.Read {
				//	if !be.DBs.GetPerms(session["Id"], name).Read {
				continue
			}

			grp, err := dbs.getGroup(session, name)

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

const CmdGetGroup = DatabaseCommand("GetGroup")

func (dbs *BackendDbs) GetGroup(session map[string]string, groupName string) (*nntp.Group, error) {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdListGroups,
		Args: []interface{}{session, groupName, ret},
	}
	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return res[0].(*nntp.Group), err
	}

	return res[0].(*nntp.Group), nil
}

func (dbs *BackendDbs) getGroup(session map[string]string, groupName string) (*nntp.Group, error) {

	if perms := dbs.GetPerms(session["Id"], groupName); perms != nil && !perms.Read {

		//	if !be.DBs.GetPerms(session["Id"], groupName).Read {
		return nil, nntpserver.ErrNoSuchGroup
	}

	if articles, ok := dbs.groupArticles[groupName]; ok {

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

	slog.Info("E GetGroup not found", "groupName", groupName, "groupdb", dbs.groupArticles)
	return nil, nntpserver.ErrNoSuchGroup
}

const CmdListArticles = DatabaseCommand("ListArticles")

func (dbs *BackendDbs) ListArticles(session map[string]string, group string, from, to int64) (<-chan int64, error) {

	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdListArticles,
		Args: []interface{}{session, group, from, to, ret},
	}
	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return res[0].(<-chan int64), err
	}

	return res[0].(<-chan int64), nil
}

func (dbs *BackendDbs) listArticles(session map[string]string, group string, from, to int64) (<-chan int64, error) {

	retChan := make(chan int64, 10)

	if from > to {
		a := from
		to = from
		from = a
	}

	row, err := dbs.groupArticles[group].Query("SELECT id FROM articles WHERE id>=? and id<=?;", from, to)
	if err != nil {
		return nil, err
	}
	id := int64(0)
	go func() {
		defer close(retChan)
		for row.Next() {
			err := row.Scan(&id)
			if err != nil {
				slog.Info("Get Error Scan", "id", id, "error", err)
				return // nil, err
			}
			retChan <- id

		}
	}()

	return retChan, nil
}

const CmdGetGroupNumber = DatabaseCommand("GetGroupNumber")

func (dbs *BackendDbs) GetGroupNumber(group string) (int64, error) {

	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdListArticles,
		Args: []interface{}{group, ret},
	}
	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return res[0].(int64), err
	}

	return res[0].(int64), nil
}

func (dbs *BackendDbs) getGroupNumber(group string) (int64, error) {

	row := dbs.groups.QueryRow("SELECT idF ROM groups WHERE name=?;", group)

	var id int64
	err := row.Scan(&id)

	if err != nil {
		slog.Info("FAILED POST article find group", "id", id, "group", group, "error", err)
		return id, err
	}

	return id, nil
}

const CmdStoreArticle = DatabaseCommand("StoreArticle")

func (dbs *BackendDbs) StoreArticle(msg *messages.MessageTool) (int64, error) {

	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdStoreArticle,
		Args: []interface{}{msg, ret},
	}
	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return res[0].(int64), err
	}

	return res[0].(int64), nil
}

func (dbs *BackendDbs) storeArticle(msg *messages.MessageTool) (int64, error) {

	article := msg.Article

	signature := article.Header.Get(messages.SignatureHeader)
	messageId := article.Header.Get("Message-Id")
	insert := `INSERT INTO articles(messageid,signature,refs) VALUES(?,?,?);`

	res, err := dbs.articles.Exec(insert, messageId, signature, 0)
	if err != nil {
		slog.Info("Ouch abc Error insert article to do db stuff at", "error", err, "messageId", article.Header.Get("Message-Id"))
		return 0, serr.New(err)
	} else {
		slog.Info("SUCCESS  insert article to do db stuff at", "error", err, "messageId", article.Header.Get("Message-Id"))

	}

	articleId, err := res.LastInsertId()
	if err != nil {
		slog.Info("Error getting inserted rowid to do db stuff at", "error", err)
		return 0, serr.New(err)
	}

	slog.Info("Last inserted rowid to do db stuff at ", "articleId", articleId)

	err = os.WriteFile(dbs.path+"/articles/"+signature, []byte(msg.RawMail()), 0600)

	if err != nil {
		slog.Info("Error writing file Ouch def Error insert article to do db stuff at", "error", err, "messageId", article.Header.Get("Message-Id"))
		return 0, err
	}

	return articleId, nil
}

const CmdAddArticleToGroup = DatabaseCommand("AddArticleToGroup")

func (dbs *BackendDbs) AddArticleToGroup(group, messageId string, articleId int64) error {

	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdAddArticleToGroup,
		Args: []interface{}{group, messageId, articleId, ret},
	}
	res := <-ret

	err, ok := res[0].(error)
	if !ok {
		return err
	}

	return nil
}

func (dbs *BackendDbs) addArticleToGroup(group, messageId string, articleId int64) error {

	insert := "INSERT INTO articles(id,messageid) VALUES(?,?);"

	_, err := dbs.groupArticles[group].Exec(insert, articleId, messageId)
	if err != nil {
		slog.Info("Ouch def Error insert article to do db stuff at", "error", err, "messageId", messageId)
		return serr.New(err)
	} else {
		slog.Info("SUCCESS  insert article to do db stuff at", "error", err, "messageId", messageId)
	}

	row := dbs.articles.QueryRow("UPDATE articles SET refs=refs + 1 WHERE messageid=? RETURNING refs;", messageId)
	refs := int64(0)
	err = row.Scan(&refs)
	if err != nil {
		slog.Info("Ouch update refs def Error insert article to do db stuff at", "error", err, "messageId", messageId)
		return serr.New(err)
	} else {
		slog.Info("SUCCESS update refs insert article to do db stuff at", "error", err, "messageId", messageId)
	}
	return nil
}

const CmdAddPeer = DatabaseCommand("AddPeer")

func (dbs *BackendDbs) AddPeer(peerId string) error {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdAddPeer,
		Args: []interface{}{peerId, ret},
	}
	res := <-ret

	err, ok := res[0].(error)
	if !ok {
		return err
	}

	return nil
}

func (dbs *BackendDbs) addPeer(peerId string) error {

	peerKey := keytool.EasyEdKey{}
	peerKey.SetTorId(peerId)

	var id int
	var pubkey, name string

	row := dbs.peers.QueryRow("SELECT id,pubkey,name FROM peers WHERE torid=?;", peerId)
	err := row.Scan(&id, &pubkey, &name)
	slog.Info("ADDING PEER", "peerId", peerId, "id", id, "error", err)
	if err == nil {

		return serr.Wrap(fmt.Errorf("Peer already exists %s=%s", "torid", peerId), err)
	} else {
		if !errors.Is(err, sql.ErrNoRows) {
			return serr.Wrap(fmt.Errorf("Peer Add error %s=%s", "torid", peerId), err)
		}
	}

	myTorKey, _ := dbs.configGetDeviceKey()
	myTorId, _ := myTorKey.TorId()
	gDB := dbs.groupArticles[myTorId+".peers."+peerId]
	query := "UPDATE OR INSERT config(key,val) VALUES(?,?) WHERE key=?;"
	gDB.Exec(query, "ControlMessages", "true")
	gDB.Exec(query, "Feed", peerId)
	gDB.Exec(query, "LastMessage", 0)

	slog.Info("Adding peer", "id", id, "torid", peerId, "pubkey", pubkey, "name", name)

	peerPubKey, _ := peerKey.PubKey()
	// NewPeer(tc *torutils.TorCon, parent chan PeeringMessage, db *sql.DB, myKey keytool.EasyEdKey, peerKey keytool.EasyEdKey, dbs BackendDbs) (*Peer, error)

	res, err := dbs.peers.Exec("INSERT INTO peers(torid,pubkey,name) VALUES(?,\"?\",\"\");", peerId, peerPubKey)
	slog.Info("ERROR ADDPEER INSERT", "error", err, "res", res)
	if err != nil {
		return serr.New(err)

	}

	return nil
}

const CmdGetPeerList = DatabaseCommand("GetPeerList")

func (dbs *BackendDbs) GetPeerList() ([]string, error) {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdGetPeerList,
		Args: []interface{}{ret},
	}
	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return res[0].([]string), err
	}

	return res[0].([]string), nil
}

func (dbs *BackendDbs) getPeerList() ([]string, error) {

	ret := []string{}
	rows, err := dbs.peers.Query("SELECT torid FROM peers;")
	if err != nil {
		rows.Close()
		return nil, serr.New(err)
	}
	for rows.Next() {
		var torid string
		err := rows.Scan(&torid)
		if err != nil {
			rows.Close()
			return nil, serr.New(err)
		}
		ret = append(ret, torid)
	}

	return ret, nil

}

const CmdGroupConfigSet = DatabaseCommand("GroupConfigSet")

func (dbs *BackendDbs) GroupConfigSet(group, key string, val interface{}) error {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdGroupConfigSet,
		Args: []interface{}{group, key, val, ret},
	}

	res := <-ret

	err, ok := res[0].(error)
	if !ok {
		return err
	}

	return nil
}

func (dbs *BackendDbs) groupConfigSet(group, key string, val interface{}) error {
	slog.Info("Attempting to uupsert key[%#v] val[%#v]", key, val)
	if msg, err := dbs.groupArticles[group].Exec("INSERT OR REPLACE INTO config (key, val) VALUES (?, ?)", key, val); err != nil {
		slog.Info("FAILED Upserting config value", "path", dbs.path, "error", err, "msg", msg, "query", createArticleIndexDB)
		return serr.New(err)
	}
	return nil
}

const CmdGroupConfigGetInt64 = DatabaseCommand("GroupConfigGetInt64")

func (dbs *BackendDbs) GroupConfigGetInt64(group, key string) (int64, error) {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdGroupConfigGetInt64,
		Args: []interface{}{group, key, ret},
	}
	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return res[0].(int64), err
	}

	return res[0].(int64), nil
}
func (dbs *BackendDbs) groupConfigGetInt64(group, key string) (int64, error) {
	row := dbs.groupArticles[group].QueryRow("SELECT val FROM config WHERE key=?", key)
	val := int64(0)
	if err := row.Scan(&val); err != nil {
		return val, serr.New(err)
	}
	return val, nil
}

const CmdGroupUpdateSubscriptions = DatabaseCommand("GroupUpdateSubscriptions")

func (dbs *BackendDbs) GroupUpdateSubscriptions(group string, list []string) error {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdGroupUpdateSubscriptions,
		Args: []interface{}{group, list, ret},
	}
	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return err
	}

	return nil
}
func (dbs *BackendDbs) groupUpdateSubscriptions(group string, list []string) error {

	_, err := dbs.groupArticles[group].Exec("DELETE FROM subscriptions;")

	if err != nil {
		return serr.New(err)
	}

	for _, i := range list {
		dbs.groupArticles[group].Exec("INSERT INTO subscriptions(group) VALUES(?);", i)

		slog.Info("INSERT INTO", "item", i)
	}
	return nil
}

const CmdGetNextArticle = DatabaseCommand("GetNextArticle")

func (dbs *BackendDbs) GetNextArticle(lastMessage int64) (*nntpserver.NumberedArticle, error) {
	ret := make(chan []interface{})
	dbs.Cmd <- DatabaseMessage{
		Cmd:  CmdGetNextArticle,
		Args: []interface{}{lastMessage, ret},
	}
	res := <-ret

	err, ok := res[1].(error)
	if !ok {
		return nil, err
	}

	return res[1].(*nntpserver.NumberedArticle), nil
}
func (dbs *BackendDbs) getNextArticle(lastMessage int64) (*nntpserver.NumberedArticle, error) {

	row := dbs.articles.QueryRow("SELECT id FROM articles WHERE id>? ORDER BY id LIMIT 1", lastMessage)
	id := int64(0)
	err := row.Scan(&id)
	if err != nil {
		return nil, serr.New(err)
	}

	article, err := dbs.getArticleById(fmt.Sprintf("%d", id))
	if err != nil {
		return nil, serr.New(err)
	}
	art := &nntpserver.NumberedArticle{
		Num:     id,
		Article: article,
	}

	return art, nil

}
