package toc

import (
	"strings"
	"testing"
)

// antennaTOC is the shape antenna writes (antennaApp DR-0013): plain groups,
// and Posts nested year, month, newest first.
const antennaTOC = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
	<head><title>example.org</title><dateModified>Fri, 20 Nov 2026 04:59:50 GMT</dateModified></head>
	<body>
		<outline text="Pages">
			<outline text="A" url="http://x.example/a.md"></outline>
			<outline text="B" url="http://x.example/b.md"></outline>
			<outline text="C" url="http://x.example/c.md"></outline>
		</outline>
		<outline text="Collections">
			<outline text="News" url="http://x.example/news_items.md"></outline>
		</outline>
		<outline text="Posts">
			<outline text="2026" type="calendarYear" name="2026">
				<outline text="November 2026" type="calendarMonth" name="11">
					<outline text="New" url="http://x.example/blog/n.md"></outline>
					<outline text="Mid" url="http://x.example/blog/m.md"></outline>
				</outline>
				<outline text="October 2026" type="calendarMonth" name="10">
					<outline text="Old" url="http://x.example/blog/o.md"></outline>
				</outline>
			</outline>
		</outline>
	</body>
</opml>`

func mustParse(t *testing.T, src, base string) *Doc {
	t.Helper()
	d, err := Parse([]byte(src), base)
	if err != nil {
		t.Fatalf("Parse: %s", err)
	}
	return d
}

func nb(t *testing.T, d *Doc, page string) (string, string, bool) {
	t.Helper()
	return d.Neighbours(page)
}

// markdownweb DR-0004: one flat list for the whole outline, as BonBon reads it
// (getTocNeighbors): the line above is previous, the line below is next, the
// groups do not stop anything and the ends wrap. This test used to say that
// Pages was a list of its own whose arrows stopped at A and C.
func TestPagesGoByLineAcrossGroupsAndWrapAtTheEnds(t *testing.T) {
	d := mustParse(t, antennaTOC, "http://x.example/.well-known/toc.opml")
	for page, want := range map[string][2]string{
		"http://x.example/a.md":          {"http://x.example/blog/o.md", "http://x.example/b.md"}, // the first wraps to the last
		"http://x.example/b.md":          {"http://x.example/a.md", "http://x.example/c.md"},
		"http://x.example/c.md":          {"http://x.example/b.md", "http://x.example/news_items.md"}, // Pages runs on into Collections
		"http://x.example/news_items.md": {"http://x.example/c.md", "http://x.example/blog/n.md"},     // and Collections into Posts
	} {
		prev, next, ok := nb(t, d, page)
		if !ok || prev != want[0] || next != want[1] {
			t.Errorf("%s: prev %q next %q ok %v, want %q and %q", page, prev, next, ok, want[0], want[1])
		}
	}
}

// BonBon does not reverse calendar outlines: it reads the lines downward. This
// test used to expect the newest post's previous to be the next line down (the
// older post), antennaApp DR-0013 decision 5's rule for antenna's own HTML. A
// file written newest first now gives the newer page as previous, as BonBon does.
func TestPostsAreNotReversedWhateverTheCalendarSays(t *testing.T) {
	d := mustParse(t, antennaTOC, "http://x.example/.well-known/toc.opml")
	for page, want := range map[string][2]string{
		"http://x.example/blog/n.md": {"http://x.example/news_items.md", "http://x.example/blog/m.md"},
		"http://x.example/blog/m.md": {"http://x.example/blog/n.md", "http://x.example/blog/o.md"}, // across the month boundary, the line below
		"http://x.example/blog/o.md": {"http://x.example/blog/m.md", "http://x.example/a.md"},      // the last wraps to the first
	} {
		prev, next, ok := nb(t, d, page)
		if !ok || prev != want[0] || next != want[1] {
			t.Errorf("%s: prev %q next %q ok %v, want %q and %q", page, prev, next, ok, want[0], want[1])
		}
	}
}

// Only a site with one page has no neighbours (BonBon: "all the way around").
// This test used to take a single-entry group for that case.
func TestASitesOnlyPageHasNoNeighbours(t *testing.T) {
	d := mustParse(t, `<opml version="2.0"><body><outline text="G"><outline text="x" url="http://x.example/only.md"/></outline></body></opml>`, "http://x.example/t")
	prev, next, ok := nb(t, d, "http://x.example/only.md")
	if !ok || prev != "" || next != "" {
		t.Errorf("prev %q next %q ok %v", prev, next, ok)
	}
	// a page listed twice under two addresses is still one page
	d = mustParse(t, `<opml version="2.0"><body><outline text="x" url="http://x.example/only.md"/><outline text="y" url="http://x.example/only.html"/></body></opml>`, "http://x.example/t")
	if prev, next, ok := nb(t, d, "http://x.example/only.md"); !ok || prev != "" || next != "" {
		t.Errorf("prev %q next %q ok %v", prev, next, ok)
	}
}

func TestAPageThatIsNotListedHasNoAnswer(t *testing.T) {
	d := mustParse(t, antennaTOC, "http://x.example/.well-known/toc.opml")
	if _, _, ok := nb(t, d, "http://x.example/unlisted.md"); ok {
		t.Error("an unlisted page was found")
	}
}

// Dave Winer's published file: ISO-8859-1, calendar outlines, a month outline
// that is itself a page (an index) with the days under it, newest first.
func daveTOC() string {
	return "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>\n<opml version=\"2.0\">\n\t<head>\n\t\t<title>Table of contents</title>\n\t\t</head>\n\t<body>\n" +
		"\t\t<outline text=\"2026\" type=\"calendarYear\" name=\"2026\" >\n" +
		"\t\t\t<outline text=\"November 2026\" type=\"calendarMonth\" name=\"11\" url=\"https://d.example/s/2026/11/index.md\" >\n" +
		"\t\t\t\t<outline text=\"November 19\" type=\"calendarDay\" name=\"19\" url=\"https://d.example/s/2026/11/19/a.md\" />\n" +
		"\t\t\t\t<outline text=\"Caf\xe9 day\" type=\"calendarDay\" name=\"18\" url=\"https://d.example/s/2026/11/18/b.md\" />\n" +
		"\t\t\t</outline>\n" +
		"\t\t\t<outline text=\"October 2026\" type=\"calendarMonth\" name=\"10\" url=\"https://d.example/s/2026/10/index.md\" >\n" +
		"\t\t\t\t<outline text=\"October 31\" type=\"calendarDay\" name=\"31\" url=\"https://d.example/s/2026/10/31/c.md\" />\n" +
		"\t\t\t</outline>\n" +
		"\t\t</outline>\n" +
		"\t\t<outline text=\"2025\" type=\"calendarYear\" name=\"2025\" >\n" +
		"\t\t\t<outline text=\"December 2025\" type=\"calendarMonth\" name=\"12\" url=\"https://d.example/s/2025/12/index.md\" >\n" +
		"\t\t\t\t<outline text=\"December 30\" type=\"calendarDay\" name=\"30\" url=\"https://d.example/s/2025/12/30/d.md\" />\n" +
		"\t\t\t</outline>\n" +
		"\t\t</outline>\n" +
		"\t</body>\n</opml>\n"
}

// Dave Winer's file read as BonBon reads it (markdownweb DR-0004): the month
// outline that is itself a page is a stop, and its first day follows it; the
// days are not reordered, so "next" is the older day, the opposite of what the
// calendar rule gave before. Dave's own pages carry urlPrev and urlNext in front
// matter, which win; this is what a page that names none gets.
func TestDavesFileIsReadLineByLineMonthIndexesAreStops(t *testing.T) {
	d := mustParse(t, daveTOC(), "https://d.example/s/.well-known/toc.opml")
	p := func(s string) string { return "https://d.example/s/" + s }
	cases := map[string][2]string{
		p("2026/11/index.md"): {p("2025/12/30/d.md"), p("2026/11/19/a.md")}, // the first line wraps to the last
		p("2026/11/19/a.md"):  {p("2026/11/index.md"), p("2026/11/18/b.md")},
		p("2026/11/18/b.md"):  {p("2026/11/19/a.md"), p("2026/10/index.md")}, // the next month's page, then its days
		p("2026/10/31/c.md"):  {p("2026/10/index.md"), p("2025/12/index.md")},
		p("2025/12/30/d.md"):  {p("2025/12/index.md"), p("2026/11/index.md")}, // the last wraps to the first
	}
	for page, want := range cases {
		prev, next, ok := nb(t, d, page)
		if !ok || prev != want[0] || next != want[1] {
			t.Errorf("%s: prev %q next %q ok %v, want %q and %q", page, prev, next, ok, want[0], want[1])
		}
	}
	// Entries is the older, leaves-only question and is unchanged: no month pages
	for _, e := range d.Entries() {
		if strings.HasSuffix(e.URL, "index.md") {
			t.Errorf("Entries lists a page that has outlines under it: %s", e.URL)
		}
	}
	// the text is decoded from ISO-8859-1
	found := false
	for _, e := range d.Entries() {
		if e.Text == "Café day" {
			found = true
		}
	}
	if !found {
		t.Errorf("ISO-8859-1 text not decoded: %+v", d.Entries())
	}
}

func TestOtherEncodingsAreRead(t *testing.T) {
	for _, enc := range []string{"iso-8859-1", "ISO-8859-1", "latin1", "US-ASCII", "windows-1252", "UTF-8", "utf-8"} {
		src := "<?xml version=\"1.0\" encoding=\"" + enc + "\"?><opml version=\"2.0\"><body><outline text=\"G\"><outline text=\"x\" url=\"http://x.example/a.md\"/></outline></body></opml>"
		if _, err := Parse([]byte(src), "http://x.example/.well-known/toc.opml"); err != nil {
			t.Errorf("%s: %s", enc, err)
		}
	}
	if _, err := Parse([]byte(`<?xml version="1.0" encoding="EBCDIC-NOPE"?><opml><body/></opml>`), "http://x.example/t"); err == nil {
		t.Error("an unknown encoding was accepted")
	}
	// windows-1252 smart quotes: byte 0x93 is a left double quote
	d := mustParse(t, "<?xml version=\"1.0\" encoding=\"windows-1252\"?><opml><body><outline text=\"G\"><outline text=\"\x93q\x94\" url=\"http://x.example/a.md\"/></outline></body></opml>", "http://x.example/t")
	if e := d.Entries(); len(e) != 1 || e[0].Text != "“q”" {
		t.Errorf("windows-1252: %+v", e)
	}
}

func TestAddressesAreResolvedAgainstTheTableOfContents(t *testing.T) {
	src := `<opml version="2.0"><body><outline text="G">
		<outline text="rel" url="b.md"/><outline text="root" url="/c.md"/><outline text="abs" url="http://y.example/d.md"/></outline></body></opml>`
	d := mustParse(t, src, "http://x.example/s/.well-known/toc.opml")
	var got []string
	for _, e := range d.Entries() {
		got = append(got, e.URL)
	}
	want := "http://x.example/s/.well-known/b.md,http://x.example/c.md,http://y.example/d.md"
	if strings.Join(got, ",") != want {
		t.Errorf("addresses %v", got)
	}
}

// A page is found by any of the forms an address takes.
func TestPagesAreMatchedWhateverFormTheirAddressTakes(t *testing.T) {
	src := `<opml version="2.0"><body><outline text="G">
		<outline text="one" url="http://x.example/p/one.md"/><outline text="idx" url="http://x.example/dir/index.md"/><outline text="two" url="http://x.example/p/two.md"/></outline></body></opml>`
	d := mustParse(t, src, "http://x.example/.well-known/toc.opml")
	for _, page := range []string{
		"http://x.example/p/one.md",
		"http://x.example/p/one.html",       // the HTML address of the same page
		"http://x.example/p/one.md#section", // a fragment
		"http://X.EXAMPLE/p/one.md",         // host case
		"http://x.example/dir/",             // a folder is its index.md
		"http://x.example/dir/index.html",
	} {
		if _, _, ok := nb(t, d, page); !ok {
			t.Errorf("%s was not found", page)
		}
	}
	if _, _, ok := nb(t, d, "https://x.example/p/one.md"); ok {
		t.Error("another scheme matched")
	}
	if prev, next, _ := nb(t, d, "http://x.example/dir/"); prev != "http://x.example/p/one.md" || next != "http://x.example/p/two.md" {
		t.Errorf("folder neighbours %q %q", prev, next)
	}
}

func TestIncludesAreListed(t *testing.T) {
	src := `<opml version="2.0"><body><outline text="Scripting News" type="include" url="/scripting/.well-known/toc.opml"/>
		<outline text="G"><outline text="x" url="/a.md"/></outline></body></opml>`
	d := mustParse(t, src, "http://x.example/.well-known/toc.opml")
	if inc := d.Includes(); len(inc) != 1 || inc[0] != "http://x.example/scripting/.well-known/toc.opml" {
		t.Errorf("includes %v", inc)
	}
	// an include is not a page
	for _, e := range d.Entries() {
		if e.URL == "http://x.example/scripting/.well-known/toc.opml" {
			t.Error("an include outline was listed as a page")
		}
	}
}

func TestSplicingAnIncludeMakesItsOutlinesPartOfTheDocument(t *testing.T) {
	root := mustParse(t, `<opml version="2.0"><body><outline text="Section" type="include" url="/s/.well-known/toc.opml"/></body></opml>`, "http://x.example/.well-known/toc.opml")
	child := mustParse(t, antennaTOC, "http://x.example/.well-known/toc.opml")
	root.Splice("http://x.example/s/.well-known/toc.opml", child)
	if _, next, ok := nb(t, root, "http://x.example/a.md"); !ok || next != "http://x.example/b.md" {
		t.Errorf("included entries are not part of the document: %q %v", next, ok)
	}
}

func TestBadDocuments(t *testing.T) {
	// not XML at all, or cut off: an error
	for name, src := range map[string]string{
		"not xml":   "this is not xml",
		"truncated": "<opml><body><outline text=\"x\"",
		"empty":     "",
	} {
		if _, err := Parse([]byte(src), "http://x.example/t"); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	// well-formed but not an outline of pages: no entries, never a page found
	for name, src := range map[string]string{
		"html":       "<html><body><p>hi</p></body></html>",
		"wrong root": `<rss version="2.0"><channel/></rss>`,
	} {
		if d, err := Parse([]byte(src), "http://x.example/t"); err == nil && len(d.Entries()) != 0 {
			t.Errorf("%s: parsed into %d entries", name, len(d.Entries()))
		}
	}
	if d, err := Parse([]byte(`<opml version="2.0"><head/><body/></opml>`), "http://x.example/t"); err != nil || len(d.Entries()) != 0 {
		t.Errorf("an empty outline is a document with no entries, not an error: %v", err)
	}
}

// antenna writes Posts oldest first since the BonBon source showed BonBon reads
// the lines downward (antennaApp DR-0013 amendment). The direction is read from
// the numbers in the calendar outlines' name attributes.
const oldestFirstTOC = `<opml version="2.0"><body><outline text="Posts">
	<outline text="2025" type="calendarYear" name="2025">
		<outline text="December 2025" type="calendarMonth" name="12">
			<outline text="Old" url="http://x.example/o.md"/></outline></outline>
	<outline text="2026" type="calendarYear" name="2026">
		<outline text="October 2026" type="calendarMonth" name="10">
			<outline text="Mid" url="http://x.example/m.md"/></outline>
		<outline text="November 2026" type="calendarMonth" name="11">
			<outline text="New" url="http://x.example/n.md"/></outline></outline>
