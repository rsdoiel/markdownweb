package page

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// otherSite serves things that are neither Markdown nor HTML.
func otherSite(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed.xml":
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprint(w, "<?xml version=\"1.0\"?><rss><channel><title>T</title></channel></rss>")
		case "/toc.opml":
			w.Header().Set("Content-Type", "text/x-opml")
			fmt.Fprint(w, "<opml/>")
		case "/data.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"a": 1}`)
		case "/evil.txt":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "ok\x1b[31mred\x1b]0;pwned\x07\r\nline2 ‮evil\x9b bad")
		case "/fence.txt":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "```\n# break out\n```\n[x](javascript:alert(1)) <script>x</script>")
		case "/big.txt":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, strings.Repeat("0123456789\n", maxSource/11+5000))
		case "/pic.png":
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", "2048")
			w.Write(make([]byte, 2048))
		case "/archive.zip":
			w.Header().Set("Content-Type", "application/zip")
			w.Write([]byte("PK\x03\x04binary"))
		case "/binary-as-text.txt":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("abc\x00def"))
		case "/noext":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprint(w, "a listing")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClassifyFollowsBonBonsKinds(t *testing.T) {
	for _, c := range []struct {
		ct, p string
		want  Kind
	}{
		{"application/rss+xml", "/feed", KindXML}, {"text/x-opml", "/x", KindXML}, {"", "/a.opml", KindXML}, {"application/atom+xml", "/", KindXML},
		{"image/png", "/x", KindImage}, {"", "/a.JPG", KindImage}, {"application/octet-stream", "/a.webp", KindImage},
		{"application/json", "/x", KindText}, {"text/plain; charset=utf-8", "/x", KindText}, {"", "/a.go", KindText}, {"text/css", "/", KindText},
		{"application/zip", "/a.zip", KindFile}, {"application/pdf", "/a.pdf", KindFile}, {"", "/noext", KindFile},
	} {
		if got := Classify(c.ct, c.p); got != c.want {
			t.Errorf("Classify(%q, %q) = %q, want %q", c.ct, c.p, got, c.want)
		}
	}
}

