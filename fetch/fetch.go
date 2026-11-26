// Package fetch is the polite HTTP client of the Markdown web: one pause per
// host, a named User-Agent, bounded redirects, bodies and waits, Retry-After,
// and, when asked, robots.txt (markdownweb DR-0002).
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rsdoiel/markdownweb"
	"github.com/temoto/robotstxt"
)

// Sentinels for the workspace DR-0003 classes a fetch can fail in. Wrap with %w.
var (
	// ErrData marks an address or content that is wrong (exit 65).
	ErrData = errors.New("bad content")
	// ErrUnavailable marks a server that cannot be reached or asks for more
	// patience than the bound allows (exit 69).
	ErrUnavailable = errors.New("server unavailable")
	// ErrForbidden marks an address robots.txt does not allow (exit 1).
	ErrForbidden = errors.New("forbidden by robots.txt")
	// ErrTooLarge marks a body over the cap. It is also an ErrData.
	ErrTooLarge = fmt.Errorf("%w: body too large", ErrData)
)

/** Options configures a Fetcher. A zero value is usable.
 *
 * Fields:
 *   UserAgent     (string)        — sent on every request; empty means markdownweb/VERSION
 *   Pause         (time.Duration) — wait between requests to one host; 0 means none
 *   Timeout       (time.Duration) — per request; 0 means 60 s
 *   MaxBody       (int64)         — bytes read from a body; 0 means 8 MiB
 *   MaxRedirects  (int)           — redirects followed; 0 means 5
 *   MaxRetryAfter (time.Duration) — longest Retry-After waited out; 0 means 2 min
 *   Robots        (bool)          — read and obey robots.txt; off by default
 *   MaxCrawlDelay (time.Duration) — a longer Crawl-delay makes the host forbidden; 0 means 30 s
 *   Now           (func() time.Time)       — clock, for tests; nil means time.Now
 *   Sleep         (func(time.Duration))    — wait, for tests; nil means time.Sleep
 *
 * Example:
 *   f := fetch.New(fetch.Options{UserAgent: "cmarkfind/0.1", Pause: time.Second, Robots: true})
 */
type Options struct {
	UserAgent     string
	Pause         time.Duration
	Timeout       time.Duration
	MaxBody       int64
	MaxRedirects  int
	MaxRetryAfter time.Duration
	Robots        bool
	MaxCrawlDelay time.Duration
	Now           func() time.Time
	Sleep         func(time.Duration)
}

/** Result is one HTTP response read in full.
 *
 * Fields:
 *   Status (int) — the status code, whatever it is
 *   ContentType (string) — the Content-Type header
 *   Body ([]byte) — the body, at most Options.MaxBody bytes
 *   FinalURL (string) — the address after redirects
 */
type Result struct {
	Status      int
	ContentType string
	Body        []byte
	FinalURL    string
}

/** Fetcher makes the requests of one run. It is safe for concurrent use; the
 * pause is measured from the last request to a host that returned, so requests
 * to one host made at the same moment may overlap.
 */
type Fetcher struct {
	o       Options
	client  *http.Client
	rclient *http.Client

	mu     sync.Mutex // guards last and robots
	last   map[string]time.Time
	robots map[string]*hostRules
}

type hostRules struct {
	data  *robotstxt.RobotsData
	delay time.Duration
	err   error // set when the host's rules cannot be used
}

/** New returns a Fetcher.
 *
 * Parameters:
 *   o (Options) — see Options; zero fields take their defaults
 *
 * Returns:
 *   *Fetcher
 *
 * Example:
 *   f := fetch.New(fetch.Options{})
 */
