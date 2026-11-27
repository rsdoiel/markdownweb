package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// clock is a fake time source: Sleep advances Now and records the wait.
type clock struct {
	mu     sync.Mutex
	t      time.Time
	sleeps []time.Duration
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
	c.t = c.t.Add(d)
}
func (c *clock) total() (n time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, d := range c.sleeps {
		n += d
	}
	return n
}

func newTest(o Options) (*Fetcher, *clock) {
	c := &clock{t: time.Unix(1000, 0)}
	o.Now, o.Sleep = c.Now, c.Sleep
	return New(o), c
}

func TestGetReturnsResponseWhateverItsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		w.WriteHeader(404)
		w.Write([]byte("gone"))
	}))
	defer srv.Close()
	f, _ := newTest(Options{})
	res, err := f.Get(context.Background(), srv.URL+"/a.md", "text/markdown")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 404 || string(res.Body) != "gone" || res.ContentType != "text/markdown" {
		t.Fatalf("got %+v", res)
	}
}

func TestSendsNamedUserAgentAndAccept(t *testing.T) {
	var ua, accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua, accept = r.Header.Get("User-Agent"), r.Header.Get("Accept")
	}))
	defer srv.Close()
	f, _ := newTest(Options{UserAgent: "cmarkfind/0.1 (+https://example.org)"})
	if _, err := f.Get(context.Background(), srv.URL, "text/x-opml"); err != nil {
		t.Fatal(err)
	}
	if ua != "cmarkfind/0.1 (+https://example.org)" || accept != "text/x-opml" {
		t.Fatalf("ua %q accept %q", ua, accept)
	}
}

func TestDefaultUserAgentIsNamed(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ua = r.Header.Get("User-Agent") }))
	defer srv.Close()
	f, _ := newTest(Options{})
	f.Get(context.Background(), srv.URL, "")
	if !strings.HasPrefix(ua, "markdownweb/") {
		t.Fatalf("default User-Agent %q", ua)
	}
}

func TestPausesBetweenRequestsToOneHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	f, c := newTest(Options{Pause: 500 * time.Millisecond})
	f.Get(context.Background(), srv.URL+"/1", "")
	if c.total() != 0 {
		t.Fatalf("first request waited %v", c.total())
	}
	f.Get(context.Background(), srv.URL+"/2", "")
	if c.total() != 500*time.Millisecond {
		t.Fatalf("second request waited %v, want 500ms", c.total())
	}
}

func TestNoPauseBetweenDifferentHosts(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	a, b := httptest.NewServer(h), httptest.NewServer(h)
	defer a.Close()
	defer b.Close()
	f, c := newTest(Options{Pause: time.Second})
	f.Get(context.Background(), a.URL, "")
	f.Get(context.Background(), b.URL, "")
	if c.total() != 0 {
		t.Fatalf("waited %v between hosts", c.total())
	}
}

func TestRetryAfterSecondsRetriesOnce(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(429)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	f, c := newTest(Options{})
	res, err := f.Get(context.Background(), srv.URL, "")
	if err != nil || res.Status != 200 || n != 2 {
		t.Fatalf("res %+v err %v n %d", res, err, n)
	}
	if c.total() != 7*time.Second {
		t.Fatalf("waited %v, want 7s", c.total())
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	n := 0
	var c *clock
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.Header().Set("Retry-After", c.Now().Add(12*time.Second).UTC().Format(http.TimeFormat))
			w.WriteHeader(503)
			return
		}
	}))
	defer srv.Close()
	var f *Fetcher
	f, c = newTest(Options{})
	res, err := f.Get(context.Background(), srv.URL, "")
	if err != nil || res.Status != 200 {
		t.Fatalf("res %+v err %v", res, err)
	}
	if c.total() != 12*time.Second {
		t.Fatalf("waited %v, want 12s", c.total())
	}
}

func TestRetryAfterBeyondBoundIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(503)
	}))
	defer srv.Close()
	f, c := newTest(Options{MaxRetryAfter: time.Minute})
	_, err := f.Get(context.Background(), srv.URL, "")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err %v", err)
	}
	if c.total() != 0 {
		t.Fatalf("slept %v", c.total())
	}
}

func TestStatusWithoutRetryAfterIsReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	f, _ := newTest(Options{})
	res, err := f.Get(context.Background(), srv.URL, "")
	if err != nil || res.Status != 503 {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestSecondRetryAfterIsReturnedNotFollowed(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	f, _ := newTest(Options{})
	res, err := f.Get(context.Background(), srv.URL, "")
	if err != nil || res.Status != 429 || n != 2 {
		t.Fatalf("res %+v err %v n %d", res, err, n)
	}
}

func TestBodyOverCapIsErrTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()
	f, _ := newTest(Options{MaxBody: 50})
	_, err := f.Get(context.Background(), srv.URL, "")
	if !errors.Is(err, ErrTooLarge) || !errors.Is(err, ErrData) {
		t.Fatalf("err %v", err)
	}
}

func TestBodyAtCapIsRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 50)))
	}))
	defer srv.Close()
	f, _ := newTest(Options{MaxBody: 50})
	res, err := f.Get(context.Background(), srv.URL, "")
	if err != nil || len(res.Body) != 50 {
		t.Fatalf("err %v", err)
	}
}

func TestRedirectLimit(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/again", 302)
	}))
	defer srv.Close()
	f, _ := newTest(Options{MaxRedirects: 3})
	_, err := f.Get(context.Background(), srv.URL, "")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err %v", err)
	}
}

func TestRedirectReportsFinalURL(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, srv.URL+"/new", 301)
		}
	}))
	defer srv.Close()
	f, _ := newTest(Options{})
	res, err := f.Get(context.Background(), srv.URL+"/old", "")
	if err != nil || res.FinalURL != srv.URL+"/new" {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestBadURLIsErrData(t *testing.T) {
	f, _ := newTest(Options{})
	for _, u := range []string{"", "ftp://example.org/x", "http://", "::bad"} {
		if _, err := f.Get(context.Background(), u, ""); !errors.Is(err, ErrData) {
			t.Errorf("%q: err %v", u, err)
		}
	}
}

func TestUnreachableIsErrUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	u := srv.URL
	srv.Close()
	f, _ := newTest(Options{})
	if _, err := f.Get(context.Background(), u, ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err %v", err)
	}
}

// robotsSite serves robots.txt with status and body; other paths answer 200.
// hits records every path requested.
func robotsSite(status int, robots string) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	hits := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*hits = append(*hits, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(status)
			w.Write([]byte(robots))
			return
		}
		w.Write([]byte("page"))
	}))
	return srv, hits
}

func TestRobotsOffByDefaultNeverRequestsIt(t *testing.T) {
	srv, hits := robotsSite(200, "User-agent: *\nDisallow: /\n")
	defer srv.Close()
	f, _ := newTest(Options{})
	res, err := f.Get(context.Background(), srv.URL+"/a", "")
	if err != nil || res.Status != 200 {
		t.Fatalf("res %+v err %v", res, err)
	}
	for _, h := range *hits {
		if h == "/robots.txt" {
			t.Fatal("robots.txt requested with Robots off")
		}
	}
}

func TestRobotsDisallowIsErrForbiddenAndNoRequest(t *testing.T) {
	srv, hits := robotsSite(200, "User-agent: *\nDisallow: /private/\n")
	defer srv.Close()
	f, _ := newTest(Options{Robots: true})
	_, err := f.Get(context.Background(), srv.URL+"/private/x", "")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err %v", err)
	}
	for _, h := range *hits {
		if h == "/private/x" {
			t.Fatal("a forbidden address was requested")
		}
	}
	if _, err := f.Get(context.Background(), srv.URL+"/public", ""); err != nil {
		t.Fatalf("allowed address: %v", err)
	}
}

func TestRobotsReadOncePerHost(t *testing.T) {
	srv, hits := robotsSite(200, "User-agent: *\nDisallow: /x\n")
	defer srv.Close()
	f, _ := newTest(Options{Robots: true})
	f.Get(context.Background(), srv.URL+"/a", "")
	f.Get(context.Background(), srv.URL+"/b", "")
	n := 0
	for _, h := range *hits {
		if h == "/robots.txt" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("robots.txt requested %d times", n)
	}
}

func TestRobotsMatchesTheNamedAgent(t *testing.T) {
	srv, _ := robotsSite(200, "User-agent: cmarkfind\nDisallow: /\n\nUser-agent: *\nAllow: /\n")
	defer srv.Close()
	f, _ := newTest(Options{Robots: true, UserAgent: "cmarkfind/0.1 (+https://example.org)"})
	if _, err := f.Get(context.Background(), srv.URL+"/a", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err %v", err)
	}
}

func TestRobots404MeansAllowed(t *testing.T) {
	srv, _ := robotsSite(404, "")
	defer srv.Close()
	f, _ := newTest(Options{Robots: true})
	if _, err := f.Get(context.Background(), srv.URL+"/a", ""); err != nil {
		t.Fatal(err)
	}
}

func TestRobots5xxMeansNothingIsAllowed(t *testing.T) {
	srv, _ := robotsSite(503, "")
	defer srv.Close()
	f, _ := newTest(Options{Robots: true})
	if _, err := f.Get(context.Background(), srv.URL+"/a", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err %v", err)
	}
}

func TestRobotsCrawlDelayRaisesThePause(t *testing.T) {
	srv, _ := robotsSite(200, "User-agent: *\nCrawl-delay: 5\n")
	defer srv.Close()
	f, c := newTest(Options{Robots: true, Pause: time.Second})
	f.Get(context.Background(), srv.URL+"/a", "")
	before := c.total()
	f.Get(context.Background(), srv.URL+"/b", "")
	if got := c.total() - before; got != 5*time.Second {
		t.Fatalf("waited %v, want 5s", got)
	}
}