func TestFetchSaysThereIsASourceOrANoteToShow(t *testing.T) {
	srv := otherSite(t)
	for _, p := range []string{"/feed.xml", "/toc.opml", "/data.json", "/pic.png", "/archive.zip"} {
		_, err := Fetch(context.Background(), srv.URL+p)
		if !IsOther(err) || IsHTML(err) || !errors.Is(err, ErrData) {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func TestXMLAndTextAreShownAsSourceInAFence(t *testing.T) {
	srv := otherSite(t)
	for p, want := range map[string]string{"/feed.xml": "<rss><channel>", "/toc.opml": "<opml/>", "/data.json": `{"a": 1}`, "/noext": "a listing"} {
		pg, err := FetchOther(context.Background(), srv.URL+p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if !strings.Contains(pg.Body, "```") || !strings.Contains(pg.Body, want) || pg.Raw == "" || pg.URL != srv.URL+p {
			t.Errorf("%s: %+v", p, pg)
		}
		if k := pg.Kind; k != KindXML && k != KindText {
			t.Errorf("%s: kind %q", p, k)
		}
	}
	pg, _ := FetchOther(context.Background(), srv.URL+"/feed.xml")
	if !strings.Contains(pg.Body, "```xml") || pg.Title() != "feed.xml" {
		t.Errorf("%q %q", pg.Body, pg.Title())
	}
}

func TestControlCharactersAndBidiOverridesNeverReachTheTerminal(t *testing.T) {
	srv := otherSite(t)
	pg, err := FetchOther(context.Background(), srv.URL+"/evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"\x1b", "\x07", "\r", "\x00", "‮", "\x9b"} {
		if strings.Contains(pg.Body, bad) || strings.Contains(pg.Raw, bad) {
			t.Errorf("control %q survived:\n%q", bad, pg.Body)
		}
	}
	if !strings.Contains(pg.Body, "ok[31mred") || !strings.Contains(pg.Body, "line2 evil") {
		t.Errorf("the text itself was lost: %q", pg.Body)
	}
}

func TestASourceCannotBreakOutOfItsFence(t *testing.T) {
	srv := otherSite(t)
	pg, _ := FetchOther(context.Background(), srv.URL+"/fence.txt")
	lines := strings.Split(pg.Body, "\n")
	open := ""
	for _, l := range lines {
		if open == "" && strings.HasPrefix(l, "````") {
			open = l
		}
	}
	if open == "" || !strings.Contains(pg.Body, "\n"+open+"\n") || strings.Count(pg.Body, open) < 2 {
		t.Errorf("the fence must be longer than the longest backtick run in the text:\n%s", pg.Body)
	}
}

func TestBigSourceIsCutAndSaysSo(t *testing.T) {
	srv := otherSite(t)
	pg, err := FetchOther(context.Background(), srv.URL+"/big.txt")
	if err != nil || len(pg.Raw) > maxSource || !strings.Contains(pg.Body, "Cut at") {
		t.Errorf("%v len %d", err, len(pg.Raw))
	}
}

func TestAnImageAndAFileAreNotedNotDownloaded(t *testing.T) {
	srv := otherSite(t)
	pg, err := FetchOther(context.Background(), srv.URL+"/pic.png")
	if err != nil || pg.Kind != KindImage || !strings.Contains(pg.Body, "image/png") || !strings.Contains(pg.Body, "2.0 KB") || !strings.Contains(pg.Body, srv.URL+"/pic.png") || pg.Raw != "" {
		t.Errorf("%v %+v", err, pg)
	}
	pg, err = FetchOther(context.Background(), srv.URL+"/archive.zip")
	if err != nil || pg.Kind != KindFile || !strings.Contains(pg.Body, "application/zip") || !strings.Contains(pg.Body, "not downloaded") {
		t.Errorf("%v %+v", err, pg)
	}
	// a "text" file with a NUL byte is not text
	pg, _ = FetchOther(context.Background(), srv.URL+"/binary-as-text.txt")
	if pg.Kind != KindFile {
		t.Errorf("kind %q", pg.Kind)
	}
}

func TestFetchOtherKeepsTheErrorClasses(t *testing.T) {
	srv := otherSite(t)
	if _, err := FetchOther(context.Background(), srv.URL+"/nope.xml"); !errors.Is(err, ErrNoInput) {
		t.Errorf("404: %v", err)
	}
	if _, err := FetchOther(context.Background(), "http://127.0.0.1:1/x.xml"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("unreachable: %v", err)
	}
	if _, err := FetchOther(context.Background(), "file:///etc/passwd"); !errors.Is(err, ErrData) {
		t.Errorf("file address: %v", err)
	}
}

func TestOtherFromFileUsesTheExtension(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "feed.xml"), []byte("<rss/>"), 0o644)
	os.WriteFile(filepath.Join(dir, "pic.png"), make([]byte, 10), 0o644)
	pg, err := OtherFromFile(filepath.Join(dir, "feed.xml"))
	if err != nil || pg.Kind != KindXML || !strings.Contains(pg.Body, "<rss/>") || !strings.HasPrefix(pg.URL, "file://") {
		t.Errorf("%v %+v", err, pg)
	}
	pg, err = OtherFromFile(filepath.Join(dir, "pic.png"))
	if err != nil || pg.Kind != KindImage || !strings.Contains(pg.Body, "10 bytes") {
		t.Errorf("%v %+v", err, pg)
	}
	if _, err := OtherFromFile(filepath.Join(dir, "missing.xml")); !errors.Is(err, ErrNoInput) {
		t.Errorf("%v", err)
	}
	if _, err := OtherFromFile(dir); !errors.Is(err, ErrNoInput) {
		t.Errorf("a directory: %v", err)
	}
}

func TestHeadingsAndAddressesCannotInjectMarkup(t *testing.T) {
	pg := otherPage(KindFile, "https://x.org/a/`evil`**name**[x](y).bin?q=`", "application/octet-stream", -1, nil)
	// the address is in a code span whose delimiter is longer than any backtick run inside it
	if !strings.Contains(pg.Body, "Address: ``") {
		t.Errorf("the address is not safely quoted:\n%s", pg.Body)
	}
	first := strings.SplitN(pg.Body, "\n", 2)[0]
	if strings.ContainsAny(first[2:], "`*[]") {
		t.Errorf("heading %q", first)
	}
}