func New(o Options) *Fetcher {
	if o.UserAgent == "" {
		o.UserAgent = "markdownweb/" + markdownweb.Version
	}
	if o.Timeout == 0 {
		o.Timeout = 60 * time.Second
	}
	if o.MaxBody == 0 {
		o.MaxBody = 8 << 20
	}
	if o.MaxRedirects == 0 {
		o.MaxRedirects = 5
	}
	if o.MaxRetryAfter == 0 {
		o.MaxRetryAfter = 2 * time.Minute
	}
	if o.MaxCrawlDelay == 0 {
		o.MaxCrawlDelay = 30 * time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	f := &Fetcher{o: o, last: map[string]time.Time{}, robots: map[string]*hostRules{}}
	limit := func(req *http.Request, via []*http.Request) error {
		if len(via) >= o.MaxRedirects {
			return fmt.Errorf("%w: stopped after %d redirects: %s", ErrUnavailable, o.MaxRedirects, req.URL)
		}
		return nil
	}
	f.rclient = &http.Client{Timeout: o.Timeout, CheckRedirect: limit}
	f.client = &http.Client{
		Timeout: o.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if err := limit(req, via); err != nil {
				return err
			}
			if err := f.allowed(req.URL); err != nil {
				return err
			}
			f.wait(req.URL.Host)
			return nil
		},
	}
	return f
}

// wait sleeps until the pause (or the host's Crawl-delay, if longer) has
// passed since the last request to host.
func (f *Fetcher) wait(host string) {
	f.mu.Lock()
	delay := f.o.Pause
	if r := f.robots[host]; r != nil && r.delay > delay {
		delay = r.delay
	}
	var rem time.Duration
	if last, ok := f.last[host]; ok && delay > 0 {
		rem = delay - f.o.Now().Sub(last)
	}
	f.mu.Unlock()
	if rem > 0 {
		f.o.Sleep(rem)
	}
}

// touch records that a request to host has just returned.
func (f *Fetcher) touch(host string) {
	f.mu.Lock()
	f.last[host] = f.o.Now()
	f.mu.Unlock()
}

// token is the product token of the User-Agent, which robots.txt groups name.
func (f *Fetcher) token() string {
	ua := f.o.UserAgent
	if i := strings.IndexAny(ua, "/ "); i > 0 {
		ua = ua[:i]
	}
	return ua
}

// rulesFor reads and caches host's robots.txt.
func (f *Fetcher) rulesFor(u *url.URL) *hostRules {
	f.mu.Lock()
	r, ok := f.robots[u.Host]
	f.mu.Unlock()
	if ok {
		return r
	}
	r = &hostRules{}
	defer func() {
		f.mu.Lock()
		f.robots[u.Host] = r
		f.mu.Unlock()
	}()
	href := u.Scheme + "://" + u.Host + "/robots.txt"
	req, err := http.NewRequest(http.MethodGet, href, nil)
	if err != nil {
		r.err = fmt.Errorf("%w: %s: %v", ErrData, href, err)
		return r
	}
	req.Header.Set("User-Agent", f.o.UserAgent)
	f.wait(u.Host)
	res, err := f.rclient.Do(req)
	f.touch(u.Host)
	if err != nil {
		r.err = fmt.Errorf("%w: %s: %v", ErrUnavailable, href, err)
		return r
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 512<<10))
	f.touch(u.Host)
	if err != nil {
		r.err = fmt.Errorf("%w: reading %s: %v", ErrUnavailable, href, err)
		return r
	}
	data, err := robotstxt.FromStatusAndBytes(res.StatusCode, body)
	if err != nil {
		// A robots.txt that cannot be parsed is read as no rules: the lenient reading.
		data, _ = robotstxt.FromStatusAndBytes(404, nil)
	}
	r.data = data
	if g := data.FindGroup(f.token()); g != nil {
		r.delay = g.CrawlDelay
	}
	if r.delay > f.o.MaxCrawlDelay {
		r.err = fmt.Errorf("%w: %s asks for a Crawl-delay of %s", ErrForbidden, u.Host, r.delay)
	}
	return r
}

