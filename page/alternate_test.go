package page

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rsdoiel/markdownweb/debuglog"
)

// altSite serves what antenna generates for a collection "food": food.html
// declares food_items.md as its Markdown alternate, while food.md is the
// author's feed list. Other pages vary what the HTML head says.
func altSite(t *testing.T) (*httptest.Server, map[string]int) {
	t.Helper()
	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		md := func(body string) {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			w.Write([]byte(body))
		}
		html := func(head, body string) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte("<!doctype html><html><head>" + head + "</head><body>" + body + "</body></html>"))
		}
		switch r.URL.Path {
		case "/food.html":
			// attribute order is not fixed (antenna writes them from a map)
			html(`<meta charset="utf-8"><link href="food_items.md" type="text/markdown" rel="alternate" >`, "<p>news</p>")
		case "/food.md":
			md("# Feed list\n")
		case "/food_items.md":
			md("# The news\n")
		case "/sub/page.html":
			html(`<link rel="alternate" type="text/markdown" href="../other/items.md">`, "")
		case "/other/items.md":
			md("# Relative alternate\n")
		case "/sub/page.md":
			md("# Sibling\n")
		case "/gone.html":
			html(`<link rel="alternate" type="text/markdown" href="gone_items.md">`, "")
		case "/gone.md":
			md("# Sibling used\n")
		case "/legacy.html":
			// antenna also writes this one; it is not a text/markdown alternate
			html(`<link rel="alternate" type="application/markdown" href="index.md">`, "")
		case "/legacy.md":
			md("# Legacy sibling\n")
		case "/index.md":
			md("# Wrong page\n")
		case "/inbody.html":
			html(``, `<link rel="alternate" type="text/markdown" href="food_items.md">`)
		case "/inbody.md":
			md("# Body link ignored\n")
		case "/elsewhere.html":
			html(`<link rel="alternate" type="text/markdown" href="http://other.example/x.md">`, "")
		case "/elsewhere.md":
			md("# Same host sibling\n")
		case "/multi.html":
			html(`<link rel="alternate stylesheet" type="text/markdown" href="no.md"><link rel="ALTERNATE" type="Text/Markdown; charset=utf-8" href="food_items.md">`, "")
		case "/rss.html":
			html(`<link rel="alternate" type="application/rss+xml" href="food.xml">`, "")
		case "/nothing.html":
			html(``, "")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func fetchBody(t *testing.T, addr string) (*Page, error) {
	t.Helper()
	return Fetch(context.Background(), addr)
}

func TestFetchPrefersTheDeclaredAlternateOverTheMdSibling(t *testing.T) {
	srv, hits := altSite(t)
	p, err := fetchBody(t, srv.URL+"/food.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p.URL, "/food_items.md") || !strings.Contains(p.Body, "The news") {
		t.Errorf("got %q %q, want the declared alternate", p.URL, p.Body)
	}
	if hits["/food.md"] != 0 {
		t.Errorf("the sibling was fetched although the page declared an alternate")
	}
}

func TestFetchResolvesARelativeAlternateAgainstTheHTMLPage(t *testing.T) {
	srv, _ := altSite(t)
	p, err := fetchBody(t, srv.URL+"/sub/page.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p.URL, "/other/items.md") {
		t.Errorf("got %q", p.URL)
	}
}

func TestFetchFallsBackToTheSiblingWhenTheAlternateIsMissing(t *testing.T) {
	srv, _ := altSite(t)
	p, err := fetchBody(t, srv.URL+"/gone.html")
	if err != nil || !strings.Contains(p.Body, "Sibling used") {
		t.Errorf("got %v, %v", p, err)
	}
}

func TestFetchIgnoresWhatIsNotATextMarkdownAlternateInTheHead(t *testing.T) {
	srv, _ := altSite(t)
	cases := map[string]string{
		"/legacy.html":    "Legacy sibling",    // application/markdown is not text/markdown
		"/inbody.html":    "Body link ignored", // a link element in the body is not a declaration
		"/elsewhere.html": "Same host sibling", // an alternate on another host is not followed
		"/rss.html":       "",                  // an RSS alternate is not a page; no sibling either
		"/nothing.html":   "",
	}
	for path, want := range cases {
		p, err := fetchBody(t, srv.URL+path)
		if want == "" {
			if err == nil {
				t.Errorf("%s: got a page %q, want the not-Markdown error", path, p.Body)
			}
			continue
		}
		if err != nil || !strings.Contains(p.Body, want) {
			t.Errorf("%s: got %v, %v, want %q", path, p, err, want)
		}
	}
}

func TestFetchReadsRelAndTypeLeniently(t *testing.T) {
	srv, _ := altSite(t)
	p, err := fetchBody(t, srv.URL+"/multi.html")
	if err != nil || !strings.HasSuffix(p.URL, "/food_items.md") {
		t.Errorf("got %v, %v", p, err)
	}
}

// A page that answers Markdown directly never looks at an alternate.
func TestFetchMarkdownNeverLooksForAnAlternate(t *testing.T) {
	srv, hits := altSite(t)
	if _, err := fetchBody(t, srv.URL+"/food.md"); err != nil {
		t.Fatal(err)
	}
	if hits["/food.html"] != 0 {
		t.Error("fetched the HTML page of a Markdown address")
	}
}

// Only the start of an HTML page is read: a large page must not be loaded whole.
func TestFetchReadsOnlyTheHeadOfALargeHTMLPage(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big.html":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><link rel="alternate" type="text/markdown" href="items.md"></head><body>`))
			w.Write([]byte(strings.Repeat("<p>filler</p>\n", 2_000_000))) // about 28 MB
		case "/items.md":
			w.Header().Set("Content-Type", "text/markdown")
			w.Write([]byte("# Items\n"))
		}
	}))
	defer big.Close()
	p, err := Fetch(context.Background(), big.URL+"/big.html")
	if err != nil || !strings.Contains(p.Body, "Items") {
		t.Errorf("got %v, %v", p, err)
	}
}

