// Package page fetches a Markdown page the way BonBon does and splits its
// front matter from its body.
package page

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rsdoiel/frontmatter"
	"github.com/rsdoiel/markdownweb/debuglog"
	"github.com/rsdoiel/markdownweb/fetch"
	"github.com/rsdoiel/markdownweb/reader"
	"golang.org/x/net/html"
)

// Sentinels for the DR-0003 classes a fetch can fail in. Wrap with %w.
var (
	// ErrData marks content that was read and is wrong (exit 65).
	ErrData = fetch.ErrData
	// ErrNoInput marks a page that is not there (exit 66).
	ErrNoInput = errors.New("no such page")
	// ErrUnavailable marks a server that cannot be reached (exit 69).
	ErrUnavailable = fetch.ErrUnavailable
	// ErrForbidden marks an address robots.txt does not allow, when the
	// installed fetcher reads it (exit 1).
	ErrForbidden = fetch.ErrForbidden
)

var (
	fetcherMu      sync.RWMutex
	currentFetcher = fetch.New(fetch.Options{Timeout: 30 * time.Second, MaxRedirects: 10})
)

/** Fetcher returns the fetcher every request of this package goes through. The
 * default names itself markdownweb/VERSION, does not pause, does not read
 * robots.txt, and follows ten redirects, as the browser did before it.
 *
 * Returns:
 *   *fetch.Fetcher
 *
 * Example:
 *   resp, err := page.Fetcher().Open(ctx, "https://example.org/", "")
 */
func Fetcher() *fetch.Fetcher {
	fetcherMu.RLock()
	defer fetcherMu.RUnlock()
	return currentFetcher
}

/** SetFetcher installs the fetcher the package uses, so a crawler can pause per
 * host, send its own User-Agent and obey robots.txt. It returns the one it
 * replaces.
 *
 * Parameters:
 *   f (*fetch.Fetcher) — the new fetcher; nil is ignored
 *
 * Returns:
 *   *fetch.Fetcher — the previous fetcher
 *
 * Example:
 *   page.SetFetcher(fetch.New(fetch.Options{UserAgent: "cmarkfind/0.1", Robots: true}))
 */
func SetFetcher(f *fetch.Fetcher) *fetch.Fetcher {
	fetcherMu.Lock()
	defer fetcherMu.Unlock()
	old := currentFetcher
	if f != nil {
		currentFetcher = f
	}
	return old
}

// errNotMarkdown marks a good response that is not a Markdown page. It is
// always wrapped with ErrData; Fetch uses it to decide to try a sibling.
var errNotMarkdown = errors.New("not Markdown")

// notMarkdown is the error for a good response that is not a page. It reads as
// one message, is an ErrData, and is also errNotMarkdown.
type notMarkdown struct {
	msg  string
	html bool   // the answer was an HTML page
	head []byte // the start of an HTML answer, to look for a declared alternate
	base string // the address that answered, for resolving that alternate
}

func (e *notMarkdown) Error() string        { return e.msg }
func (e *notMarkdown) Is(target error) bool { return target == errNotMarkdown }
func (e *notMarkdown) Unwrap() error        { return ErrData }

// Accept is the Accept header BonBon sends with every page request.
const Accept = "text/markdown, text/x-opml"

// maxHead is how much of an HTML page is read to find a declared alternate. A
// head is small; the page itself may be many megabytes and is never loaded.
const maxHead = 128 << 10

// maxBody caps what Fetch reads, so a hostile or broken server cannot fill memory.
const maxBody = 8 << 20

/** Page is a fetched Markdown page.
 *
 * Fields:
 *   URL (string) — the final address, after redirects
 *   Meta (map[string]any) — the front matter, empty when there is none
 *   Body (string) — the Markdown with the front matter removed
 *   Reader (bool) — Body is a reader view made from an HTML page
 *   Raw (string) — the page as received, front matter included, for saving a
 *     copy to edit; empty for a reader view
 *   Notice (string) — a problem with the page that does not stop it showing,
 *     such as a front matter block that is never closed; empty when all is well
 *   Kind (Kind) — what a non-Markdown, non-HTML link was; "" for Markdown
 *
 * Example:
 *   p, err := page.Fetch(ctx, "https://example.org/a.html")
 *   if err == nil { fmt.Println(p.Meta["title"], len(p.Body)) }
 */
type Page struct {
	URL  string
	Meta map[string]any
	Body string
	Raw  string
	// Kind is set for a link that was not Markdown and not HTML: source shown
	// as text, or a note about an image or a file (FetchOther). "" for Markdown
	// and for a reader view.
	Kind   Kind
	Notice string
	Reader bool // the Body is a reader view of an HTML page, not the page's own Markdown
}

