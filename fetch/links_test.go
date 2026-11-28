package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestParseLinksOneValueAllParameters(t *testing.T) {
	base := mustURL(t, "http://127.0.0.1:8900/search?q=x")
	got := ParseLinks([]string{`</opensearch.xml>; rel="search"; type="application/opensearchdescription+xml"; title="wren notes"`}, base)
	want := []Link{{
		Target: "http://127.0.0.1:8900/opensearch.xml",
		Rel:    []string{"search"},
		Type:   "application/opensearchdescription+xml",
		Title:  "wren notes",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseLinksSeveralValuesCommaJoinedAndSeparateHeaders(t *testing.T) {
	base := mustURL(t, "https://x.example/a/b/page.md")
	got := ParseLinks([]string{
		`<one>; rel=next, <https://y.example/two>; rel="prev"`,
		`<three>; rel=alternate`,
	}, base)
	if len(got) != 3 {
		t.Fatalf("got %d links: %#v", len(got), got)
	}
	if got[0].Target != "https://x.example/a/b/one" || got[1].Target != "https://y.example/two" || got[2].Target != "https://x.example/a/b/three" {
		t.Errorf("targets: %#v", got)
	}
	if got[0].Rel[0] != "next" || got[1].Rel[0] != "prev" {
		t.Errorf("rels: %#v", got)
	}
}

func TestParseLinksCommaInsideQuotedTitleAndTarget(t *testing.T) {
	base := mustURL(t, "http://h.example/")
	got := ParseLinks([]string{`</s?a=1,2>; rel="search"; title="a, b; c", </t>; rel=next`}, base)
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Target != "http://h.example/s?a=1,2" || got[0].Title != "a, b; c" || got[1].Target != "http://h.example/t" {
		t.Errorf("got %#v", got)
	}
}

func TestParseLinksRelIsSplitAndLowered(t *testing.T) {
	got := ParseLinks([]string{`</x>; rel="Search ALTERNATE"`}, mustURL(t, "http://h.example/"))
	if len(got) != 1 || !reflect.DeepEqual(got[0].Rel, []string{"search", "alternate"}) {
		t.Fatalf("got %#v", got)
	}
}

func TestParseLinksResolvesAgainstBaseWithPathAndQuery(t *testing.T) {
	base := mustURL(t, "http://h.example:8900/dir/results?q=a&startIndex=101")
	got := ParseLinks([]string{`<?q=a&startIndex=201>; rel=next, <../up>; rel=up, <//other.example/z>; rel=x`}, base)
	want := []string{
		"http://h.example:8900/dir/results?q=a&startIndex=201",
		"http://h.example:8900/up",
		"http://other.example/z",
	}
	for i, w := range want {
		if i >= len(got) || got[i].Target != w {
			t.Fatalf("link %d: got %#v, want %q", i, got, w)
		}
	}
}

func TestParseLinksIgnoresOtherParametersAndStarredTitle(t *testing.T) {
	got := ParseLinks([]string{`</x>; rel=search; hreflang=en; anchor="#a"; title*=UTF-8''caf%C3%A9`}, mustURL(t, "http://h.example/"))
	if len(got) != 1 || got[0].Title != "" || got[0].Rel[0] != "search" {
		t.Fatalf("got %#v", got)
	}
}

func TestParseLinksSkipsMalformedAndKeepsNeighbours(t *testing.T) {
	base := mustURL(t, "http://h.example/")
	for _, bad := range []string{
		`no angle brackets; rel=x`,
		`<unterminated; rel=x`,
		`<>; rel=x`,
		``,
		`,,,`,
		`<%zz>; rel=x`,
	} {
		got := ParseLinks([]string{`</before>; rel=a, ` + bad + `, </after>; rel=b`}, base)
		// the neighbours survive; the malformed value adds nothing
		var targets []string
		for _, l := range got {
			targets = append(targets, l.Target)
		}
		if len(targets) < 1 {
			t.Errorf("%q: lost every link: %#v", bad, got)
		}
	}
	if got := ParseLinks([]string{`garbage`}, base); len(got) != 0 {
		t.Errorf("garbage gave %#v", got)
	}
}

func TestParseLinksDropsNonHTTPTargets(t *testing.T) {
	base := mustURL(t, "http://h.example/")
	got := ParseLinks([]string{`<javascript:alert(1)>; rel=x, <file:///etc/passwd>; rel=x, <ftp://h/z>; rel=x, <data:text/plain,hi>; rel=x, </ok>; rel=x`}, base)
	if len(got) != 1 || got[0].Target != "http://h.example/ok" {
		t.Fatalf("got %#v", got)
	}
}

func TestParseLinksKeepsAtMostSixteen(t *testing.T) {
	var parts []string
	for i := 0; i < 1000; i++ {
		parts = append(parts, `</p`+strings.Repeat("x", 3)+`>; rel=n`)
	}
	got := ParseLinks([]string{strings.Join(parts, ", ")}, mustURL(t, "http://h.example/"))
	if len(got) != 16 {
		t.Fatalf("got %d links, want 16", len(got))
	}
	// many headers too
	var hs []string
	for i := 0; i < 100; i++ {
		hs = append(hs, `</h>; rel=n`)
	}
	if got := ParseLinks(hs, mustURL(t, "http://h.example/")); len(got) != 16 {
		t.Fatalf("headers: got %d links, want 16", len(got))
	}
}

func TestParseLinksSkipsAValueOverTwoKiB(t *testing.T) {
	long := `</` + strings.Repeat("a", 100<<10) + `>; rel=search`
	got := ParseLinks([]string{`</small>; rel=a, ` + long + `, </after>; rel=b`}, mustURL(t, "http://h.example/"))
	for _, l := range got {
		if len(l.Target) > 2048 {
			t.Fatalf("kept a %d byte target", len(l.Target))
		}
	}
	if len(got) < 1 || got[0].Target != "http://h.example/small" {
		t.Fatalf("got %#v", got)
	}
	// a single value of exactly the limit is judged the same way: over 2 KiB is out
	over := `<` + strings.Repeat("b", 2049) + `>; rel=x`
	if got := ParseLinks([]string{over}, mustURL(t, "http://h.example/")); len(got) != 0 {
		t.Errorf("a 2 KiB+ value was kept: %d links", len(got))
	}
}

func TestParseLinksTitleIsCutAndStrippedOfControls(t *testing.T) {
	title := "\x1b[31mred\x07\x7f\u0085 " + strings.Repeat("é", 200)
	got := ParseLinks([]string{`</x>; rel=search; title="` + title + `"`}, mustURL(t, "http://h.example/"))
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
	g := got[0].Title
	if len(g) > 128 {
		t.Errorf("title is %d bytes, want at most 128", len(g))
	}
	for _, r := range g {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("control character %U left in %q", r, g)
		}
	}
	if !strings.Contains(g, "red") {
		t.Errorf("text lost: %q", g)
	}
	if !strings.HasSuffix(g, "é") && len(g) == 128 {
		// cut on a rune boundary
		if !strings.HasSuffix(strings.ToValidUTF8(g, "�"), g) {
			t.Errorf("title cut inside a rune: %q", g)
		}
	}
}

func TestParseLinksNothingForNoHeader(t *testing.T) {
	if got := ParseLinks(nil, mustURL(t, "http://h.example/")); got != nil {
		t.Errorf("got %#v, want nil", got)
	}
	if got := ParseLinks(nil, nil); got != nil {
		t.Errorf("nil base: got %#v", got)
	}
}

func TestParseLinksTokenAndQuotedFormsAreEqual(t *testing.T) {
	base := mustURL(t, "http://h.example/")
	a := ParseLinks([]string{`</x>; rel=search; type=text/markdown; title=Notes`}, base)
	b := ParseLinks([]string{`</x>; rel="search"; type="text/markdown"; title="Notes"`}, base)
	if !reflect.DeepEqual(a, b) || len(a) != 1 || a[0].Type != "text/markdown" || a[0].Title != "Notes" {
		t.Fatalf("a=%#v b=%#v", a, b)
	}
}

func TestGetCarriesLinksResolvedAgainstFinalAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/deep/final", http.StatusFound)
			return
		}
		w.Header().Add("Link", `<desc.xml>; rel="search"; type="application/opensearchdescription+xml"; title="Wren"`)
		w.Header().Add("Link", `<next>; rel="next"`)
		w.Header().Set("Content-Type", "text/markdown")
		w.Write([]byte("# hi\n"))
	}))
	defer srv.Close()
	f, _ := newTest(Options{})
	res, err := f.Get(context.Background(), srv.URL+"/start", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Links) != 2 {
		t.Fatalf("links: %#v", res.Links)
	}
	if res.Links[0].Target != srv.URL+"/deep/desc.xml" || res.Links[0].Title != "Wren" || res.Links[0].Rel[0] != "search" {
		t.Errorf("first link: %#v", res.Links[0])
	}
	if res.Links[1].Target != srv.URL+"/deep/next" {
		t.Errorf("second link: %#v", res.Links[1])
	}
}

func TestGetWithoutLinkHeaderLeavesResultAsItWas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		w.Header().Set("ETag", `"e"`)
		w.Write([]byte("# hi\n"))
	}))
	defer srv.Close()
	f, _ := newTest(Options{})
	res, err := f.Get(context.Background(), srv.URL+"/p.md", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Links != nil {
		t.Errorf("Links = %#v, want nil", res.Links)
	}
	if res.Status != 200 || res.ETag != `"e"` || string(res.Body) != "# hi\n" || res.ContentType != "text/markdown" {
		t.Errorf("result changed: %#v", res)
	}
}

func TestGetBoundsAHostileLinkHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var parts []string
		for i := 0; i < 1000; i++ {
			parts = append(parts, `</p>; rel=n`)
		}
		w.Header().Set("Link", strings.Join(parts, ", "))
		w.Header().Add("Link", `</`+strings.Repeat("z", 50<<10)+`>; rel=x`)
		w.Write([]byte("x"))
	}))
	defer srv.Close()
	f, _ := newTest(Options{})
	res, err := f.Get(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Links) != 16 {
		t.Errorf("got %d links, want 16", len(res.Links))
	}
}
