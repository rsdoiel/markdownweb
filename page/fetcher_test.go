package page

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rsdoiel/markdownweb/fetch"
)

// fetcherSite serves Markdown and robots.txt, and records each request's
// User-Agent and path.
func fetcherSite(robots string) (*httptest.Server, func() (uas, paths []string)) {
	var mu sync.Mutex
	var uas, paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		uas, paths = append(uas, r.Header.Get("User-Agent")), append(paths, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/robots.txt" {
			w.Write([]byte(robots))
			return
		}
		w.Header().Set("Content-Type", "text/markdown")
		w.Write([]byte("# hi\n"))
	}))
	return srv, func() ([]string, []string) { mu.Lock(); defer mu.Unlock(); return uas, paths }
}

func useFetcher(t *testing.T, o fetch.Options) {
	t.Helper()
	old := SetFetcher(fetch.New(o))
	t.Cleanup(func() { SetFetcher(old) })
}

func TestFetchNamesItsUserAgent(t *testing.T) {
	srv, seen := fetcherSite("")
	defer srv.Close()
	if _, err := Fetch(context.Background(), srv.URL+"/a.md"); err != nil {
		t.Fatal(err)
	}
	uas, _ := seen()
	if len(uas) == 0 || !strings.HasPrefix(uas[0], "markdownweb/") {
		t.Fatalf("User-Agent %v", uas)
	}
}

func TestSetFetcherIsUsedByFetchFetchOtherAndFetchReader(t *testing.T) {
	srv, seen := fetcherSite("")
	defer srv.Close()
	useFetcher(t, fetch.Options{UserAgent: "cmarkfind/9"})
	ctx := context.Background()
	Fetch(ctx, srv.URL+"/a.md")
	FetchOther(ctx, srv.URL+"/b.md")
	FetchReader(ctx, srv.URL+"/c.md")
	uas, _ := seen()
	if len(uas) != 3 {
		t.Fatalf("requests %v", uas)
	}
	for _, ua := range uas {
		if ua != "cmarkfind/9" {
			t.Fatalf("User-Agent %q", ua)
		}
	}
}

func TestFetchPausesBetweenRequestsToOneHost(t *testing.T) {
	srv, _ := fetcherSite("")
	defer srv.Close()
	useFetcher(t, fetch.Options{Pause: 150 * time.Millisecond})
	start := time.Now()
	Fetch(context.Background(), srv.URL+"/a.md")
	Fetch(context.Background(), srv.URL+"/b.md")
	if d := time.Since(start); d < 150*time.Millisecond {
		t.Fatalf("two fetches took %v", d)
	}
}

func TestFetchRobotsForbiddenIsErrForbidden(t *testing.T) {
	srv, seen := fetcherSite("User-agent: *\nDisallow: /private/\n")
	defer srv.Close()
	useFetcher(t, fetch.Options{Robots: true})
	ctx := context.Background()
	for name, fn := range map[string]func(context.Context, string) (*Page, error){"Fetch": Fetch, "FetchOther": FetchOther, "FetchReader": FetchReader} {
		if _, err := fn(ctx, srv.URL+"/private/x.md"); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s: err %v", name, err)
		}
	}
	if _, paths := seen(); len(paths) != 1 || paths[0] != "/robots.txt" {
		t.Fatalf("requests %v", paths)
	}
}

func TestPageSentinelsAreFetchSentinels(t *testing.T) {
	if !errors.Is(ErrData, fetch.ErrData) || !errors.Is(ErrUnavailable, fetch.ErrUnavailable) {
		t.Fatal("page and fetch sentinels differ")
	}
}

func TestFetchUnreachableIsStillErrUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	if _, err := Fetch(context.Background(), u+"/a.md"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err %v", err)
	}
}
