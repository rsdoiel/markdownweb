package toc

// Tests for markdownweb DR-0004: Neighbours follows BonBon's getTocNeighbors
// (BonBon 0.6.57, index.js): one flat list of every outline with a url except
// includes, a parent before the outlines under it; previous is the line above,
// next the line below; the ends wrap; no calendar reversal; a page listed more
// than once gets the nearest different page on each side. Where a test goes
// through Lookup or LookupLocal it is the path cmarkbrowser takes.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func flat(t *testing.T, urls ...string) *Doc {
	t.Helper()
	var b strings.Builder
	b.WriteString(`<opml version="2.0"><body>`)
	for _, u := range urls {
		b.WriteString(`<outline text="t" url="` + u + `"/>`)
	}
	b.WriteString(`</body></opml>`)
	return mustParse(t, b.String(), "http://x.example/.well-known/toc.opml")
}

// An include that is not a site of its own (no "/.well-known/" in its address)
// is read in place, and the arrows run through it. One that is a site is the
// next group of tests.
func TestAnIncludeIsReadInPlaceSoTheArrowsRunThroughIt(t *testing.T) {
	root := mustParse(t, `<opml version="2.0"><body>
		<outline text="first" url="http://x.example/first.md"/>
		<outline text="S" type="include" url="/s/inc.opml"/>
		<outline text="last" url="http://x.example/last.md"/></body></opml>`, "http://x.example/.well-known/toc.opml")
	child := mustParse(t, opmlWith(entry("http://x.example/s/x.md"), entry("http://x.example/s/y.md")), "http://x.example/s/inc.opml")
	root.Splice("http://x.example/s/inc.opml", child)
	for page, want := range map[string][2]string{
		"http://x.example/first.md": {"http://x.example/last.md", "http://x.example/s/x.md"},
		"http://x.example/s/y.md":   {"http://x.example/s/x.md", "http://x.example/last.md"},
		"http://x.example/last.md":  {"http://x.example/s/y.md", "http://x.example/first.md"},
	} {
		if prev, next, ok := nb(t, root, page); !ok || prev != want[0] || next != want[1] {
			t.Errorf("%s: prev %q next %q ok %v", page, prev, next, ok)
		}
	}
}

// BonBon: "a folder's line followed by its readme's line doesn't show the page
// twice". The page is every line that is it; previous is above the first,
// next below the last.
func TestAPageListedMoreThanOnceHasTheNearestDifferentPageOnEachSide(t *testing.T) {
	d := flat(t, "http://x.example/a.md", "http://x.example/dir/", "http://x.example/dir/index.md", "http://x.example/b.md")
	if prev, next, ok := nb(t, d, "http://x.example/dir/"); !ok || prev != "http://x.example/a.md" || next != "http://x.example/b.md" {
		t.Errorf("prev %q next %q ok %v", prev, next, ok)
	}
	// apart: the lines of the page are not together, the nearest different page still
	d = flat(t, "http://x.example/a.md", "http://x.example/b.md", "http://x.example/a.html", "http://x.example/c.md")
	if prev, next, _ := nb(t, d, "http://x.example/a.md"); prev != "http://x.example/c.md" || next != "http://x.example/c.md" {
		t.Errorf("first line wraps, last line runs on: prev %q next %q", prev, next)
	}
	if prev, next, _ := nb(t, d, "http://x.example/b.md"); prev != "http://x.example/a.md" || next != "http://x.example/a.html" {
		t.Errorf("prev %q next %q", prev, next)
	}
}

func TestStopsListTheCandidatesNearestFirstWrappingAndWithoutThePage(t *testing.T) {
	d := flat(t, "http://x.example/a.md", "http://x.example/b.md", "http://x.example/c.md", "http://x.example/d.md")
	before, after, ok := d.Stops("http://x.example/b.md")
	wantBefore := []string{"http://x.example/a.md", "http://x.example/d.md", "http://x.example/c.md"}
	wantAfter := []string{"http://x.example/c.md", "http://x.example/d.md", "http://x.example/a.md"}
	if !ok || !sameList(before, wantBefore) || !sameList(after, wantAfter) {
		t.Errorf("before %v after %v ok %v", before, after, ok)
	}
	// a page listed twice, or two lines that are one page, is one candidate; the page itself never is
	d = flat(t, "http://x.example/a.md", "http://x.example/b.md", "http://x.example/a.html", "http://x.example/c.md", "http://x.example/c.html")
	before, after, _ = d.Stops("http://x.example/b.md")
	if !sameList(before, []string{"http://x.example/a.md", "http://x.example/c.html"}) || !sameList(after, []string{"http://x.example/a.html", "http://x.example/c.md"}) {
		t.Errorf("before %v after %v", before, after)
	}
	if _, _, ok := d.Stops("http://x.example/unlisted.md"); ok {
		t.Error("an unlisted page has stops")
	}
	prev, next, _ := d.Neighbours("http://x.example/b.md")
	if prev != "http://x.example/a.md" || next != "http://x.example/a.html" {
		t.Errorf("Neighbours is the first stop each way: %q %q", prev, next)
	}
}