// allowed reports whether u may be requested. With Robots off it always may.
func (f *Fetcher) allowed(u *url.URL) error {
	if !f.o.Robots {
		return nil
	}
	r := f.rulesFor(u)
	if r.err != nil {
		return r.err
	}
	if !r.data.TestAgent(u.RequestURI(), f.token()) {
		return fmt.Errorf("%w: %s", ErrForbidden, u)
	}
	return nil
}

/** Open requests href and returns the response with its body still open, for a
 * caller that reads only part of it or sets its own limit. The caller closes
 * Body. It waits out the host's pause first, applies robots.txt when asked, and
 * waits out a 429 or 503 Retry-After within MaxRetryAfter, trying once more. It
 * returns the response whatever its status, and does not apply MaxBody.
 *
 * Parameters:
 *   ctx    (context.Context) — cancels the request
 *   href   (string)          — an http or https address
 *   accept (string)          — the Accept header, empty for none
 *
 * Returns:
 *   *http.Response — the response; Request.URL is the final address
 *   error          — wraps ErrData (bad address), ErrUnavailable (cannot reach,
 *                    redirects, Retry-After too long) or ErrForbidden
 *
 * Example:
 *   resp, err := f.Open(ctx, "https://example.org/big.html", "text/html")
 *   if err == nil { defer resp.Body.Close() }
 */
func (f *Fetcher) Open(ctx context.Context, href, accept string) (*http.Response, error) {
	u, err := url.Parse(href)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%w: bad URL %q", ErrData, href)
	}
	if err := f.allowed(u); err != nil {
		return nil, err
	}
	resp, err := f.do(ctx, u, accept)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 429 && resp.StatusCode != 503 {
		return resp, nil
	}
	wait, ok := f.retryAfter(resp.Header.Get("Retry-After"))
	if !ok {
		return resp, nil
	}
	if wait > f.o.MaxRetryAfter {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %s asks to wait %s", ErrUnavailable, u.Host, wait)
	}
	resp.Body.Close()
	f.o.Sleep(wait)
	f.touch(u.Host)
	return f.do(ctx, u, accept)
}

/** Get requests href like Open and reads the body, which may be at most
 * MaxBody bytes. It returns the response whatever its status; the caller
 * decides what a 404 means.
 *
 * Parameters:
 *   ctx    (context.Context) — cancels the request
 *   href   (string)          — an http or https address
 *   accept (string)          — the Accept header, empty for none
 *
 * Returns:
 *   *Result — the response
 *   error   — as Open, and ErrTooLarge when the body is over the cap
 *
 * Example:
 *   res, err := f.Get(ctx, "https://example.org/toc.opml", "text/x-opml")
 */
func (f *Fetcher) Get(ctx context.Context, href, accept string) (*Result, error) {
	resp, err := f.Open(ctx, href, accept)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, f.o.MaxBody+1))
	f.touch(resp.Request.URL.Host)
	if err != nil {
		return nil, fmt.Errorf("%w: reading %s: %v", ErrUnavailable, href, err)
	}
	if int64(len(body)) > f.o.MaxBody {
		return nil, fmt.Errorf("%w: %s is over %d bytes", ErrTooLarge, href, f.o.MaxBody)
	}
	return &Result{
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Body:        body,
		FinalURL:    resp.Request.URL.String(),
	}, nil
}

// do makes one request, after the pause.
func (f *Fetcher) do(ctx context.Context, u *url.URL, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: bad URL %q: %v", ErrData, u, err)
	}
	req.Header.Set("User-Agent", f.o.UserAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	f.wait(u.Host)
	resp, err := f.client.Do(req)
	f.touch(u.Host)
	if err != nil {
		for _, s := range []error{ErrForbidden, ErrUnavailable, ErrData} {
			if errors.Is(err, s) {
				return nil, err
			}
		}
		return nil, fmt.Errorf("%w: %s: %v", ErrUnavailable, u, err)
	}
	return resp, nil
}

// retryAfter reads a Retry-After value: whole seconds or an HTTP date.
func (f *Fetcher) retryAfter(v string) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return time.Duration(n) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(f.o.Now()); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}
