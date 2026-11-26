package toc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestTranslateFollowsBonBonsRule(t *testing.T) {
	m := Mirror{Original: "http://scripting.com/", Folder: "https://md.example/scripting/"}
	for link, want := range map[string]string{
		"http://scripting.com/":                     "https://md.example/scripting/",
		"http://scripting.com":                      "https://md.example/scripting/",
		"http://scripting.com/index.html":           "https://md.example/scripting/",
		"https://www.scripting.com/2026/11/05.html": "https://md.example/scripting/2026/11/05.md",
		"http://scripting.com/2026/11/05.html#top":  "https://md.example/scripting/2026/11/05.md#top",
		"http://scripting.com/2026/":                "https://md.example/scripting/2026/",
		"http://scripting.com/rss.xml":              "", // no Markdown counterpart
		"http://scripting.com/pic.png":              "",
		"http://other.com/a.html":                   "",
		"not an address":                            "",
	} {
		if got := m.Translate(link); got != want {
			t.Errorf("Translate(%q) = %q, want %q", link, got, want)
		}
	}
}

func TestTranslateHonoursAPathInTheOriginal(t *testing.T) {
	m := Mirror{Original: "http://h.example/blog/", Folder: "https://md.example/b/"}
	if got := m.Translate("http://h.example/blog/a.html"); got != "https://md.example/b/a.md" {
		t.Errorf("got %q", got)
	}
	if got := m.Translate("http://h.example/other/a.html"); got != "" {
		t.Errorf("outside the folder: %q", got)
	}
}

// settingsSite serves the files given by path and counts requests.
type settingsSite struct {
	*httptest.Server
	mu   sync.Mutex
	hits map[string]int
}

func newSettingsSite(t *testing.T, files map[string]string) *settingsSite {
	s := &settingsSite{hits: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits[r.URL.Path]++
		s.mu.Unlock()
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(strings.ReplaceAll(body, "{base}", "http://"+r.Host)))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *settingsSite) count(p string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[p]
}

func TestMirrorsAreReadFromTheRootAndFromIncludedFolders(t *testing.T) {
	site := newSettingsSite(t, map[string]string{
		"/.well-known/toc.opml": `<opml version="2.0"><body><outline text="x" type="include" url="{base}/scripting/.well-known/toc.opml"/>` +
			`<outline text="far" type="include" url="http://elsewhere.example/.well-known/toc.opml"/></body></opml>`,
		"/scripting/.well-known/toc.opml":    opmlWith(entry("{base}/scripting/a.md")),
		"/scripting/.well-known/bonbon.json": `{"urlMirrorOf": "http://scripting.com/", "urlSearch": "x"}`,
		"/.well-known/bonbon.json":           `{"urlMirrorOf": "http://root.example/"}`,
	})
	f := NewFinder()
	got := f.Mirrors(context.Background(), site.URL+"/scripting/a.md")
	if len(got) != 2 || got[0] != (Mirror{"http://root.example/", site.URL + "/"}) ||
		got[1] != (Mirror{"http://scripting.com/", site.URL + "/scripting/"}) {
		t.Fatalf("%+v", got)
	}
	f.Mirrors(context.Background(), site.URL+"/scripting/a.md")
	if n := site.count("/scripting/.well-known/bonbon.json"); n != 1 {
		t.Errorf("bonbon.json fetched %d times", n)
	}
}

func TestNoBonBonJSONIsReadUntilTheRootTableOfContentsAnswers(t *testing.T) {
	site := newSettingsSite(t, map[string]string{
		"/.well-known/bonbon.json": `{"urlMirrorOf": "http://root.example/"}`,
	})
	if got := NewFinder().Mirrors(context.Background(), site.URL+"/a.md"); len(got) != 0 {
		t.Errorf("%+v", got)
	}
	if site.count("/.well-known/bonbon.json") != 0 {
		t.Error("bonbon.json was fetched with no table of contents")
	}
}

func TestOnlyAStringUrlMirrorOfCounts(t *testing.T) {
	site := newSettingsSite(t, map[string]string{
		"/.well-known/toc.opml":    opmlWith(entry("{base}/a.md")),
		"/.well-known/bonbon.json": `{"urlMirrorOf": ["http://x.example/"], "cname": "y"}`,
	})
	if got := NewFinder().Mirrors(context.Background(), site.URL+"/a.md"); len(got) != 0 {
		t.Errorf("%+v", got)
	}
}
