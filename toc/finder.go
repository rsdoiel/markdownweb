package toc

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/rsdoiel/markdownweb/debuglog"
	"github.com/rsdoiel/markdownweb/fetch"
	"github.com/rsdoiel/markdownweb/page"
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
 *   Before, After ([]string) — every candidate for Prev and for Next in the
 *     order BonBon tries them (Doc.Stops), for a caller that checks that one
 *     answers; Prev and Next are their first
 *   TOC (string) — the table of contents that listed the page
 *   Found (bool) — some table of contents lists the page
 *   Err (error) — reserved for a failure that is not "there is no table of contents"
 *
 * Example:
 *   r := toc.NewFinder().Lookup(ctx, "https://example.org/a.html")
 *   if r.Found { fmt.Println(r.Prev, r.Next) }
 */
type Result struct {
	Prev, Next string   // the page before and after, absolute; "" for none
	TOC        string   // the table of contents that listed the page
	Found      bool     // some table of contents lists the page
	Before     []string // candidates for Prev, nearest first (Stops)
	After      []string
	Err        error // reserved for a failure that is not "there is no table of contents"
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
	fetcher *fetch.Fetcher // nil: whatever page.Fetcher() is at the time of each request
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
	return &Finder{cache: map[string]*fetched{}, mirrors: map[string]string{}}
}

/** NewFinderWith returns a Finder that reads every outline through f, so a
 * crawler's own pause, User-Agent and robots.txt choice apply to table of
 * contents reads and not only to page reads.
 *
 * Parameters:
 *   f (*fetch.Fetcher) — the fetcher to use; nil is the same as NewFinder
 *
 * Returns:
 *   *Finder — a Finder with its own cache
 *
 * Example:
 *   f := toc.NewFinderWith(fetch.New(fetch.Options{UserAgent: "cmarkfind/0.1", Robots: true}))
 *   l, err := f.Pages(ctx, "https://example.org/.well-known/toc.opml", toc.Limits{})
 */
func NewFinderWith(f *fetch.Fetcher) *Finder {
	n := NewFinder()
	n.fetcher = f
	return n
}

// fx is the fetcher in use: the one given, or the package's current one, read
// at the time so a program that installs its fetcher after making a Finder
// still gets it.
func (f *Finder) fx() *fetch.Fetcher {
	if f.fetcher != nil {
		return f.fetcher
	}
	return page.Fetcher()
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
	out := []string{base + "/" + FileName}
	dir := path.Dir(u.Path)
	for dir != "/" && dir != "." && dir != "" && len(out) < maxProbes {
		out = append(out, base+strings.TrimRight(dir, "/")+"/"+FileName)
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
		if before, after, ok := doc.Stops(page); ok {
			res = Result{Prev: firstOf(before), Next: firstOf(after), Before: before, After: after, TOC: c, Found: true}
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
	res, e := f.fx().Get(ctx, address, accept)
	if e != nil {
		err = e
		return nil
	}
	status, size = res.Status, len(res.Body)
	if res.Status != http.StatusOK {
		return nil
	}
	if len(res.Body) > maxTOC {
		err = fmt.Errorf("larger than %d bytes", maxTOC)
		return nil
	}
	doc, err = Parse(res.Body, res.FinalURL)
	if err != nil {
		return nil
	}
	entries, includes = len(doc.Entries()), len(doc.Includes())
	return doc
}
