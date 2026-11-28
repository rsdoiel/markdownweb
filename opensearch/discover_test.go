package opensearch

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/rsdoiel/markdownweb/fetch"
)

const descType = "application/opensearchdescription+xml"

func link(target, title string) fetch.Link {
	return fetch.Link{Target: target, Rel: []string{"search"}, Type: descType, Title: title}
}

// site serves /.well-known files from a map, counting requests by path.
type site struct {
	*counted
	mu    sync.Mutex
	paths map[string]int
}

func newSite(t *testing.T, files map[string]string) *site { return newSiteFunc(t, files, nil) }

// newSiteFunc is newSite with a handler that answers instead of the files.
func newSiteFunc(t *testing.T, files map[string]string, override http.HandlerFunc) *site {
	t.Helper()
	s := &site{paths: map[string]int{}}
	s.counted = newCounted(t, func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.paths[r.URL.Path]++
		s.mu.Unlock()
		if override != nil {
			override(w, r)
			return
		}
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	})
	return s
}

func (s *site) hits(p string) int { s.mu.Lock(); defer s.mu.Unlock(); return s.paths[p] }

const (
	cf = "/.well-known/cmarkfind.json"
	bb = "/.well-known/bonbon.json"
)

func discover(t *testing.T, f *fetch.Fetcher, s *site, links []fetch.Link, p Policy) ([]Service, error) {
	t.Helper()
	return Discover(context.Background(), f, s.URL+"/some/deep/page.md", links, p)
}

func TestFromLinksTakesSearchLinksOfTheDescriptionType(t *testing.T) {
	site := "http://127.0.0.1:8900"
	got := FromLinks([]fetch.Link{
		{Target: site + "/next", Rel: []string{"next"}},
		{Target: site + "/rightType", Rel: []string{"alternate"}, Type: descType},
		{Target: site + "/noRel", Type: descType},
		{Target: site + "/other.xml", Rel: []string{"search"}, Type: "text/html"},
		{Target: site + "/untyped", Rel: []string{"search"}},
		{Target: site + "/also", Rel: []string{"alternate", "search"}, Type: descType + "; charset=utf-8", Title: "Both"},
		link(site+"/opensearch.xml", "Wren"),
	}, Policy{Origin: site})
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Description != site+"/also" || got[0].Title != "Both" || got[0].Source != "link" || got[0].CrossOrigin {
		t.Errorf("first: %#v", got[0])
	}
	if got[1].Description != site+"/opensearch.xml" || got[1].Title != "Wren" {
		t.Errorf("second: %#v", got[1])
	}
	if FromLinks(nil, Policy{Origin: site}) != nil {
		t.Error("no links gave services")
	}
}

func TestFromLinksPolicy(t *testing.T) {
	site, other := "http://127.0.0.1:8900", "http://search.example"
	links := []fetch.Link{link(other+"/opensearch.xml", "Provider"), link(site+"/opensearch.xml", "Own")}
	got := FromLinks(links, Policy{Origin: site})
	if len(got) != 1 || got[0].Title != "Own" {
		t.Errorf("restricted: %#v", got)
	}
	got = FromLinks(links, Policy{Origin: site, AnyOrigin: true})
	if len(got) != 2 || !got[0].CrossOrigin || got[1].CrossOrigin {
		t.Errorf("any: %#v", got)
	}
	// the default port is the same origin
	if got := FromLinks([]fetch.Link{link("http://h.example:80/d.xml", "")}, Policy{Origin: "http://h.example"}); len(got) != 1 {
		t.Errorf("default port: %#v", got)
	}
}

