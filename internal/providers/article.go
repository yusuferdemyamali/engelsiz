package providers

import (
	"context"
	"errors"
)

// ErrArticleUnavailable reports that one owned article cannot be read: the
// href failed validation, the runtime answered unexpectedly, or the payload
// was not HTML. Callers degrade to the search snippet.
var ErrArticleUnavailable = errors.New("local article unavailable")

// Article is bounded owned article bytes for one validated local href. Body
// carries the raw HTML payload (already size-capped); plaintext stripping
// stays with the retrieval caller.
type Article struct {
	PackageID   string
	Href        string
	ContentType string
	Body        []byte
}

// ArticleReader serves article bytes from installed content through the
// owned runtime transport. Implementations must validate href against the
// installed package, disable redirects and bound the read.
type ArticleReader interface {
	ReadArticle(ctx context.Context, packageID, href string) (Article, error)
}