// str returns the front matter value for key as a string, or "".
func (p *Page) str(key string) string {
	if v, ok := p.Meta[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

/** Title returns the front matter title, or "" when there is none.
 *
 * Returns:
 *   string — the title
 *
 * Example:
 *   p.Title() // "Hello"
 */
func (p *Page) Title() string { return p.str("title") }

/** URLNext returns the front matter urlNext, or "" when there is none.
 *
 * Returns:
 *   string — the address of the next page, possibly relative
 *
 * Example:
 *   p.URLNext() // "/two.md"
 */
func (p *Page) URLNext() string { return p.str("urlNext") }

/** URLPrev returns the front matter urlPrev, or "" when there is none.
 *
 * Returns:
 *   string — the address of the previous page, possibly relative
 *
 * Example:
 *   p.URLPrev() // "/zero.md"
 */
func (p *Page) URLPrev() string { return p.str("urlPrev") }

/** Parse splits optional YAML front matter from the Markdown body with the
 * frontmatter module's rules: a block starts on a first line that is exactly
 * "---" and ends at the next line that is exactly "---" or "..."; the body is
 * the original bytes after it. A block that is never closed is not an error
 * here: the whole file is the body and the page carries a Notice, so the person
 * sees the file and is told.
 *
 * Parameters:
 *   src ([]byte) — the page as served
 *
 * Returns:
 *   *Page — the page, with Meta never nil
 *   error — wraps ErrData when the front matter is not valid YAML or is not a mapping
 *
 * Example:
 *   p, err := page.Parse([]byte("---\ntitle: Hi\n---\ntext\n"))
 */
func Parse(src []byte) (*Page, error) {
	p := &Page{Meta: map[string]any{}, Body: string(src), Raw: string(src)}
	meta, body, err := frontmatter.Split(src)
	switch {
	case errors.Is(err, frontmatter.ErrUnclosed):
		p.Notice = "front matter is not closed (no closing --- or ... line): showing the whole file"
		return p, nil
	case err != nil:
		return nil, fmt.Errorf("%w: %s", ErrData, err)
	}
	p.Meta, p.Body = meta, string(body)
	return p, nil
}

/** IsMarkdown reports whether a response is a page by BonBon's rule: the type
 * is text/markdown, or it is text/plain or a generic binary type and the path
 * ends in .md or .markdown.
 *
 * Parameters:
 *   contentType (string) — the Content-Type header
 *   p (string) — the URL path
 *
 * Returns:
 *   bool — true for a page
 *
 * Example:
 *   page.IsMarkdown("text/plain", "/a.md") // true
 */
func IsMarkdown(contentType, p string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mt = strings.ToLower(strings.TrimSpace(contentType))
	}
	if mt == "text/markdown" {
		return true
	}
	ext := strings.ToLower(path.Ext(p))
	if ext != ".md" && ext != ".markdown" {
		return false
	}
	return mt == "text/plain" || mt == "application/octet-stream" || mt == "binary/octet-stream" || mt == ""
}

/** Fetch gets a page with BonBon's Accept header and parses it. When the
 * server answers a link to an HTML page or a directory with something that is
 * not Markdown, it then tries the Markdown copy of the same page (a.html to
 * a.md, a directory or a bare site address to its index.md), the way BonBon's urlMirrorOf maps links.
 * The same address is always tried first, so a server that negotiates
 * Markdown on Accept (as some CDNs do for software agents) is respected.
 *
 * Parameters:
 *   ctx (context.Context) — cancellation and deadline
 *   rawurl (string) — an http or https address
 *
 * Returns:
 *   *Page — the page; its URL is the address that served it
 *   error — wraps ErrUnavailable (cannot connect), ErrNoInput (404 or 410),
 *     or ErrData (not Markdown, bad front matter, other bad status)
 *
 * Example:
 *   p, err := page.Fetch(ctx, "https://dave.writersweb.org/")
 */
func Fetch(ctx context.Context, rawurl string) (*Page, error) {
	log := debuglog.FromContext(ctx)
	done := func(p *Page, via string, err error) (*Page, error) {
		final := ""
		if p != nil {
			final = p.URL
		}
		log.Event("fetch.result", "requested", rawurl, "final", final, "via", via, "err", err)
		return p, err
	}
	p, err := fetchOne(ctx, rawurl, "same")
	if err == nil || !errors.Is(err, errNotMarkdown) {
		return done(p, "same", err)
	}
	// The page may say where its Markdown is. That beats guessing the .md
	// sibling: for an antenna collection the sibling is the author's feed list,
	// and the news is on the page the HTML declares.
	var nm *notMarkdown
	if errors.As(err, &nm) && len(nm.head) > 0 {
		alt := DeclaredAlternate(nm.base, nm.head)
		log.Event("fetch.alternate", "base", nm.base, "head_bytes", len(nm.head), "declared", alt)
		if alt != "" && alt != rawurl {
			if p, err2 := fetchOne(ctx, alt, "alternate"); err2 == nil {
				return done(p, "alternate", nil)
			}
		}
	}
	sib := MarkdownSibling(rawurl)
	log.Event("fetch.sibling", "candidate", sib)
	if sib == "" {
		return done(nil, "none", err)
	}
	if p, err2 := fetchOne(ctx, sib, "sibling"); err2 == nil {
		return done(p, "sibling", nil)
	}
	return done(nil, "none", err)
}

/** MarkdownSibling returns the address of the Markdown copy of an HTML page or
 * directory address (a.html to a.md, a directory or a bare site address to its
 * index.md), or "" when the address is neither. It works on the path of any
 * address, so local pages use the same rule as the web (cmarkbrowser DR-0001).
 *
 * Parameters:
 *   rawurl (string) — an address
 *
 * Returns:
 *   string — the Markdown copy's address, fragment dropped, or ""
 *
 * Example:
 *   page.MarkdownSibling("https://x.org/a.html") // "https://x.org/a.md"
 */
func MarkdownSibling(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return ""
	}
	switch ext := strings.ToLower(path.Ext(u.Path)); {
	case ext == ".html" || ext == ".htm":
		u.Path = strings.TrimSuffix(u.Path, path.Ext(u.Path)) + ".md"
	case u.Path == "":
		// A site address with no path (http://localhost:8000) is the root directory.
		u.Path = "/index.md"
	case strings.HasSuffix(u.Path, "/"):
		u.Path += "index.md"
	default:
		return ""
	}
	u.Fragment = ""
	return u.String()
}

