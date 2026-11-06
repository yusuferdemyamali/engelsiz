package providers

import (
	"context"
	"testing"
)

type articleStub struct{ article Article }

func (s articleStub) ReadArticle(context.Context, string, string) (Article, error) {
	return s.article, nil
}

func TestArticleReaderContract(t *testing.T) {
	var _ ArticleReader = articleStub{}
	stub := articleStub{article: Article{PackageID: "p", Href: "/content/content/p/A", ContentType: "text/html", Body: []byte("<p>x</p>")}}
	got, err := stub.ReadArticle(context.Background(), "p", "/content/content/p/A")
	if err != nil || string(got.Body) != "<p>x</p>" || got.PackageID != "p" {
		t.Fatalf("stub = %+v %v", got, err)
	}
	if ErrArticleUnavailable == nil || ErrArticleUnavailable == ErrContentUnavailable || ErrArticleUnavailable == ErrRuntimeUnavailable {
		t.Fatal("article sentinel must be distinct")
	}
}
