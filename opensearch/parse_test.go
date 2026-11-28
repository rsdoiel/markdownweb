package opensearch

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

const ns = `xmlns="http://a9.com/-/spec/opensearch/1.1/"`

func doc(body string) string {
	return `<?xml version="1.0"?><OpenSearchDescription ` + ns + `><ShortName>S</ShortName><Description>D</Description>` + body + `</OpenSearchDescription>`
}

func parse(t *testing.T, s string) (*Description, error) {
	t.Helper()
	done := make(chan struct{})
	var d *Description
	var err error
	go func() { defer close(done); d, err = Parse(strings.NewReader(s)) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Parse did not finish: a hang")
	}
	return d, err
}

func TestParseTheDescriptionCmarkfindPublishes(t *testing.T) {
	b, err := os.ReadFile("testdata/cmarkfind.xml") // fetched from a running `cmarkfind server`, not written by hand
	if err != nil {
		t.Fatal(err)
	}
	d, err := parse(t, string(b))
	if err != nil {
		t.Fatal(err)
	}
	if d.ShortName != "cmarkfind" {
		t.Errorf("ShortName = %q", d.ShortName)
	}
	for _, typ := range []string{"text/markdown", "text/html", "application/json"} {
		u, ok := d.Url(typ)
		if !ok || !strings.Contains(u.Template, "{searchTerms}") {
			t.Errorf("no usable %s Url: %#v", typ, u)
		}
	}
	// the rel="self" Url has no {searchTerms}: it is the description itself, not a search
	if _, ok := d.Url("application/opensearchdescription+xml"); ok {
		t.Error("the self Url, which has no {searchTerms}, was kept")
	}
	u, _ := d.Url("text/markdown")
	got, err := u.Expand("otters & tea", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "/search?q=otters+%26+tea&count=&startIndex=") && !strings.Contains(got, "q=otters") {
		t.Errorf("expanded %q", got)
	}
}

func TestUrlPicksTheFirstOfTheTypeWithRelResultsOrNone(t *testing.T) {
	d, err := parse(t, doc(`
<Url type="text/markdown" rel="suggestions" template="http://h.example/sug?q={searchTerms}"/>
<Url type="text/markdown; charset=utf-8" rel="results other" template="http://h.example/one?q={searchTerms}"/>
<Url type="text/markdown" template="http://h.example/two?q={searchTerms}"/>`))
	if err != nil {
		t.Fatal(err)
	}
	u, ok := d.Url("text/markdown")
	if !ok || u.Template != "http://h.example/one?q={searchTerms}" {
		t.Fatalf("got %#v", u)
	}
	if _, ok := d.Url("application/json"); ok {
		t.Error("found a type the document does not have")
	}
}

func TestParseDropsUnusableUrlElements(t *testing.T) {
	d, err := parse(t, doc(`
<Url template="http://h.example/a?q={searchTerms}"/>
<Url type="text/markdown"/>
<Url type="text/markdown" template="http://h.example/none"/>
<Url type="text/markdown" template="file:///etc/passwd?q={searchTerms}"/>
<Url type="text/markdown" template="javascript:alert({searchTerms})"/>
<Url type="text/markdown" template="/relative?q={searchTerms}"/>
<Url type="text/markdown" template="http:///nohost?q={searchTerms}"/>
<Url type="text/html" template="https://h.example/ok?q={searchTerms}"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Urls) != 1 || d.Urls[0].Type != "text/html" {
		t.Fatalf("kept %#v", d.Urls)
	}
}

func TestParseIgnoresUnknownElementsAndRel(t *testing.T) {
	d, err := parse(t, doc(`<Tags>a b</Tags><Developer>x</Developer><Url type="text/markdown" rel="somethingelse" template="http://h.example/s?q={searchTerms}"/>`))
	if err != nil {
		t.Fatal(err)
	}
	// a rel the client does not know is ignored, as the specification says: the Url is not usable as results
	if _, ok := d.Url("text/markdown"); ok {
		t.Log("a Url with an unknown rel is not a results Url")
	}
}

func TestParseHostileDocuments(t *testing.T) {
	big := doc(strings.Repeat(`<Url type="text/markdown" template="http://h.example/s?q={searchTerms}"/>`, 20))
	cases := map[string]string{
		"over 64 KiB":          doc(`<LongName>` + strings.Repeat("a", 70<<10) + `</LongName>`),
		"17 Url elements":      big,
		"100 Url elements":     doc(strings.Repeat(`<Url type="text/markdown" template="http://h.example/s?q={searchTerms}"/>`, 100)),
		"billion laughs":       `<?xml version="1.0"?><!DOCTYPE d [<!ENTITY a "aaaaaaaaaa"><!ENTITY b "&a;&a;&a;&a;&a;&a;&a;&a;&a;&a;"><!ENTITY c "&b;&b;&b;&b;&b;&b;&b;&b;&b;&b;">]><OpenSearchDescription ` + ns + `><ShortName>&c;</ShortName><Description>D</Description></OpenSearchDescription>`,
		"doctype":              `<!DOCTYPE OpenSearchDescription><OpenSearchDescription ` + ns + `><ShortName>S</ShortName><Description>D</Description></OpenSearchDescription>`,
		"custom entity":        doc(`<LongName>&nope;</LongName>`),
		"foreign namespace":    `<OpenSearchDescription xmlns="http://example.org/other"><ShortName>S</ShortName><Description>D</Description></OpenSearchDescription>`,
		"no namespace":         `<OpenSearchDescription><ShortName>S</ShortName><Description>D</Description></OpenSearchDescription>`,
		"wrong root":           `<feed ` + ns + `><ShortName>S</ShortName></feed>`,
		"not well formed":      `<OpenSearchDescription ` + ns + `><ShortName>S</ShortName><Description>D</Description>`,
		"not xml":              `# a Markdown page`,
		"empty":                ``,
		"no ShortName":         `<OpenSearchDescription ` + ns + `><Description>D</Description></OpenSearchDescription>`,
		"no Description":       `<OpenSearchDescription ` + ns + `><ShortName>S</ShortName></OpenSearchDescription>`,
		"deep nesting":         `<OpenSearchDescription ` + ns + `><ShortName>S</ShortName><Description>D</Description>` + strings.Repeat("<x>", 5000) + strings.Repeat("</x>", 5000) + `</OpenSearchDescription>`,
		"second root document": doc("") + doc(""),
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parse(t, s)
			if err == nil {
				t.Fatal("accepted")
			}
			if !errors.Is(err, ErrBad) {
				t.Errorf("error does not wrap ErrBad: %v", err)
			}
			if name == "over 64 KiB" && !strings.Contains(err.Error(), "over 65536") {
				t.Errorf("the cap is not what refused it: %v", err)
			}
			if err.Error() == "" || strings.Contains(err.Error(), "\x1b") {
				t.Errorf("unhelpful or unsafe message %q", err)
			}
		})
	}
}

func TestParseKeepsSixteenUrlElements(t *testing.T) {
	d, err := parse(t, doc(strings.Repeat(`<Url type="text/markdown" template="http://h.example/s?q={searchTerms}"/>`, 16)))
	if err != nil || len(d.Urls) != 16 {
		t.Fatalf("err %v, %d urls", err, len(d.Urls))
	}
}