// attempt is what one request learned, for the debug log.
type attempt struct {
	status      int
	contentType string
	server      string
	location    string // the Content-Location header as sent
	bytes       int
}

// fetchOne gets one address and parses it as a page, and logs the attempt:
// its role (same, alternate or sibling), what came back and how it ended.
func fetchOne(ctx context.Context, rawurl, role string) (*Page, error) {
	start := time.Now()
	var a attempt
	p, err := fetchAttempt(ctx, rawurl, &a)
	outcome := "markdown"
	switch {
	case err == nil:
	case errors.Is(err, errNotMarkdown):
		outcome = "not_markdown"
	case errors.Is(err, ErrNoInput):
		outcome = "not_found"
	case errors.Is(err, ErrUnavailable):
		outcome = "unavailable"
	case errors.Is(err, ErrData) && a.status != 0 && a.status != http.StatusOK:
		outcome = "bad_status"
	case errors.Is(err, ErrData):
		outcome = "bad_content"
	default:
		outcome = "error"
	}
	debuglog.FromContext(ctx).Event("fetch.try", "role", role, "url", rawurl, "status", a.status,
		"content_type", a.contentType, "server", a.server, "content_location", a.location, "bytes", a.bytes, "outcome", outcome, "ms", time.Since(start), "err", err)
	return p, err
}

func fetchAttempt(ctx context.Context, rawurl string, a *attempt) (*Page, error) {
	u, err := url.Parse(rawurl)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("%w: %q is not an http or https address", ErrData, rawurl)
	}
	resp, err := Fetcher().Open(ctx, rawurl, Accept)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	a.status, a.contentType, a.server, a.location = resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Server"), resp.Header.Get("Content-Location")
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return nil, fmt.Errorf("%w: %s: %s", ErrNoInput, rawurl, resp.Status)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: %s: %s", ErrData, rawurl, resp.Status)
	}
	if !IsMarkdown(resp.Header.Get("Content-Type"), resp.Request.URL.Path) {
		// Say what did answer: a server that is not a Markdown site at all
		// (an API on the same port, say) shows in its Server header.
		what := "no Content-Type"
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			what = ct
		}
		if srvName := resp.Header.Get("Server"); srvName != "" {
			what += "; server " + srvName
		}
		nm := &notMarkdown{msg: fmt.Sprintf("%s: %s is not Markdown (%s)", ErrData, rawurl, what), base: resp.Request.URL.String()}
		// Keep the start of an HTML page: its head may declare a Markdown alternate.
		if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt == "text/html" || mt == "application/xhtml+xml" {
			nm.html = true
			nm.head, _ = io.ReadAll(io.LimitReader(resp.Body, maxHead))
			a.bytes = len(nm.head)
		}
		return nil, nm
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, err)
	}
	a.bytes = len(body)
	if len(body) > maxBody {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrData, rawurl, maxBody)
	}
	p, err := Parse(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf")))
	if err != nil {
		return nil, err
	}
	p.URL = resp.Request.URL.String()
	if loc := contentLocation(resp); loc != "" {
		p.URL = loc
	}
	return p, nil
}

