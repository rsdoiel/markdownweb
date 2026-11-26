package toc

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rsdoiel/markdownweb/debuglog"
)

// tocSite serves outlines at the paths given and counts requests per path.
type tocSite struct {
	*httptest.Server
	mu   sync.Mutex
	hits map[string]int
}

func newTOCSite(t *testing.T, files map[string]string) *tocSite {
	t.Helper()
	s := &tocSite{hits: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits[r.URL.Path]++
		s.mu.Unlock()
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		body = strings.ReplaceAll(body, "{base}", "http://"+r.Host)
		w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
		w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *tocSite) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

func opmlWith(entries ...string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><opml version="2.0"><head/><body><outline text="Pages">` +
		strings.Join(entries, "") + `</outline></body></opml>`
}

func entry(url string) string { return `<outline text="e" url="` + url + `"/>` }

func TestTheRootTableOfContentsIsAskedFirstAndNearerFoldersAreNotProbedWhenItListsThePage(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml": opmlWith(entry("{base}/blog/2026/a.md"), entry("{base}/blog/2026/b.md"), entry("{base}/blog/2026/c.md")),
	})
	f := NewFinder()
	r := f.Lookup(context.Background(), site.URL+"/blog/2026/b.html")
	if !r.Found || r.Prev != site.URL+"/blog/2026/a.md" || r.Next != site.URL+"/blog/2026/c.md" {
		t.Fatalf("result %+v", r)
	}
	if r.TOC != site.URL+"/.well-known/toc.opml" {
		t.Errorf("toc %q", r.TOC)
	}
	// BonBon asks the origin root once; folders nearer the page are not asked
	if site.count("/.well-known/toc.opml") != 1 {
		t.Errorf("root fetched %d times, want 1", site.count("/.well-known/toc.opml"))
	}
	for _, p := range []string{"/blog/2026/.well-known/toc.opml", "/blog/.well-known/toc.opml"} {
		if site.count(p) != 0 {
			t.Errorf("%s fetched %d times, want 0", p, site.count(p))
		}
	}
	// a page in a folder not seen before costs nothing either
	r = f.Lookup(context.Background(), site.URL+"/blog/2027/c.html")
	if r.Found {
		t.Errorf("unlisted page found: %+v", r)
	}
	f.Lookup(context.Background(), site.URL+"/blog/2026/c.html")
	if site.count("/.well-known/toc.opml") != 1 {
		t.Errorf("root fetched %d times after more lookups", site.count("/.well-known/toc.opml"))
	}
}

func TestMissesAreRememberedWhenNoRootListsThePage(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/blog/.well-known/toc.opml": opmlWith(entry("{base}/blog/2026/a.md"), entry("{base}/blog/2026/b.md")),
	})
	f := NewFinder()
	for i := 0; i < 2; i++ {
		r := f.Lookup(context.Background(), site.URL+"/blog/2026/a.html")
		if !r.Found || r.Next != site.URL+"/blog/2026/b.md" {
			t.Fatalf("result %+v", r)
		}
	}
	for _, p := range []string{"/.well-known/toc.opml", "/blog/2026/.well-known/toc.opml", "/blog/.well-known/toc.opml"} {
		if site.count(p) != 1 {
			t.Errorf("%s fetched %d times, want 1", p, site.count(p))
		}
	}
}

func TestTheRootWinsOverADeeperTableOfContentsWhenBothListThePage(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml":   opmlWith(entry("{base}/s/a.md"), entry("{base}/s/b.md")),
		"/s/.well-known/toc.opml": opmlWith(entry("{base}/s/x.md"), entry("{base}/s/a.md"), entry("{base}/s/y.md")),
	})
	r := NewFinder().Lookup(context.Background(), site.URL+"/s/a.md")
	if r.TOC != site.URL+"/.well-known/toc.opml" || r.Next != site.URL+"/s/b.md" {
		t.Errorf("%+v", r)
	}
}

func TestADeeperTableOfContentsIsUsedWhenTheRootDoesNotListThePage(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml":   opmlWith(entry("{base}/other.md")),
		"/s/.well-known/toc.opml": opmlWith(entry("{base}/s/x.md"), entry("{base}/s/a.md"), entry("{base}/s/y.md")),
	})
	r := NewFinder().Lookup(context.Background(), site.URL+"/s/a.md")
	if r.TOC != site.URL+"/s/.well-known/toc.opml" || r.Prev != site.URL+"/s/x.md" || r.Next != site.URL+"/s/y.md" {
		t.Errorf("%+v", r)
	}
}

func TestATableOfContentsThatDoesNotListThePageIsPassedOver(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/s/.well-known/toc.opml": opmlWith(entry("{base}/s/other.md")),
		"/.well-known/toc.opml":   opmlWith(entry("{base}/s/a.md"), entry("{base}/s/b.md")),
	})
	r := NewFinder().Lookup(context.Background(), site.URL+"/s/a.md")
	if !r.Found || r.TOC != site.URL+"/.well-known/toc.opml" || r.Next != site.URL+"/s/b.md" {
		t.Errorf("%+v", r)
	}
}

func TestNoTableOfContentsMeansNotFoundNotAnError(t *testing.T) {
	site := newTOCSite(t, nil)
	r := NewFinder().Lookup(context.Background(), site.URL+"/a/b/c.md")
	if r.Found || r.Prev != "" || r.Next != "" {
		t.Errorf("%+v", r)
	}
	if r.Err != nil {
		t.Errorf("a site with no table of contents is not an error: %v", r.Err)
	}
}

func TestASiteWhoseFileIsNotAnOutlineIsSkipped(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml": "<html><body>Not here, a soft 404 page</body></html>",
	})
	r := NewFinder().Lookup(context.Background(), site.URL+"/a.md")
	if r.Found {
		t.Errorf("%+v", r)
	}
}

// Dave Winer's root file is one include pointing at the section's own file.
func TestIncludesOnTheSameHostAreMergedAndOthersAreNot(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml": `<opml version="2.0"><body>
			<outline text="Scripting News" type="include" url="{base}/scripting/.well-known/toc.opml"/>
			<outline text="Elsewhere" type="include" url="http://other.example/x/toc.opml"/></body></opml>`,
		"/scripting/.well-known/toc.opml": `<opml version="2.0"><body><outline text="2026" type="calendarYear">
			<outline text="d2" type="calendarDay" url="{base}/scripting/2026/11/19/a.md"/>
			<outline text="d1" type="calendarDay" url="{base}/scripting/2026/11/18/b.md"/></outline></body></opml>`,
	})
	r := NewFinder().Lookup(context.Background(), site.URL+"/scripting/2026/11/19/a.md")
	// the root's outline with the include read in place: a then b, one list that
	// wraps (DR-0004; before, Dave's newest first made b the previous and nothing next)
	if !r.Found || r.Prev != site.URL+"/scripting/2026/11/18/b.md" || r.Next != site.URL+"/scripting/2026/11/18/b.md" {
		t.Errorf("%+v", r)
	}
	// a root-only site: the page is known only through the include
	rootOnly := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml": `<opml version="2.0"><body><outline text="S" type="include" url="{base}/inc.opml"/></body></opml>`,
		"/inc.opml":             opmlWith(entry("{base}/p/a.md"), entry("{base}/p/b.md")),
	})
	r = NewFinder().Lookup(context.Background(), rootOnly.URL+"/p/a.md")
	if !r.Found || r.Next != rootOnly.URL+"/p/b.md" {
		t.Errorf("include not merged: %+v", r)
	}
}

func TestIncludeLoopsAndDeepChainsDoNotRunAway(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml": `<opml version="2.0"><body><outline text="me" type="include" url="{base}/.well-known/toc.opml"/>
			<outline text="a" type="include" url="{base}/a.opml"/></body></opml>`,
		"/a.opml": `<opml version="2.0"><body><outline text="b" type="include" url="{base}/b.opml"/></body></opml>`,
		"/b.opml": `<opml version="2.0"><body><outline text="a" type="include" url="{base}/a.opml"/></body></opml>`,
	})
	r := NewFinder().Lookup(context.Background(), site.URL+"/x.md")
	if r.Found {
		t.Errorf("%+v", r)
	}
	if site.count("/a.opml") > 1 || site.count("/b.opml") > 1 || site.count("/.well-known/toc.opml") > 1 {
		t.Errorf("an include was fetched more than once: %v", site.hits)
	}
}

func TestPagesOnAnotherSchemeOrNonWebAddressesGetNoLookup(t *testing.T) {
	f := NewFinder()
	for _, page := range []string{"file:///tmp/x.md", "ftp://x.example/a.md", "", "not an address"} {
		if r := f.Lookup(context.Background(), page); r.Found || r.Err != nil {
			t.Errorf("%q: %+v", page, r)
		}
	}
}

func TestLookupsAreLoggedWithoutContent(t *testing.T) {
	site := newTOCSite(t, map[string]string{"/.well-known/toc.opml": opmlWith(entry("{base}/a.md"), entry("{base}/b.md"))})
	var buf bytes.Buffer
	ctx := debuglog.WithContext(context.Background(), debuglog.New(&buf))
	NewFinder().Lookup(ctx, site.URL+"/a.md")
	for _, want := range []string{`"event":"toc.fetch"`, `"status":200`, `"entries":2`, `"event":"toc.lookup"`, `"found":true`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("log lacks %s:\n%s", want, buf.String())
		}
	}
}

func TestAHugeFileIsRefused(t *testing.T) {
	big := newTOCSite(t, map[string]string{"/.well-known/toc.opml": opmlWith(strings.Repeat(entry("http://x.example/p.md"), 400000))})
	if r := NewFinder().Lookup(context.Background(), big.URL+"/p.md"); r.Found {
		t.Error("a file over the size cap was read")
	}
}

func TestTheRootIsAlwaysProbedEvenForAVeryDeepPage(t *testing.T) {
	c := candidates("http://h.example/a/b/c/d/e/f/g/h/page.md")
	if len(c) != maxProbes || c[0] != "http://h.example/.well-known/toc.opml" || c[1] != "http://h.example/a/b/c/d/e/f/g/h/.well-known/toc.opml" {
		t.Errorf("%v", c)
	}
}

// localSite writes files under a temp folder and returns its real path.
func localSite(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	real, _ := filepath.EvalSymlinks(dir)
	return real
}

func TestALocalPageIsFoundByTheTailOfItsPathWhateverTheOriginsName(t *testing.T) {
	root := localSite(t, map[string]string{
		".well-known/toc.opml": opmlWith(
			entry("http://harvey.local:8010/blog/a.md"), entry("http://harvey.local:8010/blog/b.md"), entry("http://harvey.local:8010/blog/c.md")),
		"blog/a.md": "# a", "blog/b.md": "# b", "blog/c.md": "# c",
	})
	r := NewFinder().LookupLocal(context.Background(), root, filepath.Join(root, "blog", "b.md"))
	if !r.Found || r.Prev != filepath.Join(root, "blog", "a.md") || r.Next != filepath.Join(root, "blog", "c.md") {
		t.Errorf("%+v", r)
	}
}

func TestALocalSitePublishedUnderASubFolderStillMatches(t *testing.T) {
	root := localSite(t, map[string]string{
		".well-known/toc.opml": opmlWith(entry("https://h.example/antenna/a.md"), entry("https://h.example/antenna/b.md")),
		"a.md":                 "# a", "b.md": "# b",
	})
	r := NewFinder().LookupLocal(context.Background(), root, filepath.Join(root, "a.md"))
	if !r.Found || r.Next != filepath.Join(root, "b.md") || r.Prev != filepath.Join(root, "b.md") { // two pages: the other one both ways (wraps, DR-0004)
		t.Errorf("%+v", r)
	}
}

func TestALocalNeighbourThatHasNoFileIsDropped(t *testing.T) {
	root := localSite(t, map[string]string{
		".well-known/toc.opml": opmlWith(entry("http://h/a.md"), entry("http://h/gone.md")),
		"a.md":                 "# a",
	})
	r := NewFinder().LookupLocal(context.Background(), root, filepath.Join(root, "a.md"))
	if !r.Found || r.Next != "" {
		t.Errorf("%+v", r)
	}
}

func TestLocalIncludesAreReadFromFilesUnderTheRootAndNothingIsFetched(t *testing.T) {
	root := localSite(t, map[string]string{
		".well-known/toc.opml":   `<opml version="2.0"><body><outline text="s" type="include" url="http://h.example/s/.well-known/toc.opml"/></body></opml>`,
		"s/.well-known/toc.opml": opmlWith(entry("http://h.example/s/x.md"), entry("http://h.example/s/y.md")),
		"s/x.md":                 "# x", "s/y.md": "# y",
	})
	r := NewFinder().LookupLocal(context.Background(), root, filepath.Join(root, "s", "x.md"))
	if !r.Found || r.Next != filepath.Join(root, "s", "y.md") {
		t.Errorf("%+v", r)
	}
}

func TestALocalLookupDoesNotLeaveTheRoot(t *testing.T) {
	outer := localSite(t, map[string]string{
		"secret/.well-known/toc.opml": opmlWith(entry("http://h/s/a.md"), entry("http://h/s/b.md")),
		"site/a.md":                   "# a",
	})
	root := filepath.Join(outer, "site")
	// the outline lists a page whose only file is outside the root; and a page outside the root asks nothing
	if r := NewFinder().LookupLocal(context.Background(), root, filepath.Join(outer, "secret", "a.md")); r.Found {
		t.Errorf("a page outside the root: %+v", r)
	}
	os.WriteFile(filepath.Join(outer, "site", "x"), nil, 0o644)
	os.MkdirAll(filepath.Join(root, ".well-known"), 0o755)
	os.Symlink(filepath.Join(outer, "secret", ".well-known", "toc.opml"), filepath.Join(root, ".well-known", "toc.opml"))
	if r := NewFinder().LookupLocal(context.Background(), root, filepath.Join(root, "a.md")); r.Found {
		t.Errorf("a symlinked outline outside the root was read: %+v", r)
	}
}

func TestANonLocalNeighboursByPathHandlesIndexAndHTML(t *testing.T) {
	d, err := Parse([]byte(opmlWith(entry("http://h/a.md"), entry("http://h/dir/"), entry("http://h/c.html"))), "http://h/")
	if err != nil {
		t.Fatal(err)
	}
	if _, next, ok := d.NeighboursByPath("a.md"); !ok || next != "http://h/dir/" {
		t.Errorf("%q %v", next, ok)
	}
	if prev, _, ok := d.NeighboursByPath("c.md"); !ok || prev != "http://h/dir/" {
		t.Errorf("an .html entry matches its .md: %q %v", prev, ok)
	}
	if _, _, ok := d.NeighboursByPath("dir/index.md"); !ok {
		t.Error("a folder entry matches its index.md")
	}
	if _, _, ok := d.NeighboursByPath("xa.md"); ok {
		t.Error("a name that only ends alike matched")
	}
}
