package nntpbackend

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"net/mail"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/kothawoc/go-nntp"
	nntpserver "github.com/kothawoc/go-nntp/server"
	"github.com/kothawoc/kothawoc/pkg/messages"
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
`

func openCreateDB(path, sqlQuery string) (*sql.DB, error) {

	t := time.Now().Unix()

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(sqlQuery, t); err != nil {
		log.Printf("FAILED Create DB database query [%v][%s][%s]", err, path, sqlQuery)
		return nil, err
	}

	return db, nil
}

func NewBackendDBs(path string) (*backendDbs, error) {

	dbs := &backendDbs{path: path}

	os.MkdirAll(path+"/groups", 0700)

	//db, err := sql.Open("sqlite3", path+"/articles.db")
	db, err := openCreateDB(path+"/articles.db", createArticlesDB)
	if err != nil {
		return nil, err
	}
	dbs.articles = db

	db, err = openCreateDB(path+"/config.db", createConfigDB)
	if err != nil {
		return nil, err
	}
	dbs.config = db

	db, err = openCreateDB(path+"/groups.db", createGroupsDB)
	if err != nil {
		return nil, err
	}
	dbs.groups = db

	db, err = openCreateDB(path+"/peers.db", createPeersDB)
	if err != nil {
		return nil, err
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
		return err
	}

	id := int64(0)
	name := ""

	log.Printf("EOpening groups do db stuff at ")
	for rows.Next() {
		err := rows.Scan(&id, &name)

		log.Printf("Open grouplist [%d][%x][%s]", id, id, name)
		if err != nil {
			return err
		}

		db, err := sql.Open("sqlite3", fmt.Sprintf("%s/groups/%x.db", dbs.path, id))
		if err != nil {
			log.Printf("Error OpenGroup o do db stuff at [%v][%v]", db, err)
			return err
		}

		dbs.groupArticles[name] = db
		dbs.groupArticlesName2Int[name] = id
		dbs.groupArticlesName2Hex[name] = strconv.FormatInt(id, 16)

	}
	return nil

}

func (dbs *backendDbs) NewGroup(name, description, flags string) error {

	res, err := dbs.groups.Exec("INSERT INTO groups(name) VALUES(?);", name)
	if err != nil {
		log.Printf("Error NewGroup INSERT to do db stuff at insert [%v]", err)
		return err
	}

	groupId, err := res.LastInsertId()
	if err != nil {
		log.Printf("Error getting inserted rowid to do db stuff at last id [%v]", err)
		return err
	}

	log.Printf("Last inserted rowid to do db stuff at [%v]", groupId)

	db, err := sql.Open("sqlite3", fmt.Sprintf("%s/groups/%x.db", dbs.path, groupId))
	if err != nil {
		log.Printf("Error opening NewGroup database id [%s][%v]", name, err)
		return err
	}

	if msg, err := db.Exec(createArticleIndexDB); err != nil {
		log.Printf("FAILED Create article index DB database query [%s][%v][%v] q[%s]", dbs.path, err, msg, createArticleIndexDB)
		return err
	}

	if msg, err := db.Exec("INSERT OR REPLACE INTO config (key, val) VALUES (?, ?)", "description", description); err != nil {
		log.Printf("FAILED Upserting group config value group:[%s] desc:[%s] err:[%v] resp[%v]", name, description, err, msg)
		return err
	}
	if msg, err := db.Exec("INSERT OR REPLACE INTO config (key, val) VALUES (?, ?)", "flags", flags); err != nil {
		log.Printf("FAILED Upserting group config value group:[%s] desc:[%s] err:[%v] resp[%v]", name, description, err, msg)
		return err
	}

	dbs.groupArticles[name] = db
	dbs.groupArticlesName2Int[name] = groupId
	dbs.groupArticlesName2Hex[name] = strconv.FormatInt(groupId, 16)

	return nil
}

func (dbs *backendDbs) GetArticleBySignature(signature string) (*nntp.Article, error) {

	message, err := os.ReadFile(dbs.path + "/articles/" + signature)
	if err != nil {
		log.Printf("GetArticleBySignature [%v] ERROR ReadFile[%v]", signature, err)
		return nil, err
	}
	body := (strings.SplitN(string(message), "\r\n\r\n", 2))[1]
	msg, err := mail.ReadMessage(bytes.NewReader(message))
	if err != nil {
		log.Printf("GetArticleBySignature [%v] ERROR Processing message[%v]", signature, err)
		return nil, err
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

	log.Printf("GetArticleById [%v]", msgId)

	row := dbs.articles.QueryRow("SELECT id, messageid, signature FROM articles WHERE messageid=?", msgId)

	id := int64(0)
	messageid := ""
	signature := ""
	err := row.Scan(&id, &messageid, &signature)
	if err != nil {
		log.Printf("GetArticleById Failed to open article final row scan [%s] [%v]", msgId, err)
		return nil, nntpserver.ErrInvalidArticleNumber
	}

	article, err := dbs.GetArticleBySignature(signature)
	if err != nil {
		log.Printf("GetArticleById Failed to get article by signature mid[%s] sig[%s] err[%v]", msgId, signature, err)
		return nil, nntpserver.ErrInvalidArticleNumber
	}

	log.Printf("GetArticle By Id return [%v] [%v]", article, err)

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
		log.Printf("CancelMessage [%v] ERROR GetArticleById[%v]", msgId, err)
		return err
	}

	if article.Header.Get("From") != from {
		log.Printf("CancelMessage [%v] ERROR from doesn't match article.", from)
		return fmt.Errorf("Cancel message from doesn't match article cancelMsg[%] article[%]", from, article.Header.Get("From"))
	}

	signature := article.Header.Get(messages.SignatureHeader)
	msgGroups := strings.Split(article.Header.Get("Newsgroups"), ",")
	delGroups := strings.Split(newsgroups, ",")

	log.Printf("CancelMessage [%v]", msgGroups)

	for _, grp := range delGroups {
		// if the message is actually in the group that they want to delete
		if containsStr(msgGroups, grp) {
			// delete message
			splitGrp := strings.Split(grp, ".")
			switch splitGrp[1] {
			case "peers":
				peerId := strings.Split(article.Header.Get("Control"), " ")[1]

				err := cmf.RemovePeer(peerId)
				if err != nil {

					log.Printf("CancelMessage: Failed to remove peer [%v] [%s]", err, peerId)
					return err
				}

			}

			_, err = dbs.groupArticles[grp].Exec("DELETE FROM articles WHERE messageid=?;", msgId)
			if err != nil {
				log.Printf("CancelMessage: Ouch def Error insert article to do db stuff at [%v] [%s]", err, msgId)
				return err
			} else {
				log.Printf("CancelMessage: SUCCESS  insert article to do db stuff at [%v] [%s]", err, msgId)
			}

			row := dbs.articles.QueryRow("UPDATE articles SET refs=refs - 1 WHERE messageid=? RETURNING refs;", msgId)
			refs := int64(0)
			err = row.Scan(&refs)
			if err != nil {
				log.Printf("CancelMessage: Ouch update refs def Error insert article to do db stuff at [%v] [%s]", err, article.Header.Get("Message-Id"))
				return err
			} else {
				log.Printf("CancelMessage: SUCCESS update refs insert article to do db stuff at [%v] [%s]", err, article.Header.Get("Message-Id"))
			}

			if refs == 0 {
				// delete the article off disc
				err := os.Remove(dbs.path + "/articles/" + signature)
				if err != nil {
					log.Printf("CancelMessage: Error[%v] id[%s] sig[%s]", err, msgId, signature)
					return err
				}

				_, err = dbs.articles.Exec("DELETE articles WHERE messageid=?;", msgId)
				if err != nil {
					log.Printf("CancelMessage: Delete from main DB Error[%v] id[%s] sig[%s]", err, msgId, signature)
					return err
				} else {
					log.Printf("CancelMessage: SUCCESS from main DB Error[%v] id[%s] sig[%s]", err, msgId, signature)
				}
			}

		}
		//if  delGroups
	}

	return nil
}

func (dbs *backendDbs) OpenArticlesDB(id int) (*sql.DB, error) {

	db, err := sql.Open("sqlite3", fmt.Sprintf("%s/groups/%x.db", dbs.path, id))
	if err != nil {
		log.Printf("FAILED Open OpenArticleDB Failed [%d] [%#v]", id, err)
		return nil, err
	}

	if msg, err := db.Exec(createArticlesDB); err != nil {
		log.Printf("FAILED Create DB OpenArticleDB QUERY [%d] [%#v] [%#v]", id, err, msg)
		return db, err
	} else {
		log.Printf("SUCCESS Create DB OpenArticleDB QUERY [%d] [%#v] [%#v]", id, err, msg)
	}

	log.Printf("OpenArticleDB SUCCESS [%d]", id)

	return db, nil
}

func (dbs *backendDbs) ConfigSet(key string, val interface{}) error {
	log.Printf("Attempting to uupsert key[%#v] val[%#v]", key, val)
	if msg, err := dbs.config.Exec("INSERT OR REPLACE INTO config (key, val) VALUES (?, ?)", key, val); err != nil {
		log.Printf("FAILED Upserting config value [%s][%v][%v] q[%s]", dbs.path, err, msg, createArticleIndexDB)
		return err
	}
	return nil
}

func (dbs *backendDbs) ConfigGetInt64(key string) (int64, error) {
	rows := dbs.config.QueryRow("SELECT val FROM config WHERE key=?", key)
	val := int64(0)
	err := rows.Scan(&val)
	return val, err
}

func (dbs *backendDbs) ConfigGetGetBytes(key string) ([]byte, error) {
	rows := dbs.config.QueryRow("SELECT val FROM config WHERE key=?", key)
	val := []byte{}
	err := rows.Scan(&val)

	return val, err
}

func (dbs *backendDbs) ConfigGetString(key string) (string, error) {
	rows := dbs.config.QueryRow("SELECT val FROM config WHERE key=?", key)
	val := string("")
	err := rows.Scan(&val)

	return val, err
}