func TestRobotsCrawlDelayBeyondBoundIsErrForbidden(t *testing.T) {
	srv, _ := robotsSite(200, "User-agent: *\nCrawl-delay: 3600\n")
	defer srv.Close()
	f, _ := newTest(Options{Robots: true, MaxCrawlDelay: time.Minute})
	if _, err := f.Get(context.Background(), srv.URL+"/a", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err %v", err)
	}
}

func TestRobotsAppliesToRedirectTargetOnAnotherHost(t *testing.T) {
	other, _ := robotsSite(200, "User-agent: *\nDisallow: /\n")
	defer other.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			return
		}
		http.Redirect(w, r, other.URL+"/p", 302)
	}))
	defer first.Close()
	f, _ := newTest(Options{Robots: true})
	if _, err := f.Get(context.Background(), first.URL+"/a", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err %v", err)
	}
}

func TestOpenReturnsTheResponseWithItsBodyOpen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Location", "x.md")
		w.WriteHeader(404)
		w.Write([]byte("body"))
	}))
	defer srv.Close()
	f, _ := newTest(Options{})
	resp, err := f.Open(context.Background(), srv.URL+"/a", "*/*")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b := make([]byte, 10)
	n, _ := resp.Body.Read(b)
	if resp.StatusCode != 404 || resp.Header.Get("Content-Location") != "x.md" || string(b[:n]) != "body" || resp.Request.URL.Path != "/a" {
		t.Fatalf("status %d header %v body %q", resp.StatusCode, resp.Header, b[:n])
	}
}

func TestOpenDoesNotCapTheBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()
	f, _ := newTest(Options{MaxBody: 10})
	resp, err := f.Open(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestOpenPausesRetriesAndObeysRobots(t *testing.T) {
	srv, _ := robotsSite(200, "User-agent: *\nDisallow: /no\n")
	defer srv.Close()
	f, c := newTest(Options{Robots: true, Pause: time.Second})
	r1, err := f.Open(context.Background(), srv.URL+"/a", "")
	if err != nil {
		t.Fatal(err)
	}
	r1.Body.Close()
	if _, err := f.Open(context.Background(), srv.URL+"/no", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err %v", err)
	}
	if c.total() < time.Second {
		t.Fatalf("no pause: %v", c.total())
	}
}

func TestConcurrentGetsAreSafe(t *testing.T) {
	srv, _ := robotsSite(200, "")
	defer srv.Close()
	f := New(Options{Robots: true})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.Get(context.Background(), srv.URL+"/a", "")
		}()
	}
	wg.Wait()
}

// cmarkfind DR-0001: a refill asks "has it changed?" with what the last read
// returned, so a 304 costs no body.
func TestGetReturnsETagAndLastModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Wed, 25 Nov 2026 10:00:00 GMT")
		w.Write([]byte("hello"))
	}))
	defer srv.Close()
	f, _ := newTest(Options{})
	res, err := f.Get(context.Background(), srv.URL+"/a.md", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.ETag != `"v1"` || res.LastModified != "Wed, 25 Nov 2026 10:00:00 GMT" {
		t.Errorf("got %+v", res)
	}
}

func TestGetIfSendsValidatorsAndTakesA304(t *testing.T) {
	var gotETag, gotSince string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotETag, gotSince = r.Header.Get("If-None-Match"), r.Header.Get("If-Modified-Since")
		if gotETag == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Write([]byte("new"))
	}))
	defer srv.Close()
	f, _ := newTest(Options{})
	res, err := f.GetIf(context.Background(), srv.URL+"/a.md", "", Validators{ETag: `"v1"`, LastModified: "Wed, 25 Nov 2026 10:00:00 GMT"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 304 || len(res.Body) != 0 || res.NotModified != true {
		t.Errorf("got %+v", res)
	}
	if gotETag != `"v1"` || gotSince != "Wed, 25 Nov 2026 10:00:00 GMT" {
		t.Errorf("headers sent: %q %q", gotETag, gotSince)
	}
	res, _ = f.GetIf(context.Background(), srv.URL+"/a.md", "", Validators{ETag: `"old"`})
	if res.Status != 200 || string(res.Body) != "new" || res.NotModified {
		t.Errorf("a changed page: %+v", res)
	}
}

func TestGetIfWithNoValidatorsSendsNoConditionalHeaders(t *testing.T) {
	var h http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h = r.Header.Clone() }))
	defer srv.Close()
	f, _ := newTest(Options{})
	if _, err := f.GetIf(context.Background(), srv.URL+"/", "", Validators{}); err != nil {
		t.Fatal(err)
	}
	if h.Get("If-None-Match") != "" || h.Get("If-Modified-Since") != "" {
		t.Errorf("sent %v", h)
	}
}

// A conditional request still waits its turn and obeys robots.txt.
func TestGetIfIsPoliteLikeGet(t *testing.T) {
	srv, _ := robotsSite(200, "User-agent: *\nDisallow: /private/\n")
	defer srv.Close()
	f, _ := newTest(Options{Robots: true})
	_, err := f.GetIf(context.Background(), srv.URL+"/private/x.md", "", Validators{ETag: `"v"`})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v, want ErrForbidden", err)
	}
}
