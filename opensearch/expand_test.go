package opensearch

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func tmpl(s string) Url { return Url{Type: "text/markdown", Template: s} }

func TestExpandEncodesTheTermsAsAQueryValue(t *testing.T) {
	long := strings.Repeat("ab ", 333) + "x" // 1,000 bytes
	for _, terms := range []string{"otters", "a b", "a&b=c", "x#y", "100%", "a+b", "é日本語", "  padded  ", long} {
		got, err := tmpl("http://h.example/search?q={searchTerms}").Expand(terms, 0, 0)
		if err != nil {
			t.Fatalf("%q: %v", terms, err)
		}
		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("%q: %v", terms, err)
		}
		q := u.Query()
		if len(q) != 1 || q.Get("q") != strings.TrimSpace(terms) {
			t.Errorf("%q came back as %q (%d parameters) from %q", terms, q.Get("q"), len(q), got)
		}
		if u.Fragment != "" {
			t.Errorf("%q leaked a fragment: %q", terms, got)
		}
	}
}

func TestExpandOptionalParametersEmptyWhenZeroAndFilledWhenGiven(t *testing.T) {
	u := tmpl("http://h.example/s?q={searchTerms}&count={count?}&startIndex={startIndex?}&l={language?}&x={thing:other?}&e={inputEncoding?}")
	got, err := u.Expand("a", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://h.example/s?q=a&count=&startIndex=&l=&x=&e=" {
		t.Errorf("zero: %q", got)
	}
	got, err = u.Expand("a", 25, 101)
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://h.example/s?q=a&count=25&startIndex=101&l=&x=&e=" {
		t.Errorf("given: %q", got)
	}
}

func TestExpandRefusesAnUnknownRequiredParameter(t *testing.T) {
	_, err := tmpl("http://h.example/s?q={searchTerms}&p={startPage}").Expand("a", 0, 0)
	if !errors.Is(err, ErrUnknownParam) {
		t.Fatalf("err = %v", err)
	}
	_, err = tmpl("http://h.example/s?q={searchTerms}&p={other:thing}").Expand("a", 0, 0)
	if !errors.Is(err, ErrUnknownParam) {
		t.Fatalf("namespaced: err = %v", err)
	}
}

func TestExpandRefusesAnOverLongResult(t *testing.T) {
	_, err := tmpl("http://h.example/s?q={searchTerms}").Expand(strings.Repeat("é", 3000), 0, 0)
	if !errors.Is(err, ErrTooLong) {
		t.Fatalf("err = %v", err)
	}
}

func TestExpandTermsMayBeInThePath(t *testing.T) {
	got, err := tmpl("http://h.example/find/{searchTerms}/page").Expand("a b", 0, 0)
	if err != nil || got != "http://h.example/find/a%20b/page" {
		t.Fatalf("%q, %v", got, err)
	}
}

func TestUrlOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"http://127.0.0.1:8900/search?q={searchTerms}": "http://127.0.0.1:8900",
		"https://Search.Example/s?q={searchTerms}":     "https://search.example",
		"https://search.example:443/s?q={searchTerms}": "https://search.example:443",
	} {
		if got := tmpl(in).Origin(); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
}
