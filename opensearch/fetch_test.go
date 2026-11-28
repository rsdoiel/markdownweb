package opensearch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/rsdoiel/markdownweb/fetch"
)

// counted is a server that counts the requests it gets and says what the last
// one carried.
type counted struct {
	*httptest.Server
	mu      sync.Mutex
	n       int
	cookies int
	refers  int
}

func (c *counted) count() int { c.mu.Lock(); defer c.mu.Unlock(); return c.n }

func newCounted(t *testing.T, h http.HandlerFunc) *counted {
	t.Helper()
	c := &counted{}
	c.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.n++
		if r.Header.Get("Cookie") != "" {
			c.cookies++
		}
		if r.Header.Get("Referer") != "" {
			c.refers++
		}
		c.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(c.Close)
	return c
}

func serveDescription(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/opensearchdescription+xml; charset=utf-8")
		w.Write([]byte(body))
	}
}

// own is a description whose templates are on host.
func own(host string) string {
	return doc(`<Url type="text/markdown" template="` + host + `/search?q={searchTerms}"/>` +
		`<Url type="application/json" template="` + host + `/search.json?q={searchTerms}"/>`)
}

func strict() *fetch.Fetcher { return fetch.New(fetch.Options{SameOrigin: true}) }
func loose() *fetch.Fetcher  { return fetch.New(fetch.Options{}) }

