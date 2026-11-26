package page

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rsdoiel/markdownweb/debuglog"
)

// locationSite answers every path with Markdown and the Content-Location
// header the test gives for it.
func locationSite(t *testing.T, headers map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		if loc, ok := headers[r.URL.Path]; ok {
			w.Header().Set("Content-Location", strings.ReplaceAll(loc, "{host}", r.Host))
		}
		w.Write([]byte("# Page at " + r.URL.Path + "\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestContentLocationBecomesThePagesAddress(t *testing.T) {
	srv := locationSite(t, map[string]string{
		"/dir/":          "/dir/index.md",      // BonBon's folder case: an absolute path
		"/rel/":          "index.md",           // relative to the address asked for
		"/sub/page.html": "../other/items.md",  // in another folder
		"/frag/":         "/frag/index.md#top", // a fragment is not part of an address
		"/q/":            "/q/index.md?v=2",    // a query is
		"/abs/":          "http://{host}/abs/index.md",
	})
	cases := map[string]string{
		"/dir/":          "/dir/index.md",
		"/rel/":          "/rel/index.md",
		"/sub/page.html": "/other/items.md",
		"/frag/":         "/frag/index.md",
		"/q/":            "/q/index.md?v=2",
		"/abs/":          "/abs/index.md",
	}
	for asked, want := range cases {
		p, err := Fetch(context.Background(), srv.URL+asked)
		if err != nil {
			t.Fatalf("%s: %s", asked, err)
		}
		if p.URL != srv.URL+want {
			t.Errorf("%s: URL %q, want %q", asked, p.URL, srv.URL+want)
		}
	}
}

// A server cannot make a page appear to come from somewhere else.
func TestContentLocationOnAnotherHostOrSchemeIsIgnored(t *testing.T) {
	srv := locationSite(t, map[string]string{
		"/other-host/": "http://other.example/x.md",
		"/https/":      "https://{host}/x.md", // same host name, other scheme: not the same origin
		"/file/":       "file:///etc/passwd",
		"/js/":         "javascript:alert(1)",
		"/bad/":        "http://[::1",
		"/empty/":      "",
	})
	for _, asked := range []string{"/other-host/", "/https/", "/file/", "/js/", "/bad/", "/empty/"} {
		p, err := Fetch(context.Background(), srv.URL+asked)
		if err != nil {
			t.Fatalf("%s: %s", asked, err)
		}
		if p.URL != srv.URL+asked {
			t.Errorf("%s: URL %q, want the address asked for", asked, p.URL)
		}
	}
}

func TestNoContentLocationLeavesTheAddressAlone(t *testing.T) {
	srv := locationSite(t, nil)
	p, err := Fetch(context.Background(), srv.URL+"/plain.md")
	if err != nil || p.URL != srv.URL+"/plain.md" {
		t.Errorf("got %v, %v", p, err)
	}
}

// The location is resolved against the address that answered, after redirects.
func TestContentLocationIsRelativeToTheFinalAddress(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/old/", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/new/", http.StatusFound) })
	mux.HandleFunc("/new/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		w.Header().Set("Content-Location", "index.md")
		w.Write([]byte("# New\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	p, err := Fetch(context.Background(), srv.URL+"/old/")
	if err != nil || p.URL != srv.URL+"/new/index.md" {
		t.Errorf("got %v, %v; want /new/index.md", p, err)
	}
}

// It applies to the fallbacks too: a declared alternate or a sibling can carry one.
func TestContentLocationAppliesToAnAlternateFetch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/p.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><link rel="alternate" type="text/markdown" href="a.md"></head></html>`))
	})
	mux.HandleFunc("/a.md", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		w.Header().Set("Content-Location", "/canonical/a.md")
		w.Write([]byte("# A\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	p, err := Fetch(context.Background(), srv.URL+"/p.html")
	if err != nil || p.URL != srv.URL+"/canonical/a.md" {
		t.Errorf("got %v, %v", p, err)
	}
}

func TestContentLocationIsLogged(t *testing.T) {
	srv := locationSite(t, map[string]string{"/dir/": "/dir/index.md"})
	var buf bytes.Buffer
	ctx := debuglog.WithContext(context.Background(), debuglog.New(&buf))
	if _, err := Fetch(ctx, srv.URL+"/dir/"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"content_location":"/dir/index.md"`) {
		t.Errorf("log lacks the Content-Location:\n%s", buf.String())
	}
}