// The server's Content-Location says a folder's address is its readme: the page
// is found under either address (BonBon: urlPageFile).
func TestNeighboursOfMatchesAPageUnderAnyOfItsAddresses(t *testing.T) {
	d := flat(t, "http://x.example/a.md", "http://x.example/dir/readme.md", "http://x.example/b.md")
	if _, _, ok := d.Neighbours("http://x.example/dir/"); ok {
		t.Fatal("the folder address alone is not listed")
	}
	prev, next, ok := d.NeighboursOf("http://x.example/dir/", "http://x.example/dir/readme.md")
	if !ok || prev != "http://x.example/a.md" || next != "http://x.example/b.md" {
		t.Errorf("prev %q next %q ok %v", prev, next, ok)
	}
	if _, _, ok := d.NeighboursOf(); ok {
		t.Error("no address, no answer")
	}
	before, after, ok := d.StopsOf("http://x.example/dir/", "http://x.example/dir/readme.md")
	if !ok || !sameList(before, []string{"http://x.example/a.md", "http://x.example/b.md"}) || !sameList(after, []string{"http://x.example/b.md", "http://x.example/a.md"}) {
		t.Errorf("before %v after %v ok %v", before, after, ok)
	}
}

func TestAnIncludeThatCouldNotBeReadIsNotAStop(t *testing.T) {
	d := mustParse(t, `<opml version="2.0"><body><outline text="a" url="http://x.example/a.md"/>
		<outline text="gone" type="include" url="http://x.example/gone.opml"/>
		<outline text="b" url="http://x.example/b.md"/></body></opml>`, "http://x.example/.well-known/toc.opml")
	if prev, next, _ := nb(t, d, "http://x.example/a.md"); prev != "http://x.example/b.md" || next != "http://x.example/b.md" {
		t.Errorf("prev %q next %q", prev, next)
	}
}

// ---- through Lookup, the path cmarkbrowser takes --------------------------

func TestLookupWrapsAtBothEnds(t *testing.T) {
	site := newTOCSite(t, map[string]string{"/.well-known/toc.opml": opmlWith(entry("{base}/a.md"), entry("{base}/b.md"), entry("{base}/c.md"))})
	f := NewFinder()
	r := f.Lookup(context.Background(), site.URL+"/a.html")
	if !r.Found || r.Prev != site.URL+"/c.md" || r.Next != site.URL+"/b.md" {
		t.Errorf("first page: %+v", r)
	}
	r = f.Lookup(context.Background(), site.URL+"/c.md")
	if !r.Found || r.Prev != site.URL+"/b.md" || r.Next != site.URL+"/a.md" {
		t.Errorf("last page: %+v", r)
	}
}

// Dave's file, served: the next line down is next, which is the older day. Before
// DR-0004 this gave the newer day as next (newest first).
func TestLookupDoesNotReverseDavesNewestFirstFile(t *testing.T) {
	body := strings.ReplaceAll(daveTOC(), "https://d.example/s", "{base}/s")
	site := newTOCSite(t, map[string]string{"/.well-known/toc.opml": body})
	r := NewFinder().Lookup(context.Background(), site.URL+"/s/2026/11/18/b.html")
	if !r.Found || r.Prev != site.URL+"/s/2026/11/19/a.md" || r.Next != site.URL+"/s/2026/10/index.md" {
		t.Errorf("%+v", r)
	}
}

func TestLookupGivesTheStopsToACallerThatWantsToProbe(t *testing.T) {
	site := newTOCSite(t, map[string]string{"/.well-known/toc.opml": opmlWith(entry("{base}/a.md"), entry("{base}/b.md"), entry("{base}/c.md"))})
	r := NewFinder().Lookup(context.Background(), site.URL+"/b.md")
	if !sameList(r.Before, []string{site.URL + "/a.md", site.URL + "/c.md"}) || !sameList(r.After, []string{site.URL + "/c.md", site.URL + "/a.md"}) {
		t.Errorf("Before %v After %v", r.Before, r.After)
	}
}

