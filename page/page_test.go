package page

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseFrontMatter(t *testing.T) {
	src := "---\ntitle: Hello\nauthor: Dave\nurlNext: /two.md\n---\n# Body\n\ntext\n"
	p, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if p.Title() != "Hello" || p.Meta["author"] != "Dave" || p.URLNext() != "/two.md" {
		t.Errorf("meta %#v", p.Meta)
	}
	if strings.Contains(p.Body, "title:") || !strings.HasPrefix(p.Body, "# Body") {
		t.Errorf("body %q", p.Body)
	}
}

func TestParseNoFrontMatter(t *testing.T) {
	p, err := Parse([]byte("# Just text\n"))
	if err != nil || p.Body != "# Just text\n" || len(p.Meta) != 0 {
		t.Errorf("%#v %v", p, err)
	}
}

func TestParseBadYAMLIsDataError(t *testing.T) {
	_, err := Parse([]byte("---\ntitle: [unclosed\n---\nbody\n"))
	if err == nil || !strings.Contains(err.Error(), "front matter") {
		t.Errorf("err %v", err)
	}
}

func TestParseUnterminatedFrontMatterIsBody(t *testing.T) {
	src := "---\nnot closed\n"
	p, err := Parse([]byte(src))
	if err != nil || p.Body != src {
		t.Errorf("%#v %v", p, err)
	}
}

func TestIsMarkdown(t *testing.T) {
	cases := []struct {
		ct, path string
		want     bool
	}{
		{"text/markdown; charset=utf-8", "/a", true},
		{"text/plain", "/a.md", true},
		{"text/plain; charset=utf-8", "/a.markdown", true},
		{"application/octet-stream", "/a.md", true},
		{"text/plain", "/a.txt", false},
		{"text/html", "/a.md", false},
	}
	for _, c := range cases {
		if got := IsMarkdown(c.ct, c.path); got != c.want {
			t.Errorf("%q %q: %v", c.ct, c.path, got)
		}
	}
}

func TestFetchSendsBonBonAcceptAndReturnsMarkdown(t *testing.T) {
	var accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Write([]byte("---\ntitle: T\n---\nhi\n"))
	}))
	defer srv.Close()
	p, err := Fetch(context.Background(), srv.URL+"/x")
	if err != nil {
		t.Fatal(err)
	}
	if accept != "text/markdown, text/x-opml" {
		t.Errorf("Accept %q", accept)
	}
	if p.Title() != "T" || p.Body != "hi\n" {
		t.Errorf("%#v", p)
	}
}

func TestFetchHTMLIsNotAPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<p>hi</p>"))
	}))
	defer srv.Close()
	_, err := Fetch(context.Background(), srv.URL+"/x")
	if err == nil || !strings.Contains(err.Error(), "not Markdown") {
		t.Errorf("err %v", err)
	}
}

func TestFetchMissingPage(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, err := Fetch(context.Background(), srv.URL+"/x")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err %v", err)
	}
}

// mirrorSite serves HTML for /a.html and /dir/, Markdown for /a.md and
// /dir/index.md, HTML only for /only.html, and counts requests per path.
func mirrorSite(t *testing.T, negotiate bool) (*httptest.Server, map[string]int) {
	t.Helper()
	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		md := func(body string) {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			w.Write([]byte(body))
		}
		html := func() {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<p>html</p>"))
		}
		switch r.URL.Path {
		case "/a.md":
			md("# From md\n")
		case "/dir/index.md":
			md("# Dir index\n")
		case "/index.md":
			md("# Site root\n")
		case "/a.html", "/dir/", "/":
			if negotiate && strings.Contains(r.Header.Get("Accept"), "text/markdown") {
				md("# Negotiated\n")
				return
			}
			html()
		case "/only.html", "/feed.xml":
			html()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func TestFetchHTMLLinkFallsBackToMarkdownSibling(t *testing.T) {
	srv, _ := mirrorSite(t, false)
	p, err := Fetch(context.Background(), srv.URL+"/a.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p.URL, "/a.md") || !strings.Contains(p.Body, "From md") {
		t.Errorf("url %q body %q", p.URL, p.Body)
	}
}

// A site's address is often typed with no path at all, as antenna preview
// prints it: http://localhost:8000. That is the site root, a directory.
func TestFetchSiteRootWithNoPathFallsBackToIndexMd(t *testing.T) {
	srv, _ := mirrorSite(t, false)
	for _, addr := range []string{srv.URL, srv.URL + "/", srv.URL + "?x=1"} {
		p, err := Fetch(context.Background(), addr)
		if err != nil {
			t.Fatalf("%s: %s", addr, err)
		}
		if !strings.Contains(p.URL, "/index.md") || !strings.Contains(p.Body, "Site root") {
			t.Errorf("%s: url %q body %q", addr, p.URL, p.Body)
		}
	}
}

func TestFetchDirectoryFallsBackToIndexMd(t *testing.T) {
	srv, _ := mirrorSite(t, false)
	p, err := Fetch(context.Background(), srv.URL+"/dir/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p.URL, "/dir/index.md") {
		t.Errorf("url %q", p.URL)
	}
}

func TestFetchPrefersTheServersOwnNegotiation(t *testing.T) {
	srv, hits := mirrorSite(t, true)
	p, err := Fetch(context.Background(), srv.URL+"/a.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Body, "Negotiated") || hits["/a.md"] != 0 {
		t.Errorf("body %q, sibling hits %d", p.Body, hits["/a.md"])
	}
}

func TestFetchNoSiblingKeepsTheNotMarkdownError(t *testing.T) {
	srv, _ := mirrorSite(t, false)
	_, err := Fetch(context.Background(), srv.URL+"/only.html")
	if err == nil || !strings.Contains(err.Error(), "not Markdown") || !errors.Is(err, ErrData) {
		t.Errorf("err %v", err)
	}
}

func TestFetchOnlyHTMLAndDirectoryLinksTryASibling(t *testing.T) {
	srv, hits := mirrorSite(t, false)
	if _, err := Fetch(context.Background(), srv.URL+"/feed.xml"); err == nil {
		t.Error("xml fetched as a page")
	}
	if hits["/feed.md"] != 0 {
		t.Errorf("tried a sibling for xml: %d", hits["/feed.md"])
	}
}

func TestFetchSiblingKeepsTheQuery(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/s.md" {
			got = r.URL.RawQuery
			w.Header().Set("Content-Type", "text/markdown")
			w.Write([]byte("ok\n"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
	}))
	defer srv.Close()
	if _, err := Fetch(context.Background(), srv.URL+"/s.html?q=1"); err != nil || got != "q=1" {
		t.Errorf("err %v, query %q", err, got)
	}
}
