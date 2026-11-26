package toc

// Tests for markdownweb DR-0003 (the flat list of pages for crawlers) and the
// Finder's move onto fetch.Fetcher. They go through Finder.Pages and
// Finder.PagesLocal as cmarkfind will, against httptest servers and real
// files, not through the walker's internals.

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rsdoiel/markdownweb/debuglog"
	"github.com/rsdoiel/markdownweb/fetch"
	"github.com/rsdoiel/markdownweb/page"
)

func outline(body ...string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><opml version="2.0"><head/><body>` + strings.Join(body, "") + `</body></opml>`
}

func pg(text, address string) string { return `<outline text="` + text + `" url="` + address + `"/>` }

func inc(text, address string) string {
	return `<outline text="` + text + `" type="include" url="` + address + `"/>`
}

func urlsOf(l *Listing) []string {
	var out []string
	for _, p := range l.Pages {
		out = append(out, p.URL)
	}
	return out
}

func sameList(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func skipReasons(l *Listing) map[string]string {
	m := map[string]string{}
	for _, s := range l.Skipped {
		m[s.Address] = s.Reason
	}
	return m
}

func mustPages(t *testing.T, f *Finder, start string, lim Limits) *Listing {
	t.Helper()
	l, err := f.Pages(context.Background(), start, lim)
	if err != nil {
		t.Fatalf("Pages(%s): %v", start, err)
	}
	return l
}

// ---- the list ----------------------------------------------------------

func TestPagesListsEveryPageInDocumentOrderIncludesInPlace(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml": outline(
			`<outline text="Pages">`, pg("Home", "{base}/index.md"),
			`<outline text="2026/11" url="{base}/2026/11/index.md">`, pg("Day 1", "{base}/2026/11/1.md"), pg("Day 2", "{base}/2026/11/2.md"), `</outline>`,
			`</outline>`,
			inc("More", "{base}/more/.well-known/toc.opml"),
			pg("Last", "{base}/z.md")),
		"/more/.well-known/toc.opml": outline(pg("M1", "{base}/more/1.md"), pg("M2", "{base}/more/2.md")),
	})
	l := mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{})
	want := []string{"/index.md", "/2026/11/index.md", "/2026/11/1.md", "/2026/11/2.md", "/more/1.md", "/more/2.md", "/z.md"}
	for i := range want {
		want[i] = site.URL + want[i]
	}
	if !sameList(urlsOf(l), want) {
		t.Fatalf("got %v\nwant %v", urlsOf(l), want)
	}
	if !l.Complete || len(l.Skipped) != 0 {
		t.Errorf("Complete %v Skipped %v", l.Complete, l.Skipped)
	}
	byURL := map[string]Page{}
	for _, p := range l.Pages {
		byURL[p.URL] = p
	}
	if p := byURL[site.URL+"/2026/11/index.md"]; !p.Group || p.Title != "2026/11" {
		t.Errorf("a page with outlines under it is a Group: %+v", p)
	}
	if p := byURL[site.URL+"/2026/11/1.md"]; p.Group || p.Title != "Day 1" || p.Depth != 0 || p.TOC != site.URL+"/.well-known/toc.opml" {
		t.Errorf("leaf: %+v", p)
	}
	if p := byURL[site.URL+"/more/2.md"]; p.Depth != 1 || p.TOC != site.URL+"/more/.well-known/toc.opml" || p.Title != "M2" {
		t.Errorf("a page of an include carries the include's own address and depth 1: %+v", p)
	}
}

// BonBon does not reverse or sort (DR-0003, the BonBon rule): the author's
// order is the order, whichever way the calendar outlines run.
func TestPagesAreNotReorderedWhateverTheCalendarSays(t *testing.T) {
	for name, years := range map[string][2]string{"newest first": {"2026", "2025"}, "oldest first": {"2025", "2026"}} {
		site := newTOCSite(t, map[string]string{"/.well-known/toc.opml": outline(
			`<outline text="`+years[0]+`" type="calendarYear" name="`+years[0]+`">`, pg("a", "{base}/"+years[0]+"/a.md"), `</outline>`,
			`<outline text="`+years[1]+`" type="calendarYear" name="`+years[1]+`">`, pg("b", "{base}/"+years[1]+"/b.md"), `</outline>`)})
		l := mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{})
		want := []string{site.URL + "/" + years[0] + "/a.md", site.URL + "/" + years[1] + "/b.md"}
		if !sameList(urlsOf(l), want) {
			t.Errorf("%s: got %v want %v", name, urlsOf(l), want)
		}
	}
}

func TestAPageIsListedOnceWhateverFormItsAddressTakes(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml": outline(pg("first", "{base}/a.md"), pg("again", "{base}/a.html"), pg("frag", "{base}/a.md#top"), inc("i", "{base}/i.opml"), pg("dir", "{base}/d/"), pg("index", "{base}/d/index.md")),
		"/i.opml":               outline(pg("in the include", "{base}/a.html")),
	})
	l := mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{})
	want := []string{site.URL + "/a.md", site.URL + "/d/"}
	if !sameList(urlsOf(l), want) {
		t.Fatalf("got %v want %v", urlsOf(l), want)
	}
	if l.Pages[0].Title != "first" {
		t.Errorf("the first listing wins: %+v", l.Pages[0])
	}
}

func TestAllPagesOfADocKeepsPagesThatHaveOutlinesUnderThem(t *testing.T) {
	d, err := Parse([]byte(outline(
		`<outline text="g"><outline text="m" url="http://h.example/m.md"><outline text="d" url="http://h.example/d.md"/></outline></outline>`,
		inc("i", "http://h.example/i.opml"))), "http://h.example/toc.opml")
	if err != nil {
		t.Fatal(err)
	}
	all := d.AllPages()
	if len(all) != 2 || all[0].URL != "http://h.example/m.md" || !all[0].Group || all[1].URL != "http://h.example/d.md" || all[1].Group {
		t.Errorf("%+v", all)
	}
	if e := d.Entries(); len(e) != 1 || e[0].URL != "http://h.example/d.md" {
		t.Errorf("Entries is unchanged, leaves only: %+v", e)
	}
}

// ---- limits ------------------------------------------------------------

func chain(t *testing.T) *tocSite {
	return newTOCSite(t, map[string]string{
		"/.well-known/toc.opml": outline(pg("r", "{base}/r.md"), inc("a", "{base}/a.opml")),
		"/a.opml":               outline(pg("a", "{base}/a.md"), inc("b", "{base}/b.opml")),
		"/b.opml":               outline(pg("b", "{base}/b.md"), inc("c", "{base}/c.opml")),
		"/c.opml":               outline(pg("c", "{base}/c.md")),
	})
}

func TestIncludeDepthDefaultsToOneLevelAsBonBon(t *testing.T) {
	site := chain(t)
	start := site.URL + "/.well-known/toc.opml"
	l := mustPages(t, NewFinder(), start, Limits{})
	if want := []string{site.URL + "/r.md", site.URL + "/a.md"}; !sameList(urlsOf(l), want) {
		t.Errorf("got %v want %v", urlsOf(l), want)
	}
	if l.Complete {
		t.Error("a depth limit that left something out makes the list incomplete")
	}
	if got := skipReasons(l)[site.URL+"/b.opml"]; got != SkipDepth {
		t.Errorf("skipped %v", l.Skipped)
	}
	if site.count("/b.opml") != 0 {
		t.Error("an include beyond the depth limit was fetched")
	}
	l = mustPages(t, NewFinder(), start, Limits{Depth: 2})
	if len(l.Pages) != 3 || l.Pages[2].URL != site.URL+"/b.md" || l.Pages[2].Depth != 2 {
		t.Errorf("Depth 2: %v", urlsOf(l))
	}
	l = mustPages(t, NewFinder(), start, Limits{Depth: -1})
	if len(l.Pages) != 4 || !l.Complete {
		t.Errorf("a negative limit is no limit: %v %v", urlsOf(l), l.Skipped)
	}
}

func TestIncludeDepthZeroInTheStructIsTheDefaultNotNoIncludes(t *testing.T) {
	site := chain(t)
	l := mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{Pages: 100})
	if len(l.Pages) != 2 {
		t.Errorf("an unset Depth means the default of one level: %v", urlsOf(l))
	}
}

func TestThePagesLimitStopsTheListAndSaysSo(t *testing.T) {
	site := newTOCSite(t, map[string]string{"/.well-known/toc.opml": outline(
		pg("1", "{base}/1.md"), pg("2", "{base}/2.md"), pg("3", "{base}/3.md"), pg("4", "{base}/4.md"), pg("5", "{base}/5.md"))})
	l := mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{Pages: 3})
	if len(l.Pages) != 3 || l.Complete {
		t.Fatalf("%v complete=%v", urlsOf(l), l.Complete)
	}
	found := false
	for _, s := range l.Skipped {
		found = found || s.Reason == SkipPages
	}
	if !found {
		t.Errorf("no skip says the pages limit was reached: %v", l.Skipped)
	}
	// exactly at the limit is complete
	l = mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{Pages: 5})
	if len(l.Pages) != 5 || !l.Complete {
		t.Errorf("5 pages under a limit of 5: %v %v", urlsOf(l), l.Skipped)
	}
}

func TestThePagesLimitStopsAnIncludeInTheMiddleToo(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml": outline(pg("r", "{base}/r.md"), inc("a", "{base}/a.opml"), pg("z", "{base}/z.md")),
		"/a.opml":               outline(pg("a1", "{base}/a1.md"), pg("a2", "{base}/a2.md")),
	})
	l := mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{Pages: 2})
	if want := []string{site.URL + "/r.md", site.URL + "/a1.md"}; !sameList(urlsOf(l), want) || l.Complete {
		t.Errorf("%v complete=%v", urlsOf(l), l.Complete)
	}
}

func TestTheFilesLimitCountsOutlineFilesReadIncludingTheFirst(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml": outline(inc("a", "{base}/a.opml"), inc("b", "{base}/b.opml"), inc("c", "{base}/c.opml")),
		"/a.opml":               outline(pg("a", "{base}/a.md")),
		"/b.opml":               outline(pg("b", "{base}/b.md")),
		"/c.opml":               outline(pg("c", "{base}/c.md")),
	})
	l := mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{Files: 2})
	if want := []string{site.URL + "/a.md"}; !sameList(urlsOf(l), want) || l.Complete {
		t.Fatalf("%v complete=%v", urlsOf(l), l.Complete)
	}
	r := skipReasons(l)
	if r[site.URL+"/b.opml"] != SkipFiles || r[site.URL+"/c.opml"] != SkipFiles {
		t.Errorf("%v", l.Skipped)
	}
	if site.count("/b.opml") != 0 || site.count("/c.opml") != 0 {
		t.Error("a file over the limit was fetched")
	}
}

func TestTheBytesLimitCapsTheSumOfOutlineBytes(t *testing.T) {
	rootBody := outline(pg("r", "{base}/r.md"), inc("a", "{base}/a.opml"))
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml": rootBody,
		"/a.opml":               outline(pg("a", "{base}/a.md")),
	})
	size := int64(len(strings.ReplaceAll(rootBody, "{base}", site.URL)))
	l := mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{Bytes: size + 10})
	if len(l.Pages) != 1 || l.Complete || skipReasons(l)[site.URL+"/a.opml"] != SkipBytes {
		t.Errorf("%v complete=%v %v", urlsOf(l), l.Complete, l.Skipped)
	}
	l = mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{Bytes: size + 4096})
	if len(l.Pages) != 2 || !l.Complete {
		t.Errorf("under the limit: %v %v", urlsOf(l), l.Skipped)
	}
}

// ---- hosts -------------------------------------------------------------

func TestOtherHostsAreSkippedAndReportedUnlessTheCallerNamesThem(t *testing.T) {
	other := newTOCSite(t, map[string]string{"/inc.opml": outline(pg("o", "{base}/o.md"))})
	site := newTOCSite(t, map[string]string{"/.well-known/toc.opml": outline(
		pg("mine", "{base}/mine.md"), pg("theirs", other.URL+"/theirs.md"), inc("inc", other.URL+"/inc.opml"))})
	start := site.URL + "/.well-known/toc.opml"
	l := mustPages(t, NewFinder(), start, Limits{})
	if want := []string{site.URL + "/mine.md"}; !sameList(urlsOf(l), want) {
		t.Fatalf("got %v", urlsOf(l))
	}
	r := skipReasons(l)
	if r[other.URL+"/theirs.md"] != SkipHost || r[other.URL+"/inc.opml"] != SkipHost {
		t.Errorf("%v", l.Skipped)
	}
	if !l.Complete {
		t.Error("a host rule is not a limit: Complete stays true")
	}
	if other.count("/inc.opml") != 0 {
		t.Error("an include on an unnamed host was fetched")
	}
	u, _ := url.Parse(other.URL)
	l = mustPages(t, NewFinder(), start, Limits{Hosts: []string{u.Host}})
	if want := []string{site.URL + "/mine.md", other.URL + "/theirs.md", other.URL + "/o.md"}; !sameList(urlsOf(l), want) {
		t.Errorf("with the host named: %v %v", urlsOf(l), l.Skipped)
	}
}

// ---- politeness: the Fetcher -------------------------------------------

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
}

func TestTheWalkGoesThroughTheFetcherPauseAndUserAgent(t *testing.T) {
	var mu sync.Mutex
	var agents []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		agents = append(agents, r.Header.Get("User-Agent"))
		mu.Unlock()
		base := "http://" + r.Host
		switch r.URL.Path {
		case "/.well-known/toc.opml":
			w.Write([]byte(outline(pg("r", base+"/r.md"), inc("a", base+"/a.opml"), inc("b", base+"/b.opml"))))
		default:
			w.Write([]byte(outline(pg("x", base+r.URL.Path+".md"))))
		}
	}))
	defer srv.Close()
	clk := &fakeClock{now: time.Unix(1000, 0)}
	f := NewFinderWith(fetch.New(fetch.Options{UserAgent: "cmarkfind/9", Pause: 500 * time.Millisecond, Now: clk.Now, Sleep: clk.Sleep}))
	l := mustPages(t, f, srv.URL+"/.well-known/toc.opml", Limits{})
	if len(l.Pages) != 3 {
		t.Fatalf("%v %v", urlsOf(l), l.Skipped)
	}
	if len(clk.sleeps) != 2 || clk.sleeps[0] != 500*time.Millisecond {
		t.Errorf("three requests to one host pause twice: %v", clk.sleeps)
	}
	for _, a := range agents {
		if a != "cmarkfind/9" {
			t.Errorf("User-Agent %q", a)
		}
	}
}

func TestRobotsApplyToIncludesWhenTheCallerTurnsThemOn(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/robots.txt":           "User-agent: *\nDisallow: /private/\n",
		"/.well-known/toc.opml": outline(pg("r", "{base}/r.md"), inc("p", "{base}/private/inc.opml"), inc("ok", "{base}/ok.opml")),
		"/private/inc.opml":     outline(pg("secret", "{base}/private/s.md")),
		"/ok.opml":              outline(pg("fine", "{base}/fine.md")),
	})
	start := site.URL + "/.well-known/toc.opml"
	l := mustPages(t, NewFinderWith(fetch.New(fetch.Options{Robots: true})), start, Limits{})
	if want := []string{site.URL + "/r.md", site.URL + "/fine.md"}; !sameList(urlsOf(l), want) {
		t.Fatalf("got %v %v", urlsOf(l), l.Skipped)
	}
	if skipReasons(l)[site.URL+"/private/inc.opml"] != SkipForbidden {
		t.Errorf("%v", l.Skipped)
	}
	if site.count("/private/inc.opml") != 0 {
		t.Error("a disallowed include was fetched")
	}
	// robots off, the default: DR-0035's case, the include is read
	l = mustPages(t, NewFinderWith(fetch.New(fetch.Options{})), start, Limits{})
	if len(l.Pages) != 3 {
		t.Errorf("robots off: %v %v", urlsOf(l), l.Skipped)
	}
}

func TestAStartOutlineThatRobotsForbidIsAnError(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/robots.txt":           "User-agent: *\nDisallow: /.well-known/\n",
		"/.well-known/toc.opml": outline(pg("r", "{base}/r.md")),
	})
	_, err := NewFinderWith(fetch.New(fetch.Options{Robots: true})).Pages(context.Background(), site.URL+"/.well-known/toc.opml", Limits{})
	if !errors.Is(err, fetch.ErrForbidden) {
		t.Errorf("%v", err)
	}
}

func TestNewFinderUsesThePackageFetcherAtCallTimeForLookupAndPages(t *testing.T) {
	var mu sync.Mutex
	var agents []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		agents = append(agents, r.Header.Get("User-Agent"))
		mu.Unlock()
		base := "http://" + r.Host
		w.Write([]byte(outline(pg("a", base+"/a.md"), pg("b", base+"/b.md"))))
	}))
	defer srv.Close()
	f := NewFinder() // made before the program installs its fetcher, as a browser may
	old := page.SetFetcher(fetch.New(fetch.Options{UserAgent: "cmarkbrowser/7"}))
	defer page.SetFetcher(old)
	if r := f.Lookup(context.Background(), srv.URL+"/a.md"); !r.Found || r.Next != srv.URL+"/b.md" {
		t.Errorf("Lookup: %+v", r)
	}
	mustPages(t, f, srv.URL+"/.well-known/toc.opml", Limits{})
	if len(agents) < 2 {
		t.Fatalf("%v", agents)
	}
	for _, a := range agents {
		if a != "cmarkbrowser/7" {
			t.Errorf("User-Agent %q: toc reads go through page.Fetcher()", a)
		}
	}
}

func TestLookupFindsNoTableOfContentsWhereRobotsForbidIt(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/robots.txt":           "User-agent: *\nDisallow: /\n",
		"/.well-known/toc.opml": outline(pg("a", "{base}/a.md"), pg("b", "{base}/b.md")),
	})
	r := NewFinderWith(fetch.New(fetch.Options{Robots: true})).Lookup(context.Background(), site.URL+"/a.md")
	if r.Found || r.Err != nil {
		t.Errorf("%+v", r)
	}
	if site.count("/.well-known/toc.opml") != 0 {
		t.Error("a disallowed outline was fetched")
	}
}

// ---- errors and failures inside the walk -------------------------------

func TestAStartOutlineThatCannotBeReadIsAnErrorOfTheRightClass(t *testing.T) {
	site := newTOCSite(t, map[string]string{"/soft.opml": "<html><body>not here</body></html>"})
	f := NewFinder()
	for name, start := range map[string]string{"404": site.URL + "/missing.opml", "not an outline": site.URL + "/soft.opml"} {
		if _, err := f.Pages(context.Background(), start, Limits{}); !errors.Is(err, ErrNoOutline) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := f.Pages(context.Background(), "ftp://h.example/toc.opml", Limits{}); !errors.Is(err, fetch.ErrData) {
		t.Errorf("a bad address: %v", err)
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	addr := dead.URL
	dead.Close()
	if _, err := f.Pages(context.Background(), addr+"/toc.opml", Limits{}); !errors.Is(err, fetch.ErrUnavailable) {
		t.Errorf("unreachable: %v", err)
	}
}

func TestAnIncludeThatFailsIsSkippedWithItsReasonAndTheRestIsListed(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml": outline(inc("gone", "{base}/gone.opml"), inc("soft", "{base}/soft.opml"), pg("r", "{base}/r.md")),
		"/soft.opml":            "<html>soft 404</html>",
	})
	l := mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{})
	if want := []string{site.URL + "/r.md"}; !sameList(urlsOf(l), want) {
		t.Fatalf("%v", urlsOf(l))
	}
	r := skipReasons(l)
	if r[site.URL+"/gone.opml"] != SkipUnavailable || r[site.URL+"/soft.opml"] != SkipNotOutline {
		t.Errorf("%v", l.Skipped)
	}
}

// ---- cycles ------------------------------------------------------------

func TestAnOutlineThatIncludesItselfIsReadOnceAndReportedAsALoop(t *testing.T) {
	site := newTOCSite(t, map[string]string{"/.well-known/toc.opml": outline(pg("r", "{base}/r.md"), inc("me", "{base}/.well-known/toc.opml"))})
	l := mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{Depth: 5})
	if len(l.Pages) != 1 || skipReasons(l)[site.URL+"/.well-known/toc.opml"] != SkipLoop {
		t.Errorf("%v %v", urlsOf(l), l.Skipped)
	}
	if site.count("/.well-known/toc.opml") != 1 {
		t.Errorf("fetched %d times", site.count("/.well-known/toc.opml"))
	}
}

func TestTwoOutlinesThatIncludeEachOtherAreEachReadOnce(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml": outline(inc("a", "{base}/a.opml")),
		"/a.opml":               outline(pg("a", "{base}/a.md"), inc("b", "{base}/b.opml")),
		"/b.opml":               outline(pg("b", "{base}/b.md"), inc("a", "{base}/a.opml")),
	})
	l := mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{Depth: 5})
	if want := []string{site.URL + "/a.md", site.URL + "/b.md"}; !sameList(urlsOf(l), want) {
		t.Errorf("%v", urlsOf(l))
	}
	if skipReasons(l)[site.URL+"/a.opml"] != SkipLoop || site.count("/a.opml") != 1 || site.count("/b.opml") != 1 {
		t.Errorf("%v %v", l.Skipped, site.hits)
	}
}

func TestADiamondReadsTheSharedOutlineOnceAndSaysNothing(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml": outline(inc("a", "{base}/a.opml"), inc("b", "{base}/b.opml")),
		"/a.opml":               outline(pg("a", "{base}/a.md"), inc("c", "{base}/c.opml")),
		"/b.opml":               outline(pg("b", "{base}/b.md"), inc("c", "{base}/c.opml")),
		"/c.opml":               outline(pg("c", "{base}/c.md")),
	})
	l := mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{Depth: 3})
	if want := []string{site.URL + "/a.md", site.URL + "/c.md", site.URL + "/b.md"}; !sameList(urlsOf(l), want) {
		t.Errorf("%v", urlsOf(l))
	}
	if len(l.Skipped) != 0 || !l.Complete || site.count("/c.opml") != 1 {
		t.Errorf("a diamond is not a fault: %v hits %v", l.Skipped, site.hits)
	}
}

func TestAnIncludeLoopIsRecognisedWhateverTheCaseOrFragmentOfItsAddress(t *testing.T) {
	site := newTOCSite(t, map[string]string{"/.well-known/toc.opml": outline(inc("me", "{base}/.well-known/toc.opml#again"))})
	l := mustPages(t, NewFinder(), site.URL+"/.well-known/toc.opml", Limits{Depth: 5})
	if skipReasons(l)[site.URL+"/.well-known/toc.opml#again"] != SkipLoop && len(l.Skipped) != 1 {
		t.Errorf("%v", l.Skipped)
	}
	if site.count("/.well-known/toc.opml") != 1 {
		t.Errorf("fetched %d times", site.count("/.well-known/toc.opml"))
	}
}

func TestPagesAreLoggedWithoutContent(t *testing.T) {
	site := newTOCSite(t, map[string]string{"/.well-known/toc.opml": outline(pg("a", "{base}/a.md"), pg("b", "{base}/b.md"))})
	var buf bytes.Buffer
	ctx := debuglog.WithContext(context.Background(), debuglog.New(&buf))
	if _, err := NewFinder().Pages(ctx, site.URL+"/.well-known/toc.opml", Limits{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"event":"toc.pages"`, `"pages":2`, `"complete":true`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("log lacks %s:\n%s", want, buf.String())
		}
	}
}

