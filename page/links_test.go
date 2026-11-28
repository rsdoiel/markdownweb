package page

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rsdoiel/markdownweb/fetch"
)

func linkSite(t *testing.T, link string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			http.NotFound(w, r)
			return
		}
		if link != "" {
			w.Header().Set("Link", link)
		}
		w.Header().Set("Content-Type", "text/markdown")
		w.Write([]byte("# Results\n\nHits 1 to 2 of 2\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchCarriesLinksOnThePage(t *testing.T) {
	srv := linkSite(t, `</opensearch.xml>; rel="search"; type="application/opensearchdescription+xml"; title="Wren"`)
	useFetcher(t, fetch.Options{})
	p, err := Fetch(context.Background(), srv.URL+"/search?q=x")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Links) != 1 {
		t.Fatalf("links: %#v", p.Links)
	}
	l := p.Links[0]
	if l.Target != srv.URL+"/opensearch.xml" || l.Rel[0] != "search" || l.Type != "application/opensearchdescription+xml" || l.Title != "Wren" {
		t.Errorf("link: %#v", l)
	}
}

func TestFetchWithoutTheHeaderHasNoLinksAndTheSameBody(t *testing.T) {
	srv := linkSite(t, "")
	useFetcher(t, fetch.Options{})
	p, err := Fetch(context.Background(), srv.URL+"/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if p.Links != nil {
		t.Errorf("Links = %#v, want nil", p.Links)
	}
	if p.Title() != "" || p.Body == "" || p.URL != srv.URL+"/a.md" {
		t.Errorf("page changed: %#v", p)
	}
}

func TestFetchReaderCarriesLinksToo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Link", `</d.xml>; rel=search; type="application/opensearchdescription+xml"`)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><head><title>T</title></head><body><p>Hello reader</p></body></html>"))
	}))
	defer srv.Close()
	useFetcher(t, fetch.Options{})
	p, err := FetchReader(context.Background(), srv.URL+"/x.html")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Links) != 1 || p.Links[0].Target != srv.URL+"/d.xml" {
		t.Errorf("links: %#v", p.Links)
	}
}

func TestFetchLinksFromTheAddressThatServedThePage(t *testing.T) {
	// the page is found at a sibling; the links are the sibling's response's
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			http.NotFound(w, r)
		case "/dir/":
			w.Header().Set("Link", `</wrong>; rel=search`)
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html><body>x</body></html>"))
		case "/dir/index.md":
			w.Header().Set("Link", `</right>; rel=search`)
			w.Header().Set("Content-Type", "text/markdown")
			w.Write([]byte("# dir\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	useFetcher(t, fetch.Options{})
	p, err := Fetch(context.Background(), srv.URL+"/dir/")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Links) != 1 || p.Links[0].Target != srv.URL+"/right" {
		t.Errorf("links: %#v (page %s)", p.Links, p.URL)
	}
}

func TestAFilePageHasNoLinks(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.md")
	if err := os.WriteFile(f, []byte("# a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if p.Links != nil {
		t.Errorf("Links = %#v", p.Links)
	}
}