// ---- a local site: BonBon asks whether something is at the neighbour's address;
// for files that is free, so a missing file is passed over, not the end of the line

func TestALocalLookupWrapsAndPassesOverALineWithNoFile(t *testing.T) {
	root := localSite(t, map[string]string{
		".well-known/toc.opml": opmlWith(entry("http://h/a.md"), entry("http://h/gone.md"), entry("http://h/c.md"), entry("http://h/also-gone.md")),
		"a.md":                 "# a", "c.md": "# c",
	})
	f := NewFinder()
	r := f.LookupLocal(context.Background(), root, filepath.Join(root, "a.md"))
	if !r.Found || r.Next != filepath.Join(root, "c.md") || r.Prev != filepath.Join(root, "c.md") {
		t.Errorf("a: %+v", r)
	}
	r = f.LookupLocal(context.Background(), root, filepath.Join(root, "c.md"))
	if !r.Found || r.Prev != filepath.Join(root, "a.md") || r.Next != filepath.Join(root, "a.md") {
		t.Errorf("c: %+v", r)
	}
}

func TestStopsByPathFollowTheSameRuleForALocalPage(t *testing.T) {
	d, err := Parse([]byte(opmlWith(entry("http://h/a.md"), entry("http://h/dir/"), entry("http://h/c.html"))), "http://h/")
	if err != nil {
		t.Fatal(err)
	}
	before, after, ok := d.StopsByPath("dir/index.md")
	if !ok || !sameList(before, []string{"http://h/a.md", "http://h/c.html"}) || !sameList(after, []string{"http://h/c.html", "http://h/a.md"}) {
		t.Errorf("before %v after %v ok %v", before, after, ok)
	}
	if prev, next, ok := d.NeighboursByPath("a.md"); !ok || prev != "http://h/c.html" || next != "http://h/dir/" {
		t.Errorf("wraps: %q %q %v", prev, next, ok)
	}
}

// ---- an included site is a list of its own (BonBon getSiteToc) --------------
//
// Found by running BonBon's own getTocNeighbors against rsdoiel.github.io: a
// page whose address is under the folder of a top-level include (the include's
// url up to "/.well-known/") gets only that include's pages as its list, and
// wraps inside it. Pages outside every such folder get the whole domain's list,
// which has the included pages in place. 214 of 220 pages agreed before this;
// the six that did not were the first and last pages of each included site.

const scopedRoot = `<opml version="2.0"><body>
	<outline text="first" url="http://x.example/first.md"/>
	<outline text="S" type="include" url="http://x.example/s/.well-known/toc.opml"/>
	<outline text="last" url="http://x.example/last.md"/></body></opml>`

func scopedDoc(t *testing.T) *Doc {
	t.Helper()
	root := mustParse(t, scopedRoot, "http://x.example/.well-known/toc.opml")
	child := mustParse(t, opmlWith(entry("http://x.example/s/x.md"), entry("http://x.example/s/y.md"), entry("http://x.example/s/z.md")), "http://x.example/s/.well-known/toc.opml")
	root.Splice("http://x.example/s/.well-known/toc.opml", child)
	return root
}

func TestAPageUnderAnIncludedSitesFolderWrapsInsideThatSite(t *testing.T) {
	d := scopedDoc(t)
	for page, want := range map[string][2]string{
		"http://x.example/s/x.md": {"http://x.example/s/z.md", "http://x.example/s/y.md"}, // not first.md: the site is its own list
		"http://x.example/s/y.md": {"http://x.example/s/x.md", "http://x.example/s/z.md"},
		"http://x.example/s/z.md": {"http://x.example/s/y.md", "http://x.example/s/x.md"}, // not last.md
		// the domain's own pages see the whole list, the included site in place
		"http://x.example/first.md": {"http://x.example/last.md", "http://x.example/s/x.md"},
		"http://x.example/last.md":  {"http://x.example/s/z.md", "http://x.example/first.md"},
	} {
		if prev, next, ok := nb(t, d, page); !ok || prev != want[0] || next != want[1] {
			t.Errorf("%s: prev %q next %q ok %v, want %q and %q", page, prev, next, ok, want[0], want[1])
		}
	}
	before, after, _ := d.Stops("http://x.example/s/x.md")
	if !sameList(before, []string{"http://x.example/s/z.md", "http://x.example/s/y.md"}) || !sameList(after, []string{"http://x.example/s/y.md", "http://x.example/s/z.md"}) {
		t.Errorf("Stops stay inside the site: %v %v", before, after)
	}
}

