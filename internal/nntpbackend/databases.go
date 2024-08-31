package nntpbackend

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
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
	refs TEXT NOT NULL
	);
INSERT INTO articles(id,messageid,signature,refs)
	VALUES(?,"DELETEME","1","1");
DELETE FROM articles WHERE messageID="DELETEME";
`

const createArticleIndexDB string = `
CREATE TABLE IF NOT EXISTS articles (
	id INTEGER NOT NULL,
	messageid TEXT NOT NULL UNIQUE
	);
CREATE TABLE IF NOT EXISTS subscriptions (
	peer TEXT NOT NULL UNIQUE,
	lastmsg INTEGER NOT NULL,
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

	insert := `INSERT INTO
		groups(name) 
		VALUES(?);`

	res, err := dbs.groups.Exec(insert, name)
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