// logged runs fn with a debug logger in the context and returns the events.
func logged(t *testing.T, fn func(ctx context.Context)) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	fn(debuglog.WithContext(context.Background(), debuglog.New(&buf)))
	var evs []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("not JSON: %q", line)
		}
		evs = append(evs, m)
	}
	return evs
}

func TestFetchLogsTheChainThatProducedThePage(t *testing.T) {
	srv, _ := altSite(t)
	evs := logged(t, func(ctx context.Context) {
		if _, err := Fetch(ctx, srv.URL+"/food.html"); err != nil {
			t.Fatal(err)
		}
	})
	var tries []map[string]any
	var result map[string]any
	for _, e := range evs {
		switch e["event"] {
		case "fetch.try":
			tries = append(tries, e)
		case "fetch.result":
			result = e
		}
	}
	if len(tries) != 2 {
		t.Fatalf("want a try of the same address and one of the alternate, got %v", evs)
	}
	if tries[0]["role"] != "same" || tries[0]["status"] != float64(200) || tries[0]["outcome"] != "not_markdown" ||
		!strings.HasPrefix(tries[0]["content_type"].(string), "text/html") {
		t.Errorf("first try: %v", tries[0])
	}
	if tries[1]["role"] != "alternate" || tries[1]["outcome"] != "markdown" || !strings.HasSuffix(tries[1]["url"].(string), "/food_items.md") {
		t.Errorf("second try: %v", tries[1])
	}
	if result == nil || result["via"] != "alternate" || !strings.HasSuffix(result["final"].(string), "/food_items.md") || result["requested"] != srv.URL+"/food.html" {
		t.Errorf("result: %v", result)
	}
	for _, e := range tries {
		if _, ok := e["ms"]; !ok {
			t.Errorf("no timing: %v", e)
		}
		if _, ok := e["bytes"]; !ok {
			t.Errorf("no size: %v", e)
		}
	}
}

func TestFetchLogsWhyTheSiblingWasUsed(t *testing.T) {
	srv, _ := altSite(t)
	evs := logged(t, func(ctx context.Context) {
		if _, err := Fetch(ctx, srv.URL+"/gone.html"); err != nil {
			t.Fatal(err)
		}
	})
	var roles, outcomes []string
	var via string
	for _, e := range evs {
		if e["event"] == "fetch.try" {
			roles = append(roles, e["role"].(string))
			outcomes = append(outcomes, e["outcome"].(string))
		}
		if e["event"] == "fetch.result" {
			via = e["via"].(string)
		}
	}
	if strings.Join(roles, ",") != "same,alternate,sibling" || strings.Join(outcomes, ",") != "not_markdown,not_found,markdown" || via != "sibling" {
		t.Errorf("roles %v outcomes %v via %q", roles, outcomes, via)
	}
}

func TestFetchLogsAFailure(t *testing.T) {
	srv, _ := altSite(t)
	evs := logged(t, func(ctx context.Context) {
		if _, err := Fetch(ctx, srv.URL+"/nothing.html"); err == nil {
			t.Fatal("want an error")
		}
	})
	last := evs[len(evs)-1]
	if last["event"] != "fetch.result" || last["via"] != "none" || last["err"] == nil {
		t.Errorf("last event %v", last)
	}
}

// Logging records no page content.
func TestFetchLogHoldsNoPageContent(t *testing.T) {
	srv, _ := altSite(t)
	var buf bytes.Buffer
	ctx := debuglog.WithContext(context.Background(), debuglog.New(&buf))
	if _, err := Fetch(ctx, srv.URL+"/food.html"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "The news") || strings.Contains(buf.String(), "<p>news</p>") {
		t.Errorf("log holds page content:\n%s", buf.String())
	}
}

// A server that is not a Markdown site at all (here, one that answers 200 with
// no Content-Type and names itself in Server) must be recognisable from the
// error and from the log, not just "is not Markdown ()".
func TestNotMarkdownErrorNamesTheContentTypeAndTheServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "oatpp/1.4.0")
		if r.URL.Path != "/" {
			http.NotFound(w, r) // as hailo-ollama does for /index.md
			return
		}
		w.Header()["Content-Type"] = nil // no Content-Type at all
		w.Write([]byte("hailo-ollama is running"))
	}))
	defer srv.Close()
	var buf bytes.Buffer
	ctx := debuglog.WithContext(context.Background(), debuglog.New(&buf))
	_, err := Fetch(ctx, srv.URL)
	if err == nil {
		t.Fatal("want an error")
	}
	msg := err.Error()
	if strings.Contains(msg, "()") || !strings.Contains(msg, "no Content-Type") || !strings.Contains(msg, "oatpp/1.4.0") {
		t.Errorf("message %q: want no empty parentheses, 'no Content-Type' and the Server", msg)
	}
	if !strings.Contains(buf.String(), `"server":"oatpp/1.4.0"`) {
		t.Errorf("log lacks the server:\n%s", buf.String())
	}
}

func TestNotMarkdownErrorStillNamesAContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<p>hi</p>"))
	}))
	defer srv.Close()
	_, err := Fetch(context.Background(), srv.URL+"/x")
	if err == nil || !strings.Contains(err.Error(), "text/html") || strings.Contains(err.Error(), "no Content-Type") {
		t.Errorf("err %v", err)
	}
}
