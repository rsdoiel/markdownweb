package page

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F5: the front matter rules are the frontmatter module's. These are the
// behaviours that differ from cmarkbrowser's own splitter, through the paths a
// page takes in use: Parse, a file, and a server.

func serve(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAnUnclosedBlockIsShownAsTextWithANotice(t *testing.T) {
	src := "---\ntitle: T\nnot closed, so this is the page\n"
	p, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("an unclosed block must not stop the page showing: %v", err)
	}
	if p.Body != src || len(p.Meta) != 0 {
		t.Errorf("the whole file is the body: %#v", p)
	}
	if !strings.Contains(p.Notice, "not closed") {
		t.Errorf("Notice = %q, want it to say the block is not closed", p.Notice)
	}
}

func TestAPageThatIsWellFormedHasNoNotice(t *testing.T) {
	for _, src := range []string{"# Just text\n", "---\ntitle: T\n---\nbody\n", "---\n---\nbody\n"} {
		if p, err := Parse([]byte(src)); err != nil || p.Notice != "" {
			t.Errorf("%q: notice %q, err %v", src, p.Notice, err)
		}
	}
}

func TestAnUnclosedBlockHasANoticeWhenReadFromAFileAndFromAServer(t *testing.T) {
	src := "---\ntitle: T\nnever closed\n"
	path := filepath.Join(t.TempDir(), "u.md")
	os.WriteFile(path, []byte(src), 0o644)
	fp, err := ReadFile(path)
	if err != nil || fp.Body != src || fp.Notice == "" {
		t.Errorf("file: %#v %v", fp, err)
	}
	sp, err := Fetch(context.Background(), serve(t, src).URL+"/u.md")
	if err != nil || sp.Body != src || sp.Notice == "" {
		t.Errorf("server: %#v %v", sp, err)
	}
}

func TestDotsCloseTheBlock(t *testing.T) {
	src := "---\ntitle: T\n...\nBody.\n"
	p, err := Parse([]byte(src))
	if err != nil || p.Title() != "T" || p.Body != "Body.\n" || p.Notice != "" {
		t.Errorf("%#v %v", p, err)
	}
	sp, err := Fetch(context.Background(), serve(t, src).URL+"/d.md")
	if err != nil || sp.Title() != "T" || sp.Body != "Body.\n" {
		t.Errorf("server: %#v %v", sp, err)
	}
}

func TestACRLFBodyIsKeptAsWritten(t *testing.T) {
	src := "---\r\ntitle: T\r\n---\r\nBody.\r\nMore.\r\n"
	p, err := Parse([]byte(src))
	if err != nil || p.Title() != "T" || p.Body != "Body.\r\nMore.\r\n" {
		t.Errorf("%#v %v", p, err)
	}
	if p.Raw != src {
		t.Errorf("Raw must be the page as received: %q", p.Raw)
	}
}

func TestARuleInTheBodyIsNotFrontMatter(t *testing.T) {
	p, err := Parse([]byte("---\ntitle: T\n---\nBody.\n\n---\n\nafter the rule\n"))
	if err != nil || p.Title() != "T" || p.Body != "Body.\n\n---\n\nafter the rule\n" {
		t.Errorf("%#v %v", p, err)
	}
}

func TestAListOrAScalarBlockIsADataError(t *testing.T) {
	for _, src := range []string{"---\n- a\n- b\n---\nbody\n", "---\njust some text\n---\nbody\n"} {
		_, err := Parse([]byte(src))
		if !errors.Is(err, ErrData) || !strings.Contains(err.Error(), "front matter") {
			t.Errorf("%q: %v, want ErrData naming the front matter", src, err)
		}
	}
}
