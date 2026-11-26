package toc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/rsdoiel/markdownweb/debuglog"
)

const (
	// maxTOC caps one table of contents. Dave Winer's, with 3,500 entries, is 456 KB.
	maxTOC = 8 << 20
	// maxProbes is how many folders above a page are tried for a table of contents.
	maxProbes = 6
	// maxIncludeDepth and maxFetches bound the includes followed for one lookup.
	maxIncludeDepth = 3
	maxFetches      = 12
)

// accept is what is asked for, as BonBon asks (text/x-opml) plus plain XML.
const accept = "text/x-opml, application/xml;q=0.9, text/xml;q=0.9, */*;q=0.1"

/** Result is the answer to Lookup.
 *
 * Fields:
 *   Prev, Next (string) — the page before and after, absolute; "" for none
 *   TOC (string) — the table of contents that listed the page
 *   Found (bool) — some table of contents lists the page
 *   Err (error) — reserved for a failure that is not "there is no table of contents"
 *
 * Example:
 *   r := toc.NewFinder().Lookup(ctx, "https://example.org/a.html")
 *   if r.Found { fmt.Println(r.Prev, r.Next) }
 */
type Result struct {
	Prev, Next string // the page before and after, absolute; "" for none
	TOC        string // the table of contents that listed the page
	Found      bool   // some table of contents lists the page
	Err        error  // reserved for a failure that is not "there is no table of contents"
}

type fetched struct {
	doc  *Doc
	incs []string // the include outlines the file named, kept because Splice removes them from doc
}

/** Finder finds the table of contents that covers a page and remembers what it
 * has fetched, a miss as well as a hit, so the cost of reading a site's table
 * of contents is paid once per site in a session. Make one with NewFinder.
 *
 * Example:
 *   f := toc.NewFinder()
 *   r := f.Lookup(ctx, "https://example.org/blog/a.html")
 */
type Finder struct {
	client  *http.Client
	mu      sync.Mutex
	cache   map[string]*fetched
	mirrors map[string]string // bonbon.json address to its urlMirrorOf, "" for none
}

/** NewFinder returns a Finder with its own cache.
 *
 * Example:
 *   f := toc.NewFinder()
 *   r := f.Lookup(ctx, "https://example.org/blog/2026/11/19/a.html")
 */
func NewFinder() *Finder {
	return &Finder{client: &http.Client{Timeout: 15 * time.Second}, cache: map[string]*fetched{}, mirrors: map[string]string{}}
}

// candidates lists the addresses a table of contents for the page may be at:
// the site root first, as BonBon asks it and nothing nearer, then
// <folder>/.well-known/toc.opml for each folder from the page's up to, but not
// including, the root, nearest first, at most maxProbes addresses in all. A
// root that lists the page ends the lookup, so a page in a folder not seen
// before costs no request once the root's answer is remembered.
func candidates(page string) []string {
	u, err := url.Parse(page)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil
	}
	base := u.Scheme + "://" + u.Host
	out := []string{base + "/.well-known/toc.opml"}
	dir := path.Dir(u.Path)
	for dir != "/" && dir != "." && dir != "" && len(out) < maxProbes {
		out = append(out, base+strings.TrimRight(dir, "/")+"/.well-known/toc.opml")
		dir = path.Dir(dir)
	}
	return out
}

/** Lookup finds the table of contents that lists page and returns the pages
 * before and after it (see the package comment for the rule). It tries the
 * site root first, then the folders above the page, nearest first, and takes
 * the first table of contents that lists the page, and merges the outlines it includes from the same host.
 * A site with no table of contents, a file that is not an outline, a page that
 * is not listed: all give Found false, not an error.
 *
 * Parameters:
 *   ctx (context.Context) — cancellation, and the debug logger
 *   page (string) — the address of the page being shown
 *
 * Returns:
 *   Result — the neighbours and where they came from
 *
 * Example:
 *   r := finder.Lookup(ctx, view.Page.URL)
 *   if r.Found && r.Next != "" { open(r.Next) }
 */
func (f *Finder) Lookup(ctx context.Context, page string) Result {
	log := debuglog.FromContext(ctx)
	cands := candidates(page)
	var res Result
	probes := 0
	for _, c := range cands {
		probes++
		budget := maxFetches
		doc := f.get(ctx, c, map[string]bool{}, 0, &budget)
		if doc == nil {
			continue
		}
		if prev, next, ok := doc.Neighbours(page); ok {
			res = Result{Prev: prev, Next: next, TOC: c, Found: true}
			break
		}
	}
	log.Event("toc.lookup", "page", page, "toc", res.TOC, "found", res.Found, "prev", res.Prev, "next", res.Next, "probes", probes)
	return res
}

// get returns the outline at address with its same-host includes merged, or
// nil when there is none. Everything fetched is cached, including failures.
func (f *Finder) get(ctx context.Context, address string, inProgress map[string]bool, depth int, budget *int) *Doc {
	f.mu.Lock()
	if c, ok := f.cache[address]; ok {
		f.mu.Unlock()
		return c.doc
	}
	f.mu.Unlock()
	if inProgress[address] || *budget <= 0 {
		return nil // an include loop, or too many fetches for one lookup
	}
	*budget--
	inProgress[address] = true
	defer delete(inProgress, address)

	doc := f.fetch(ctx, address)
	var incs []string
	if doc != nil {
		incs = doc.Includes()
	}
	if doc != nil && depth < maxIncludeDepth {
		base, _ := url.Parse(address)
		for _, inc := range doc.Includes() {
			u, err := url.Parse(inc)
			if err != nil || base == nil || u.Host != base.Host || u.Scheme != base.Scheme {
				continue // an include on another host is not followed
			}
			if child := f.get(ctx, inc, inProgress, depth+1, budget); child != nil {
				doc.Splice(inc, child)
			}
		}
	}
	f.mu.Lock()
	f.cache[address] = &fetched{doc: doc, incs: incs}
	f.mu.Unlock()
	return doc
}

// fetch reads and parses one table of contents; nil for any failure.
func (f *Finder) fetch(ctx context.Context, address string) *Doc {
	log := debuglog.FromContext(ctx)
	start := time.Now()
	status, size, entries, includes := 0, 0, 0, 0
	var doc *Doc
	var err error
	defer func() {
		log.Event("toc.fetch", "url", address, "status", status, "bytes", size, "entries", entries, "includes", includes, "ms", time.Since(start), "err", err)
	}()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if e != nil {
		err = e
		return nil
	}
	req.Header.Set("Accept", accept)
	resp, e := f.client.Do(req)
	if e != nil {
		err = e
		return nil
	}
	defer resp.Body.Close()
	status = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, maxTOC+1))
	size = len(body)
	if e != nil {
		err = e
		return nil
	}
	if len(body) > maxTOC {
		err = fmt.Errorf("larger than %d bytes", maxTOC)
		return nil
	}
	doc, err = Parse(body, resp.Request.URL.String())
	if err != nil {
		return nil
	}
	entries, includes = len(doc.Entries()), len(doc.Includes())
	return doc
}