</outline></body></opml>`

// Oldest first or newest first, the lines are read downward (DR-0004). antenna
// writes oldest first (antennaApp DR-0013 amendment) because BonBon does; the
// direction detection that read the name attributes is gone with the reversal.
func TestOldestFirstPostsAreReadDownward(t *testing.T) {
	d := mustParse(t, oldestFirstTOC, "http://x.example/.well-known/toc.opml")
	for page, want := range map[string][2]string{
		"http://x.example/o.md": {"http://x.example/n.md", "http://x.example/m.md"},
		"http://x.example/m.md": {"http://x.example/o.md", "http://x.example/n.md"},
		"http://x.example/n.md": {"http://x.example/m.md", "http://x.example/o.md"},
	} {
		prev, next, ok := nb(t, d, page)
		if !ok || prev != want[0] || next != want[1] {
			t.Errorf("%s: prev %q next %q ok %v, want %q and %q", page, prev, next, ok, want[0], want[1])
		}
	}
}

// This test used to expect Dave's newest first to stand when the names had
// nothing to compare, so the first line's previous was the second. Now the next
// line down is next, always.
func TestWithNothingToCompareTheNextLineIsNext(t *testing.T) {
	src := `<opml version="2.0"><body><outline text="Posts"><outline text="2026" type="calendarYear" name="2026">
		<outline text="November 2026" type="calendarMonth" name="11">
		<outline text="A" url="http://x.example/a.md"/><outline text="B" url="http://x.example/b.md"/><outline text="C" url="http://x.example/c.md"/></outline></outline></outline></body></opml>`
	d := mustParse(t, src, "http://x.example/.well-known/toc.opml")
	if prev, next, _ := nb(t, d, "http://x.example/a.md"); prev != "http://x.example/c.md" || next != "http://x.example/b.md" {
		t.Errorf("prev %q next %q", prev, next)
	}
}

// antenna writes every collection's items page under one Collections group, in
// antenna.yaml order. The group is a list of its own: `]` and `[` step between
// the collections, stop at its ends, and do not run on into Pages or Posts.
// The .html form of an items address (what a link may name) finds the same entry.
const antennaCollectionsTOC = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
	<head><title>x.example/antenna</title></head>
	<body>
		<outline text="Pages">
			<outline text="Home" url="http://x.example/antenna/index.md"></outline>
			<outline text="Search" url="http://x.example/antenna/search.md"></outline>
		</outline>
		<outline text="Collections">
			<outline text="Columns" url="http://x.example/antenna/columns_items.md"></outline>
			<outline text="Craft" url="http://x.example/antenna/craft_items.md"></outline>
			<outline text="Pacific" url="http://x.example/antenna/pacific_items.md"></outline>
			<outline text="ebooks.md" url="http://x.example/antenna/ebooks_items.md"></outline>
		</outline>
	</body>
</opml>`

// antenna writes every collection's items page under one Collections group. In
// BonBon's one list the group does not stop the arrows: they run on from Pages
// into Collections and wrap at the ends (this test used to expect each group to
// be a list of its own). The .html form of an items address finds the same entry.
func TestCollectionsAreStopsInTheOneListAndTheEndsWrap(t *testing.T) {
	d := mustParse(t, antennaCollectionsTOC, "http://x.example/antenna/.well-known/toc.opml")
	const base = "http://x.example/antenna/"
	for page, want := range map[string][2]string{
		base + "index.md":           {base + "ebooks_items.md", base + "search.md"},
		base + "search.md":          {base + "index.md", base + "columns_items.md"}, // Pages runs on into Collections
		base + "columns_items.md":   {base + "search.md", base + "craft_items.md"},
		base + "pacific_items.md":   {base + "craft_items.md", base + "ebooks_items.md"},
		base + "pacific_items.html": {base + "craft_items.md", base + "ebooks_items.md"},
		base + "ebooks_items.md":    {base + "pacific_items.md", base + "index.md"},
	} {
		prev, next, ok := nb(t, d, page)
		if !ok || prev != want[0] || next != want[1] {
			t.Errorf("%s: prev %q next %q ok %v, want %q and %q", page, prev, next, ok, want[0], want[1])
		}
	}
}