// BonBon: a page under the folder that the included outline does not list has
// no arrows from the table of contents; it does not fall back to the domain's.
func TestAPageUnderTheFolderThatTheIncludedSiteDoesNotListHasNoAnswer(t *testing.T) {
	d := scopedDoc(t)
	if _, _, ok := nb(t, d, "http://x.example/s/unlisted.md"); ok {
		t.Error("an unlisted page under the folder was answered")
	}
}

func TestOnlyATopLevelIncludeWithAWellKnownAddressIsASite(t *testing.T) {
	for name, src := range map[string]string{
		"no .well-known": `<opml version="2.0"><body><outline text="first" url="http://x.example/first.md"/>
			<outline text="S" type="include" url="http://x.example/s/toc.opml"/><outline text="last" url="http://x.example/last.md"/></body></opml>`,
		"inside a group": `<opml version="2.0"><body><outline text="first" url="http://x.example/first.md"/>
			<outline text="G"><outline text="S" type="include" url="http://x.example/s/.well-known/toc.opml"/></outline><outline text="last" url="http://x.example/last.md"/></body></opml>`,
	} {
		root := mustParse(t, src, "http://x.example/.well-known/toc.opml")
		child := mustParse(t, opmlWith(entry("http://x.example/s/x.md"), entry("http://x.example/s/y.md")), "http://x.example/s/toc.opml")
		for _, inc := range root.Includes() {
			root.Splice(inc, child)
		}
		if prev, _, ok := nb(t, root, "http://x.example/s/x.md"); !ok || prev != "http://x.example/first.md" {
			t.Errorf("%s: the list is the whole domain's: prev %q ok %v", name, prev, ok)
		}
	}
}

func TestLookupScopesAPageToTheIncludedSiteItIsIn(t *testing.T) {
	site := newTOCSite(t, map[string]string{
		"/.well-known/toc.opml":   `<opml version="2.0"><body><outline text="f" url="{base}/first.md"/><outline text="S" type="include" url="{base}/s/.well-known/toc.opml"/><outline text="l" url="{base}/last.md"/></body></opml>`,
		"/s/.well-known/toc.opml": opmlWith(entry("{base}/s/x.md"), entry("{base}/s/y.md")),
	})
	f := NewFinder()
	if r := f.Lookup(context.Background(), site.URL+"/s/x.html"); !r.Found || r.Prev != site.URL+"/s/y.md" || r.Next != site.URL+"/s/y.md" {
		t.Errorf("in the site: %+v", r)
	}
	if r := f.Lookup(context.Background(), site.URL+"/last.md"); !r.Found || r.Prev != site.URL+"/s/y.md" || r.Next != site.URL+"/first.md" {
		t.Errorf("outside it: %+v", r)
	}
}

func TestALocalLookupScopesAPageToTheIncludedSiteToo(t *testing.T) {
	root := localSite(t, map[string]string{
		".well-known/toc.opml":   `<opml version="2.0"><body><outline text="f" url="http://h.example/first.md"/><outline text="S" type="include" url="http://h.example/s/.well-known/toc.opml"/><outline text="l" url="http://h.example/last.md"/></body></opml>`,
		"s/.well-known/toc.opml": opmlWith(entry("http://h.example/s/x.md"), entry("http://h.example/s/y.md"), entry("http://h.example/s/z.md")),
		"first.md":               "x", "last.md": "x", "s/x.md": "x", "s/y.md": "x", "s/z.md": "x",
	})
	f := NewFinder()
	r := f.LookupLocal(context.Background(), root, filepath.Join(root, "s", "x.md"))
	if !r.Found || r.Prev != filepath.Join(root, "s", "z.md") || r.Next != filepath.Join(root, "s", "y.md") {
		t.Errorf("in the site: %+v", r)
	}
	r = f.LookupLocal(context.Background(), root, filepath.Join(root, "first.md"))
	if !r.Found || r.Prev != filepath.Join(root, "last.md") || r.Next != filepath.Join(root, "s", "x.md") {
		t.Errorf("outside it: %+v", r)
	}
}
