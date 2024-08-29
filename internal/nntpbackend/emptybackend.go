package nntpbackend

import (
	"log"

	"github.com/kothawoc/go-nntp"
	nntpserver "github.com/kothawoc/go-nntp/server"
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
type EmptyNntpBackend struct {
	ConfigPath  string
	DBs         *backendDbs
	NextBackend nntpserver.Backend
}

func (be *EmptyNntpBackend) ListGroups(session map[string]string) (<-chan *nntp.Group, error) {

	log.Printf("E ListGroups")
	return nil, nntpserver.ErrNotAuthenticated
}

func (be *EmptyNntpBackend) GetGroup(session map[string]string, name string) (*nntp.Group, error) {
	log.Printf("E GetGroup")
	return nil, nntpserver.ErrNotAuthenticated

}

func (be *EmptyNntpBackend) GetArticleWithNoGroup(session map[string]string, id string) (*nntp.Article, error) {

	log.Printf("E GetArticleWithNoGroup")
	return nil, nntpserver.ErrNotAuthenticated
}
func (be *EmptyNntpBackend) GetArticle(session map[string]string, group *nntp.Group, id string) (*nntp.Article, error) {

	log.Printf("E GetArticle")
	return nil, nntpserver.ErrNotAuthenticated
}

func (be *EmptyNntpBackend) GetArticles(session map[string]string, group *nntp.Group, from, to int64) (<-chan nntpserver.NumberedArticle, error) {

	log.Printf("E GetArticles")
	return nil, nntpserver.ErrNotAuthenticated
}
func (be *EmptyNntpBackend) Authorized(session map[string]string) bool {
	log.Printf("E Authorized")
	return false
}

func (be *EmptyNntpBackend) Authenticate(usession map[string]string, ser, pass string) (nntpserver.Backend, error) {
	log.Printf("E Authenticate")
	return be.NextBackend, nil
}

func (be *EmptyNntpBackend) AllowPost(session map[string]string) bool {
	return false
}

func (be *EmptyNntpBackend) Post(session map[string]string, article *nntp.Article) error {
	log.Printf("E Post")
	return nntpserver.ErrNotAuthenticated
}