func TestFetchTheRealDescriptionWithItsOwnAddress(t *testing.T) {
	real, err := os.ReadFile("testdata/cmarkfind.xml")
	if err != nil {
		t.Fatal(err)
	}
	var srv *counted
	srv = newCounted(t, func(w http.ResponseWriter, r *http.Request) {
		// the fixture names 127.0.0.1:8911; serve it as this server's own
		body := strings.ReplaceAll(string(real), "http://127.0.0.1:8911", srv.URL)
		serveDescription(body)(w, r)
	})
	d, err := Fetch(context.Background(), strict(), srv.URL+"/opensearch.xml", Policy{Origin: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	u, ok := d.Url("text/markdown")
	if !ok || u.Origin() != srv.URL {
		t.Fatalf("%#v", u)
	}
}

func TestFetchRefusesWhatIsNotADescription(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"html": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(own("http://x")))
		},
		"no type": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "")
			w.Write([]byte(own("http://x")))
		},
		"404": func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) },
		"503": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) },
		"too large": func(w http.ResponseWriter, r *http.Request) {
			serveDescription(doc(`<LongName>`+strings.Repeat("a", 70<<10)+`</LongName>`))(w, r)
		},
		"garbage": serveDescription("not xml"),
	} {
		t.Run(name, func(t *testing.T) {
			srv := newCounted(t, h)
			if _, err := Fetch(context.Background(), strict(), srv.URL+"/d", Policy{Origin: srv.URL}); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestFetchRestrictedNeverReachesAnotherOrigin(t *testing.T) {
	other := newCounted(t, serveDescription(own("http://x")))
	site := newCounted(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redir":
			http.Redirect(w, r, other.URL+"/d", http.StatusFound)
		default:
			serveDescription(own(other.URL))(w, r)
		}
	})
	ctx := context.Background()
	p := Policy{Origin: site.URL}
	// a description at the other origin
	if _, err := Fetch(ctx, strict(), other.URL+"/d", p); !errors.Is(err, ErrCrossOrigin) {
		t.Errorf("description elsewhere: %v", err)
	}
	// a redirect off the site
	if _, err := Fetch(ctx, strict(), site.URL+"/redir", p); err == nil {
		t.Error("followed a redirect off the site")
	}
	// templates elsewhere are dropped, leaving nothing
	if _, err := Fetch(ctx, strict(), site.URL+"/d", p); !errors.Is(err, ErrNoSearch) {
		t.Errorf("templates elsewhere: %v", err)
	}
	if n := other.count(); n != 0 {
		t.Errorf("the other origin was reached %d times", n)
	}
}

func TestFetchRestrictedNeedsASameOriginFetcher(t *testing.T) {
	site := newCounted(t, serveDescription(own("http://x")))
	_, err := Fetch(context.Background(), loose(), site.URL+"/d", Policy{Origin: site.URL})
	if !errors.Is(err, ErrBad) {
		t.Fatalf("err = %v", err)
	}
	if site.count() != 0 {
		t.Error("a request was made")
	}
}

func TestFetchRestrictedKeepsOwnTemplatesAndDropsOthers(t *testing.T) {
	other := newCounted(t, serveDescription(""))
	var site *counted
	site = newCounted(t, func(w http.ResponseWriter, r *http.Request) {
		serveDescription(doc(`<Url type="text/markdown" template="`+site.URL+`/s?q={searchTerms}"/><Url type="text/html" template="`+other.URL+`/s?q={searchTerms}"/>`))(w, r)
	})
	d, err := Fetch(context.Background(), strict(), site.URL+"/d", Policy{Origin: site.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Url("text/markdown"); !ok {
		t.Error("lost its own template")
	}
	if _, ok := d.Url("text/html"); ok {
		t.Error("kept a template on another origin")
	}
	if other.count() != 0 {
		t.Error("the other origin was reached")
	}
}

func TestFetchAnyOriginAllowsTheProvider(t *testing.T) {
	var provider *counted
	provider = newCounted(t, func(w http.ResponseWriter, r *http.Request) {
		serveDescription(own(provider.URL))(w, r)
	})
	site := newCounted(t, func(w http.ResponseWriter, r *http.Request) {})
	d, err := Fetch(context.Background(), loose(), provider.URL+"/d", Policy{Origin: site.URL, AnyOrigin: true})
	if err != nil {
		t.Fatal(err)
	}
	u, ok := d.Url("text/markdown")
	if !ok || u.Origin() != provider.URL || u.Origin() == site.URL {
		t.Fatalf("%#v", u)
	}
	if provider.count() != 1 || site.count() != 0 {
		t.Errorf("requests: provider %d, site %d", provider.count(), site.count())
	}
}

func TestFetchAnyOriginStillRefusesOtherSchemes(t *testing.T) {
	site := newCounted(t, serveDescription(doc(`<Url type="text/markdown" template="file:///etc/passwd?q={searchTerms}"/><Url type="text/html" template="javascript:x({searchTerms})"/>`)))
	_, err := Fetch(context.Background(), loose(), site.URL+"/d", Policy{AnyOrigin: true})
	if !errors.Is(err, ErrNoSearch) {
		t.Fatalf("err = %v", err)
	}
	if _, err := Fetch(context.Background(), loose(), "file:///etc/passwd", Policy{AnyOrigin: true}); err == nil {
		t.Error("fetched a file address")
	}
	if _, err := Fetch(context.Background(), loose(), "ftp://h/x", Policy{AnyOrigin: true}); err == nil {
		t.Error("fetched an ftp address")
	}
}

func TestFetchSendsNoCookiesOrReferer(t *testing.T) {
	site := newCounted(t, serveDescription(own("http://x")))
	Fetch(context.Background(), loose(), site.URL+"/d", Policy{AnyOrigin: true})
	site.mu.Lock()
	defer site.mu.Unlock()
	if site.n == 0 || site.cookies != 0 || site.refers != 0 {
		t.Errorf("n %d cookies %d referers %d", site.n, site.cookies, site.refers)
	}
}

func TestFetchAskedForTheDescriptionType(t *testing.T) {
	var accept string
	site := newCounted(t, func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		serveDescription(own("http://x"))(w, r)
	})
	Fetch(context.Background(), loose(), site.URL+"/d", Policy{AnyOrigin: true})
	if !strings.Contains(accept, "application/opensearchdescription+xml") {
		t.Errorf("Accept = %q", accept)
	}
}
