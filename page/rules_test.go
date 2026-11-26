package page

import "testing"

// The shared rules of the Markdown web. antennaApp wrote each of them first and
// cmarkbrowser wrote them again; these cases are the union of both test suites
// plus the places where the two copies disagreed.

func TestPrefersMarkdown(t *testing.T) {
	for accept, want := range map[string]bool{
		"":     false,
		"*/*":  false,
		Accept: true,
		"text/html, application/xhtml+xml, */*;q=0.8": false,
		"text/markdown":                        true,
		"text/markdown;q=0":                    false,
		"text/html, text/markdown":             false, // a tie goes to HTML
		"text/markdown;q=0.8, text/html;q=0.7": true,
		"TEXT/MARKDOWN":                        true,
		"text/markdown; charset=utf-8":         true,
		"garbage;;;q=,,,":                      false,
	} {
		if got := PrefersMarkdown(accept); got != want {
			t.Errorf("PrefersMarkdown(%q) = %v, want %v", accept, got, want)
		}
	}
}

func TestDeclaredAlternateIgnoresABlankHref(t *testing.T) {
	// A blank href resolves to the page itself, which is no alternate.
	head := []byte(`<link rel="alternate" type="text/markdown" href="  ">`)
	if got := DeclaredAlternate("https://x.org/a.html", head); got != "" {
		t.Errorf("blank href gave %q, want none", got)
	}
}

func TestDeclaredAlternateSkipsOffHostForALaterOne(t *testing.T) {
	head := []byte(`<link rel="alternate" type="text/markdown" href="https://evil.org/a.md">` +
		`<link rel="alternate" type="text/markdown" href="a_items.md">`)
	if got, want := DeclaredAlternate("https://x.org/a.html", head), "https://x.org/a_items.md"; got != want {
		t.Errorf("DeclaredAlternate = %q, want %q", got, want)
	}
}

func TestMarkdownPath(t *testing.T) {
	for in, want := range map[string]string{
		"a.html":            "a.md",
		"a.HTM":             "a.md",
		"/a/b.html":         "a/b.md",
		"a.md":              "a.md",
		"./a/../b.markdown": "b.markdown",
		"a.txt":             "",
		"a":                 "",
		"":                  "",
	} {
		if got := MarkdownPath(in); got != want {
			t.Errorf("MarkdownPath(%q) = %q, want %q", in, got, want)
		}
	}
}
