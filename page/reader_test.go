package page

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rsdoiel/markdownweb/debuglog"
)

func readerSite(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := func(body string) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(body))
		}
		switch r.URL.Path {
		case "/post":
			html(`<html><head><title>The Post</title><script>var x=1</script></head><body><nav>menu</nav>
<article><h1>The Post</h1><p>` + strings.Repeat("Article words. ", 30) + `<a href="/next">next</a></p></article></body></html>`)
		case "/empty":
			html(`<html><head><title>App</title></head><body><div id="root"></div><script>render()</script></body></html>`)
		case "/image.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG"))
		case "/redirect":
			http.Redirect(w, r, "/post", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchReaderConvertsAnHTMLPage(t *testing.T) {
	srv := readerSite(t)
	p, err := FetchReader(context.Background(), srv.URL+"/post")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Reader || p.Title() != "The Post" || !strings.Contains(p.Body, "Article words.") {
		t.Errorf("page %+v", p)
	}
	if !strings.Contains(p.Body, "["+"next]("+srv.URL+"/next)") {
		t.Errorf("link not resolved to the page's address:\n%s", p.Body)
	}
	if strings.Contains(p.Body, "menu") || strings.Contains(p.Body, "var x") {
		t.Errorf("chrome or script in the reader view:\n%s", p.Body)
	}
}

func TestFetchReaderFollowsARedirectAndReportsTheFinalAddress(t *testing.T) {
	srv := readerSite(t)
	p, err := FetchReader(context.Background(), srv.URL+"/redirect")
	if err != nil || !strings.HasSuffix(p.URL, "/post") {
		t.Errorf("got %v, %v", p, err)
	}
}

func TestFetchReaderRefusesWhatIsNotHTML(t *testing.T) {
	srv := readerSite(t)
	_, err := FetchReader(context.Background(), srv.URL+"/image.png")
	if !errors.Is(err, ErrData) {
		t.Errorf("err %v, want ErrData", err)
	}
	_, err = FetchReader(context.Background(), srv.URL+"/missing")
	if !errors.Is(err, ErrNoInput) {
		t.Errorf("err %v, want ErrNoInput", err)
	}
}

func TestFetchReaderSaysWhenAPageHasNoText(t *testing.T) {
	srv := readerSite(t)
	_, err := FetchReader(context.Background(), srv.URL+"/empty")
	if !errors.Is(err, ErrData) || !strings.Contains(err.Error(), "JavaScript") {
		t.Errorf("err %v: want a data error that names JavaScript", err)
	}
}

func TestIsHTMLTellsAnHTMLAnswerFromOtherFailures(t *testing.T) {
	srv := readerSite(t)
	_, err := Fetch(context.Background(), srv.URL+"/post")
	if !IsHTML(err) {
		t.Errorf("an HTML page with no Markdown copy should be IsHTML: %v", err)
	}
	_, err = Fetch(context.Background(), srv.URL+"/image.png")
	if IsHTML(err) {
		t.Errorf("an image is not an HTML page: %v", err)
	}
	_, err = Fetch(context.Background(), srv.URL+"/missing")
	if IsHTML(err) {
		t.Errorf("a 404 is not an HTML page: %v", err)
	}
	if IsHTML(nil) {
		t.Error("nil")
	}
}

func TestFetchReaderIsLogged(t *testing.T) {
	srv := readerSite(t)
	var buf bytes.Buffer
	ctx := debuglog.WithContext(context.Background(), debuglog.New(&buf))
	if _, err := FetchReader(ctx, srv.URL+"/post"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"event":"fetch.reader"`, `"status":200`, `"title":"The Post"`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("log lacks %s:\n%s", want, buf.String())
		}
	}
	if strings.Contains(buf.String(), "Article words") {
		t.Error("the log holds page content")
	}
}