/** contentLocation returns the address a response says its content is at, from
 * its Content-Location header, resolved against the address that answered (after
 * redirects), or "" when there is none or it cannot be trusted. BonBon honours
 * the header: it is how a folder's page finds itself, and how an HTML address
 * answered with Markdown names the Markdown file, so relative links in it, a
 * bookmark and history all use the right address. Only the same scheme and host
 * count (a server cannot make a page seem to come from elsewhere); a fragment
 * is dropped.
 *
 * Parameters:
 *   resp (*http.Response) — the response
 *
 * Returns:
 *   string — the absolute address, or ""
 *
 * Example:
 *   // Content-Location: index.md on a response to http://x.org/dir/
 *   contentLocation(resp) // "http://x.org/dir/index.md"
 */
func contentLocation(resp *http.Response) string {
	h := strings.TrimSpace(resp.Header.Get("Content-Location"))
	if h == "" || resp.Request == nil {
		return ""
	}
	ref, err := url.Parse(h)
	if err != nil {
		return ""
	}
	base := resp.Request.URL
	abs := base.ResolveReference(ref)
	if abs.Scheme != base.Scheme || abs.Host != base.Host {
		return ""
	}
	abs.Fragment = ""
	return abs.String()
}

/** DeclaredAlternate returns the address of the Markdown page that an HTML
 * page declares in its head, as <link rel="alternate" type="text/markdown"
 * href="...">, resolved against the page's own address (antennaApp DR-0008
 * writes one on every collection page). rel may hold several words and type
 * may carry parameters; both are compared without case. A link in the body
 * does not count, nor does application/markdown or an alternate stylesheet,
 * nor an address on another host: a page is not allowed to send the reader elsewhere.
 *
 * Parameters:
 *   base (string) — the address that served the HTML
 *   head ([]byte) — the start of the HTML
 *
 * Returns:
 *   string — the absolute address, or "" when there is no usable declaration
 *
 * Example:
 *   alt := DeclaredAlternate("https://x.org/food.html", head) // "https://x.org/food_items.md"
 */
func DeclaredAlternate(base string, head []byte) string {
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	z := html.NewTokenizer(bytes.NewReader(head))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return ""
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "head" {
				return ""
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "body":
				return "" // the head is over
			case "link":
				if !hasAttr {
					continue
				}
				var rel, typ, href string
				for {
					key, val, more := z.TagAttr()
					switch string(key) {
					case "rel":
						rel = string(val)
					case "type":
						typ = string(val)
					case "href":
						href = string(val)
					}
					if !more {
						break
					}
				}
				// "alternate stylesheet" is an alternate style, not an alternate page.
				if !hasWord(rel, "alternate") || hasWord(rel, "stylesheet") || strings.TrimSpace(href) == "" {
					continue
				}
				if mt, _, err := mime.ParseMediaType(typ); err != nil || mt != "text/markdown" {
					continue
				}
				ref, err := url.Parse(strings.TrimSpace(href))
				if err != nil {
					continue
				}
				abs := b.ResolveReference(ref)
				if (abs.Scheme != "http" && abs.Scheme != "https") || abs.Host != b.Host {
					continue
				}
				abs.Fragment = ""
				return abs.String()
			}
		}
	}
}

// hasWord reports whether the space-separated list has word, ignoring case.
func hasWord(list, word string) bool {
	for _, w := range strings.Fields(list) {
		if strings.EqualFold(w, word) {
			return true
		}
	}
	return false
}

/** IsHTML reports whether err from Fetch means the address answered with an
 * HTML page and no Markdown copy of it could be found, so FetchReader can show
 * the page in reader view.
 *
 * Parameters:
 *   err (error) — what Fetch returned
 *
 * Returns:
 *   bool — true for an HTML page, false for nil and every other failure
 *
 * Example:
 *   if page.IsHTML(err) { p, err = page.FetchReader(ctx, addr) }
 */
func IsHTML(err error) bool {
	var nm *notMarkdown
	return errors.As(err, &nm) && nm.html
}

