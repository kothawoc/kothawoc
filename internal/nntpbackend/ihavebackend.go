package nntpbackend

// Optionals to have later.
// the streaming interface with Ihave and Iwant stuff will probably be very useful
// in the future.
/*
import (
	"github.com/kothawoc/go-nntp"
	nntpserver "github.com/kothawoc/go-nntp/server"
)

// An optional Interface Backend-objects may provide.
//
// This interface provides functions used in the IHAVE command.
// If this interface is not provided by a backend, the server falls back to
// similar functions from the required core interface.
type BackendIHave interface {
	// This method is like the Post(article *nntp.Article)-method except that it is executed
	// on the IHAVE command instead of on the POST command. IHave is required to return
	// ErrIHaveFailed or ErrIHaveRejected instead of the ErrPostingFailed error (otherwise
	// the server will missbehave)
	//
	// If BackendIHave is not provided, the server will use the Post-method with
	// any ErrPostingFailed-result being replaced by ErrIHaveFailed automatically.
	IHave(session map[string]string, id string, article *nntp.Article) error

	// This method will tell the server frontent, and thus, the client, wether the server should
	// accept the Article or not.
	// If the article is wanted and should be transfered, nil should be returned.
	// If it is clear, IHAVE would reject, ErrNotWanted should be returned.
	// If it is clear, IHAVE would fail, ErrIHaveNotPossible should be returned.
	//
	// If BackendIHave is not provided, the server will use the method
	// GetArticleWithNoGroup-method to determine.
	IHaveWantArticle(session map[string]string, id string) error
}

func (be *NntpBackend) IHave(session map[string]string, id string, article *nntp.Article) error {
	return nntpserver.ErrIHaveNotPossible
}

func (be *NntpBackend) IHaveWantArticle(session map[string]string, id string) error {
	return nntpserver.ErrNotWanted
}

// An optional Interface Backend-objects may provide.
//
// This interface provides an alternative version of "ListGroups"
// which gives the Backend developer the opportunity to improve
// both performance and efficiency of the LIST ACTIVE/LIST NEWSGROUPS
// command.
type BackendListWildMat interface {
	// This function will be called instead of ListGroups, if the
	// WildMat parameter is given. The implementor must return at
	// least all groups, that matches the given pattern.
	ListGroupsWildMat(session map[string]string, pattern *nntpserver.WildMat) (<-chan *nntp.Group, error)
}

func (be *NntpBackend) ListGroupsWildMat(session map[string]string, pattern *nntpserver.WildMat) (<-chan *nntp.Group, error) {
	return nil, nntpserver.ErrNotWanted
}

*/