// ---- a site on disk ----------------------------------------------------

const pub = "https://example.org"

func TestPagesLocalListsPagesWithTheirFilesAndFetchesNothing(t *testing.T) {
	root := localSite(t, map[string]string{
		".well-known/toc.opml": outline(pg("Home", pub+"/index.md"), pg("Post", pub+"/blog/a.html"), pg("Gone", pub+"/blog/gone.md")),
		"index.md":             "# home",
		"blog/a.md":            "# a",
	})
	l, err := NewFinder().PagesLocal(context.Background(), root, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{pub + "/index.md", pub + "/blog/a.html", pub + "/blog/gone.md"}; !sameList(urlsOf(l), want) {
		t.Fatalf("%v", urlsOf(l))
	}
	if l.Pages[0].Path != filepath.Join(root, "index.md") || l.Pages[1].Path != filepath.Join(root, "blog", "a.md") {
		t.Errorf("paths: %+v", l.Pages)
	}
	if l.Pages[2].Path != "" || skipReasons(l)[pub+"/blog/gone.md"] != SkipMissing {
		t.Errorf("a page with no file is kept, with no Path, and reported: %+v %v", l.Pages[2], l.Skipped)
	}
	if l.Pages[0].TOC != filepath.Join(root, ".well-known", "toc.opml") {
		t.Errorf("TOC %q", l.Pages[0].TOC)
	}
	if !l.Complete {
		t.Error("a missing file is not a limit")
	}
}

func TestPagesLocalReadsIncludesUnderTheSamePublishedOriginOnly(t *testing.T) {
	root := localSite(t, map[string]string{
		".well-known/toc.opml":      outline(pg("Home", pub+"/index.md"), inc("Blog", pub+"/blog/.well-known/toc.opml"), inc("Elsewhere", "https://other.example/x/.well-known/toc.opml")),
		"blog/.well-known/toc.opml": outline(pg("A", pub+"/blog/a.md")),
		"index.md":                  "x",
		"blog/a.md":                 "x",
	})
	l, err := NewFinder().PagesLocal(context.Background(), root, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{pub + "/index.md", pub + "/blog/a.md"}; !sameList(urlsOf(l), want) {
		t.Fatalf("%v %v", urlsOf(l), l.Skipped)
	}
	if l.Pages[1].Depth != 1 || l.Pages[1].TOC != filepath.Join(root, "blog", ".well-known", "toc.opml") {
		t.Errorf("%+v", l.Pages[1])
	}
	if skipReasons(l)["https://other.example/x/.well-known/toc.opml"] != SkipHost {
		t.Errorf("%v", l.Skipped)
	}
}

func TestPagesLocalDoesNotLeaveTheRoot(t *testing.T) {
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0o644)
	root := localSite(t, map[string]string{".well-known/toc.opml": outline(pg("Leak", pub+"/leak.md"))})
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "leak.md")); err != nil {
		t.Skip("no symlinks here")
	}
	l, err := NewFinder().PagesLocal(context.Background(), root, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Pages) != 1 || l.Pages[0].Path != "" || skipReasons(l)[pub+"/leak.md"] != SkipMissing {
		t.Errorf("a link out of the root is no file: %+v %v", l.Pages, l.Skipped)
	}
}

func TestPagesLocalAppliesTheLimitsAndTheLoopRule(t *testing.T) {
	root := localSite(t, map[string]string{
		".well-known/toc.opml": outline(pg("1", pub+"/1.md"), pg("2", pub+"/2.md"), pg("3", pub+"/3.md"), inc("me", pub+"/.well-known/toc.opml")),
	})
	l, err := NewFinder().PagesLocal(context.Background(), root, Limits{Pages: 2})
	if err != nil || len(l.Pages) != 2 || l.Complete {
		t.Errorf("%v %v %v", err, urlsOf(l), l.Complete)
	}
	l, err = NewFinder().PagesLocal(context.Background(), root, Limits{Depth: 3})
	if err != nil || len(l.Pages) != 3 || skipReasons(l)[pub+"/.well-known/toc.opml"] != SkipLoop {
		t.Errorf("%v %v %v", err, urlsOf(l), l.Skipped)
	}
}

func TestPagesLocalWithNoOutlineIsAnError(t *testing.T) {
	root := localSite(t, map[string]string{"index.md": "x"})
	if _, err := NewFinder().PagesLocal(context.Background(), root, Limits{}); !errors.Is(err, ErrNoOutline) {
		t.Errorf("%v", err)
	}
}