func TestDiscoverFromCmarkfindJSONPathAndAbsoluteForms(t *testing.T) {
	files := map[string]string{}
	s := newSite(t, files)
	files[cf] = `{"index": "/cmarkfind/manifest.json", "search": "/opensearch.xml"}`
	got, err := discover(t, strict(), s, nil, Policy{Origin: s.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Description != s.URL+"/opensearch.xml" || got[0].Source != "cmarkfind.json" || got[0].CrossOrigin {
		t.Fatalf("path form: %#v", got)
	}
	files[cf] = `{"search": "` + s.URL + `/find.xml"}`
	got, err = discover(t, strict(), s, nil, Policy{Origin: s.URL})
	if err != nil || len(got) != 1 || got[0].Description != s.URL+"/find.xml" {
		t.Fatalf("absolute form: %#v %v", got, err)
	}
}

func TestDiscoverFromBonbonJSONAppendsTheQuery(t *testing.T) {
	files := map[string]string{}
	s := newSite(t, files)
	for val, want := range map[string]string{
		"/find":          "/find?q={searchTerms}",
		"/find?lang=en":  "/find?lang=en&q={searchTerms}",
		s.URL + "/s":     "/s?q={searchTerms}",
		"/cgi-bin/q.cgi": "/cgi-bin/q.cgi?q={searchTerms}",
	} {
		files[bb] = `{"urlSearch": "` + val + `", "urlMirrorOf": "http://x.example/"}`
		got, err := discover(t, strict(), s, nil, Policy{Origin: s.URL})
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: %#v %v", val, got, err)
		}
		g := got[0]
		if g.Source != "bonbon.json" || g.Description != "" || g.Url.Type != "text/markdown" || g.Url.Template != s.URL+want {
			t.Errorf("%s: %#v", val, g)
		}
		q, err := g.Url.Expand("a b", 25, 101)
		if err != nil || !strings.Contains(q, "q=a+b") || strings.Contains(q, "count") {
			t.Errorf("%s: expanded %q %v", val, q, err)
		}
	}
}

func TestDiscoverOrderAndDuplicates(t *testing.T) {
	files := map[string]string{}
	s := newSite(t, files)
	files[cf] = `{"search": "/opensearch.xml"}`
	files[bb] = `{"urlSearch": "/find"}`
	links := []fetch.Link{link(s.URL+"/opensearch.xml", "Header")}
	got, err := discover(t, strict(), s, links, Policy{Origin: s.URL})
	if err != nil {
		t.Fatal(err)
	}
	// the header and cmarkfind.json name the same description: one service, the header's
	if len(got) != 2 || got[0].Source != "link" || got[0].Title != "Header" || got[1].Source != "bonbon.json" {
		t.Fatalf("got %#v", got)
	}
	files[cf] = `{"search": "/other.xml"}`
	got, _ = discover(t, strict(), s, links, Policy{Origin: s.URL})
	if len(got) != 3 || got[0].Source != "link" || got[1].Source != "cmarkfind.json" || got[2].Source != "bonbon.json" {
		t.Fatalf("order: %#v", got)
	}
}

func TestDiscoverFilesAreReadOncePerCallAtTheSiteRoot(t *testing.T) {
	files := map[string]string{cf: `{"search": "/o.xml"}`, bb: `{"urlSearch": "/f"}`}
	s := newSite(t, files)
	if _, err := discover(t, strict(), s, nil, Policy{Origin: s.URL}); err != nil {
		t.Fatal(err)
	}
	if s.hits(cf) != 1 || s.hits(bb) != 1 || s.count() != 2 {
		t.Errorf("requests: cmarkfind %d, bonbon %d, all %d", s.hits(cf), s.hits(bb), s.count())
	}
}

func TestDiscoverNothingPublishedIsNotAnError(t *testing.T) {
	s := newSite(t, map[string]string{})
	got, err := discover(t, strict(), s, nil, Policy{Origin: s.URL})
	if err != nil || len(got) != 0 {
		t.Fatalf("%#v %v", got, err)
	}
	// a cmarkfind.json with only an index has no search service, and is not wrong
	s = newSite(t, map[string]string{cf: `{"index": "/cmarkfind/manifest.json"}`})
	got, err = discover(t, strict(), s, nil, Policy{Origin: s.URL})
	if err != nil || len(got) != 0 {
		t.Fatalf("index only: %#v %v", got, err)
	}
}

func TestDiscoverIgnoresValuesThatAreNotASearchAddress(t *testing.T) {
	for _, v := range []string{
		`42`, `["/o.xml"]`, `{"a":"/o.xml"}`, `null`, `true`, `""`, `"   "`,
		`"javascript:alert(1)"`, `"file:///etc/passwd"`, `"ftp://h/o.xml"`, `"relative/o.xml"`, `"o.xml"`,
		`"//other.example/o.xml"`, `"/o.xml\u0000"`, `"/o\u001b[31m.xml"`, `"/o\\..\\x"`, `"http:///nohost"`,
	} {
		for _, file := range []string{cf, bb} {
			key := "search"
			if file == bb {
				key = "urlSearch"
			}
			s := newSite(t, map[string]string{file: `{"` + key + `": ` + v + `}`})
			got, err := discover(t, strict(), s, nil, Policy{Origin: s.URL, AnyOrigin: true})
			if err != nil || len(got) != 0 {
				t.Errorf("%s %s: %#v %v", file, v, got, err)
			}
		}
	}
}

func TestDiscoverHostileFiles(t *testing.T) {
	big := `{"search": "/o.xml", "pad": "` + strings.Repeat("a", 70<<10) + `"}`
	for name, body := range map[string]string{
		"over 64 KiB":    big,
		"duplicate keys": `{"search": "/a.xml", "search": "/b.xml"}`,
		"not an object":  `["search"]`,
		"not json":       `search: /o.xml`,
		"two values":     `{"search": "/o.xml"} {"x":1}`,
		"truncated":      `{"search": "/o.xml"`,
	} {
		s := newSite(t, map[string]string{cf: body})
		got, err := discover(t, strict(), s, nil, Policy{Origin: s.URL})
		if len(got) != 0 {
			t.Errorf("%s: services from a bad file: %#v", name, got)
		}
		if !errors.Is(err, ErrBad) {
			t.Errorf("%s: err = %v", name, err)
		}
		if name == "over 64 KiB" && !strings.Contains(err.Error(), "over 65536") {
			t.Errorf("the cap is not what refused it: %v", err)
		}
	}
}

func TestDiscoverOneBadFileDoesNotHideTheGoodOne(t *testing.T) {
	s := newSite(t, map[string]string{cf: `{not json`, bb: `{"urlSearch": "/find"}`})
	got, err := discover(t, strict(), s, []fetch.Link{link(s.URL+"/h.xml", "H")}, Policy{Origin: s.URL})
	if !errors.Is(err, ErrBad) {
		t.Errorf("err = %v", err)
	}
	if len(got) != 2 || got[0].Source != "link" || got[1].Source != "bonbon.json" {
		t.Errorf("got %#v", got)
	}
}

func TestDiscoverServerErrorsAreReportedAndOthersStillRead(t *testing.T) {
	s := newSiteFunc(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == cf {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{"urlSearch": "/find"}`))
	})
	got, err := discover(t, strict(), s, nil, Policy{Origin: s.URL})
	if err == nil || len(got) != 1 {
		t.Fatalf("%#v %v", got, err)
	}
}

func TestDiscoverRestrictedNeverReachesAnotherOrigin(t *testing.T) {
	other := newCounted(t, serveDescription(own("http://x")))
	s := newSite(t, map[string]string{
		cf: `{"search": "` + other.URL + `/opensearch.xml"}`,
		bb: `{"urlSearch": "` + other.URL + `/find"}`,
	})
	got, err := discover(t, strict(), s, []fetch.Link{link(other.URL+"/h.xml", "H")}, Policy{Origin: s.URL})
	if err != nil || len(got) != 0 {
		t.Fatalf("%#v %v", got, err)
	}
	if other.count() != 0 {
		t.Errorf("the other origin was reached %d times", other.count())
	}
}

func TestDiscoverAnyOriginNamesTheProviderWithoutReachingIt(t *testing.T) {
	other := newCounted(t, serveDescription(own("http://x")))
	s := newSite(t, map[string]string{
		cf: `{"search": "` + other.URL + `/opensearch.xml"}`,
		bb: `{"urlSearch": "` + other.URL + `/find"}`,
	})
	got, err := discover(t, loose(), s, nil, Policy{Origin: s.URL, AnyOrigin: true})
	if err != nil || len(got) != 2 {
		t.Fatalf("%#v %v", got, err)
	}
	for _, g := range got {
		if !g.CrossOrigin {
			t.Errorf("not marked cross-origin: %#v", g)
		}
	}
	if got[1].Url.Origin() != other.URL {
		t.Errorf("origin %q", got[1].Url.Origin())
	}
	// listing services never fetches a description: that waits for the reader's query
	if other.count() != 0 {
		t.Errorf("the provider was reached %d times", other.count())
	}
}

func TestDiscoverRestrictedNeedsASameOriginFetcherAndTheSameRoot(t *testing.T) {
	s := newSite(t, map[string]string{cf: `{"search": "/o.xml"}`})
	if _, err := discover(t, loose(), s, nil, Policy{Origin: s.URL}); !errors.Is(err, ErrBad) {
		t.Errorf("loose fetcher: %v", err)
	}
	if _, err := discover(t, strict(), s, nil, Policy{Origin: "http://127.0.0.1:1"}); !errors.Is(err, ErrCrossOrigin) {
		t.Errorf("another offering origin: %v", err)
	}
	if s.count() != 0 {
		t.Errorf("%d requests were made", s.count())
	}
}

func TestDiscoverFileRedirectedOffTheSiteIsRefused(t *testing.T) {
	other := newCounted(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"urlSearch": "/f"}`)) })
	s := newSiteFunc(t, nil, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	})
	got, err := discover(t, strict(), s, nil, Policy{Origin: s.URL})
	if err == nil || len(got) != 0 {
		t.Fatalf("%#v %v", got, err)
	}
	if other.count() != 0 {
		t.Errorf("followed the redirect: %d", other.count())
	}
	// with a fetcher that follows, AnyOrigin still reads only the site's own files
	got, err = discover(t, loose(), s, nil, Policy{Origin: s.URL, AnyOrigin: true})
	if len(got) != 0 {
		t.Errorf("read a file served from another origin: %#v %v", got, err)
	}
}

func TestDiscoverBadSiteAddress(t *testing.T) {
	for _, site := range []string{"", "file:///etc", "ftp://h/", "not a url", "/relative"} {
		if _, err := Discover(context.Background(), strict(), site, nil, Policy{AnyOrigin: true}); err == nil {
			t.Errorf("%q accepted", site)
		}
	}
}