/** FetchReader gets an HTML page and converts it to Markdown: a reader view,
 * for a link that leaves the Markdown web. Scripts, styles, menus, forms and
 * unsafe addresses are dropped (package reader). The returned page's address is
 * the final one after redirects, and its Reader field is true.
 *
 * Parameters:
 *   ctx (context.Context) — cancellation and deadline
 *   rawurl (string) — an http or https address
 *
 * Returns:
 *   *Page — the converted page; Meta holds its title
 *   error — wraps ErrUnavailable, ErrNoInput or ErrData (not HTML, no text
 *     without JavaScript, too large)
 *
 * Example:
 *   p, err := page.FetchReader(ctx, "https://example.org/post")
 */
func FetchReader(ctx context.Context, rawurl string) (*Page, error) {
	start := time.Now()
	log := debuglog.FromContext(ctx)
	u, err := url.Parse(rawurl)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("%w: %q is not an http or https address", ErrData, rawurl)
	}
	resp, err := Fetcher().Open(ctx, rawurl, "text/html, application/xhtml+xml")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	event := func(outcome string, bytes int, r reader.Result, err error) {
		log.Event("fetch.reader", "url", rawurl, "final", resp.Request.URL.String(), "status", resp.StatusCode,
			"content_type", ct, "server", resp.Header.Get("Server"), "bytes", bytes, "title", r.Title,
			"out_bytes", len(r.Markdown), "truncated", r.Truncated, "outcome", outcome, "ms", time.Since(start), "err", err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		err = fmt.Errorf("%w: %s: %s", ErrNoInput, rawurl, resp.Status)
	case resp.StatusCode != http.StatusOK:
		err = fmt.Errorf("%w: %s: %s", ErrData, rawurl, resp.Status)
	}
	if err != nil {
		event("bad_status", 0, reader.Result{}, err)
		return nil, err
	}
	if mt, _, _ := mime.ParseMediaType(ct); mt != "text/html" && mt != "application/xhtml+xml" {
		err = fmt.Errorf("%w: %s is not an HTML page (%s)", ErrData, rawurl, ct)
		event("not_html", 0, reader.Result{}, err)
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		err = fmt.Errorf("%w: %s", ErrUnavailable, err)
		event("error", len(body), reader.Result{}, err)
		return nil, err
	}
	if len(body) > maxBody {
		err = fmt.Errorf("%w: %s is larger than %d bytes", ErrData, rawurl, maxBody)
		event("too_large", len(body), reader.Result{}, err)
		return nil, err
	}
	res, err := reader.Convert(body, resp.Request.URL.String())
	if err != nil {
		err = fmt.Errorf("%w: %s", ErrData, err)
		event("error", len(body), res, err)
		return nil, err
	}
	if res.Empty {
		err = fmt.Errorf("%w: %s has no text without JavaScript, which cmarkbrowser does not run", ErrData, rawurl)
		event("empty", len(body), res, err)
		return nil, err
	}
	event("reader", len(body), res, nil)
	meta := map[string]any{}
	if res.Title != "" {
		meta["title"] = res.Title
	}
	return &Page{URL: resp.Request.URL.String(), Meta: meta, Body: res.Markdown, Reader: true}, nil
}

/** ReadFile reads a local Markdown file as a page. It is only for a file the
 * person named: the program's start argument and the bookmarks file. Fetch,
 * the way every link is followed, refuses file addresses, so a page from the
 * network can never make the browser read a local file.
 *
 * Parameters:
 *   path (string) — the file, absolute or relative to the working directory
 *
 * Returns:
 *   *Page — the page; its URL is the file:// address of the file
 *   error — wraps ErrNoInput (missing or unreadable) or ErrData (a directory, a
 *     file larger than the cap, a binary file, bad front matter)
 *
 * Example:
 *   p, err := page.ReadFile("bookmarks.md")
 */
func ReadFile(path string) (*Page, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNoInput, err)
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoInput, err) // errors.Is(err, fs.ErrPermission) still works
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNoInput, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%w: %s is a directory", ErrData, abs)
	}
	body, err := io.ReadAll(io.LimitReader(f, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNoInput, err)
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrData, abs, maxBody)
	}
	if bytes.IndexByte(body[:min(len(body), 8000)], 0) >= 0 {
		return nil, fmt.Errorf("%w: %s is not a text file", ErrData, abs)
	}
	p, err := Parse(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf")))
	if err != nil {
		return nil, err
	}
	p.URL = (&url.URL{Scheme: "file", Path: abs}).String()
	return p, nil
}
