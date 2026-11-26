package toc

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rsdoiel/markdownweb/debuglog"
	"github.com/rsdoiel/markdownweb/fetch"
	"github.com/rsdoiel/markdownweb/local"
)

// The reasons a Skip gives. The first four are limits: a list that has one is
// not Complete. The others are rules and faults, reported but not limits.
const (
	SkipDepth       = "depth"          // an include nested deeper than Limits.Depth
	SkipPages       = "pages"          // the list reached Limits.Pages
	SkipFiles       = "files"          // Limits.Files outline files were already read
	SkipBytes       = "bytes"          // Limits.Bytes, or one file over the fetcher's cap
	SkipHost        = "host"           // an address on a host the caller did not name
	SkipLoop        = "loop"           // an include already being read
	SkipForbidden   = "forbidden"      // robots.txt disallows it
	SkipUnavailable = "unavailable"    // could not be fetched, or the server said no
	SkipNotOutline  = "not an outline" // read, but not an OPML document
	SkipMissing     = "missing"        // a local site: no file under the root
)

/** ErrNoOutline says a table of contents is not there: the server answered
 * with anything but 200, a local site has no .well-known/toc.opml, or what was
 * read is not an outline. Pages and PagesLocal wrap it for the starting
 * outline; reach it with errors.Is.
 *
 * Example:
 *   if errors.Is(err, toc.ErrNoOutline) { os.Exit(66) }
 */
var ErrNoOutline = errors.New("no table of contents")

const (
	defaultDepth = 1 // BonBon follows includes one level
	defaultPages = 10000
	defaultFiles = 50
	defaultBytes = 32 << 20
)

/** Limits bound what Pages and PagesLocal read. A zero field is the default; a
 * negative one is no limit, which the caller asks for on purpose. Reaching a
 * limit is not an error: the list so far is returned and says it is not
 * Complete.
 *
 * Fields:
 *   Depth (int) — how many levels of includes to follow; default 1 (BonBon's)
 *   Pages (int) — the most pages to list; default 10,000
 *   Files (int) — the most outline files to read, the first included;
 *     default 50
 *   Bytes (int64) — the most outline bytes to read in all; default 32 MiB
 *   Hosts ([]string) — hosts, as in a URL's Host (with any port), whose
 *     includes and pages may be listed besides the starting outline's own
 *     scheme and host. Pages only; a local site has no hosts to name.
 *
 * Example:
 *   lim := toc.Limits{Depth: 2, Pages: 500, Hosts: []string{"mirror.example.org"}}
 */
type Limits struct {
	Depth int
	Pages int
	Files int
	Bytes int64
	Hosts []string
}

func capOf(v, def int) int {
	switch {
	case v == 0:
		return def
	case v < 0:
		return math.MaxInt
	}
	return v
}

/** Page is a page the walk listed.
 *
 * Fields:
 *   URL (string) — the address as the outline lists it, absolute
 *   Path (string) — PagesLocal only: the file under the site root, "" when
 *     there is none
 *   Title (string) — the outline's text
 *   TOC (string) — the outline it was listed in: the include's own address
 *     (a file path for a local site), not the root's
 *   Depth (int) — 0 for the starting outline, 1 for an include of it, ...
 *   Group (bool) — it has outlines under it (a month page with its days)
 *
 * Example:
 *   for _, p := range l.Pages { if !p.Group { fmt.Println(p.Title, p.URL) } }
 */
type Page struct {
	URL   string
	Path  string
	Title string
	TOC   string
	Depth int
	Group bool
}

/** Skip is something the walk left out, with why: one of the Skip constants.
 *
 * Fields:
 *   Address (string) — the include or page, as the outline gave it
 *   Reason (string) — SkipDepth, SkipHost, SkipLoop and the others
 *
 * Example:
 *   for _, s := range l.Skipped { fmt.Fprintln(os.Stderr, s.Reason, s.Address) }
 */
type Skip struct {
	Address string
	Reason  string
}

/** Listing is what Pages and PagesLocal return.
 *
 * Fields:
 *   Pages ([]Page) — every page, in the outline's own order (includes read in
 *     place, parents before the outlines under them), once per address
 *     (toc.Key decides: a.html, a.md and a fragment are one page)
 *   Skipped ([]Skip) — what was left out and why
 *   Complete (bool) — no limit (depth, pages, files, bytes) cut the walk. A
 *     host rule, a loop or an include that failed does not clear it: read
 *     Skipped as well
 *
 * Example:
 *   if !l.Complete { log.Printf("only a prefix: %v", l.Skipped) }
 */
type Listing struct {
	Pages    []Page
	Skipped  []Skip
	Complete bool
}

/** Pages lists every page the table of contents at tocURL names, as BonBon
 * flattens an outline: each outline with a url except includes, in document
 * order, a page before the outlines under it, includes read in place. It
 * reads through the Finder's fetcher, so the pause, User-Agent and robots.txt
 * choice of the caller apply to every outline file. Includes are followed
 * under lim and only on the starting outline's scheme and host and the hosts
 * lim names; what the limits and rules leave out is in Skipped.
 *
 * Parameters:
 *   ctx (context.Context) — cancellation, and the debug logger
 *   tocURL (string) — the address of the starting outline
 *   lim (Limits) — the bounds; the zero value is the defaults
 *
 * Returns:
 *   *Listing — the pages and what was skipped
 *   error — only when the starting outline could not be read: ErrNoOutline,
 *     fetch.ErrForbidden, fetch.ErrUnavailable or fetch.ErrData (a bad
 *     address), fetch.ErrTooLarge
 *
 * Example:
 *   l, err := toc.NewFinder().Pages(ctx, "https://example.org/.well-known/toc.opml", toc.Limits{})
 */
func (f *Finder) Pages(ctx context.Context, tocURL string, lim Limits) (*Listing, error) {
	u, err := url.Parse(tocURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not an address", fetch.ErrData, tocURL)
	}
	w := f.newWalker(ctx, lim)
	w.origin = strings.ToLower(u.Scheme + "://" + u.Host)
	w.hosts = map[string]bool{}
	for _, h := range lim.Hosts {
		w.hosts[strings.ToLower(h)] = true
	}
	w.read = func(address string) readResult {
		res, err := f.fx().Get(ctx, address, accept)
		if err != nil {
			reason := SkipUnavailable
			switch {
			case errors.Is(err, fetch.ErrForbidden):
				reason = SkipForbidden
			case errors.Is(err, fetch.ErrTooLarge):
				reason = SkipBytes
			}
			return readResult{reason: reason, err: err}
		}
		if res.Status != http.StatusOK {
			return readResult{reason: SkipUnavailable, err: fmt.Errorf("%w: %s answered %d", ErrNoOutline, address, res.Status)}
		}
		if len(res.Body) > maxTOC {
			return readResult{reason: SkipBytes, err: fmt.Errorf("%w: %s is over %d bytes", fetch.ErrTooLarge, address, maxTOC)}
		}
		doc, err := Parse(res.Body, res.FinalURL)
		if err != nil {
			return readResult{reason: SkipNotOutline, err: fmt.Errorf("%w: %s: %v", ErrNoOutline, address, err)}
		}
		return readResult{doc: doc, key: keyOf(res.FinalURL), toc: res.FinalURL, size: int64(len(res.Body))}
	}
	w.keyFor = func(address string) (string, string, bool) { return keyOf(address), address, true }
	w.hostOK = func(address string) bool {
		a, err := url.Parse(address)
		if err != nil {
			return false
		}
		return strings.ToLower(a.Scheme+"://"+a.Host) == w.origin || w.hosts[strings.ToLower(a.Host)]
	}
	return w.run(tocURL, "toc.pages")
}

/** PagesLocal is Pages for a site on disk: the outline is root/.well-known/toc.opml
 * and nothing is fetched. An outline lists the published site's addresses, so
 * each page is matched to a file under the root as LookupLocal does (the
 * longest suffix of its path that is a file, .html as .md, a folder as its
 * index.md), and Page.Path says which. A page with no file is kept, with an
 * empty Path and a SkipMissing, and so is one whose file lies outside the
 * root through a link. An include is read when its address is on the
 * published origin the outline's pages use and names a file under the root;
 * any other is skipped with SkipHost. Limits.Hosts does not apply.
 *
 * Parameters:
 *   ctx (context.Context) — the debug logger
 *   root (string) — the site root, a folder
 *   lim (Limits) — the bounds; the zero value is the defaults
 *
 * Returns:
 *   *Listing — as Pages; TOC and Path are file paths
 *   error — ErrNoOutline when the root has no readable table of contents
 *
 * Example:
 *   l, err := toc.NewFinder().PagesLocal(ctx, "/home/me/site", toc.Limits{})
 */
func (f *Finder) PagesLocal(ctx context.Context, root string, lim Limits) (*Listing, error) {
	site := &local.Site{Root: root}
	w := f.newWalker(ctx, lim)
	start := filepath.Join(root, filepath.FromSlash(FileName))
	w.site = site
	w.read = func(file string) readResult {
		real, err := site.Inside(file)
		if err != nil {
			return readResult{reason: SkipMissing, err: fmt.Errorf("%w: %s: %v", ErrNoOutline, file, err)}
		}
		info, err := os.Stat(real)
		if err != nil || info.IsDir() {
			return readResult{reason: SkipMissing, err: fmt.Errorf("%w: %s", ErrNoOutline, file)}
		}
		if info.Size() > maxTOC {
			return readResult{reason: SkipBytes, err: fmt.Errorf("%w: %s is over %d bytes", fetch.ErrTooLarge, file, maxTOC)}
		}
		body, err := os.ReadFile(real)
		if err != nil {
			return readResult{reason: SkipUnavailable, err: fmt.Errorf("%w: %s: %v", ErrNoOutline, file, err)}
		}
		rel, _ := filepath.Rel(root, real)
		doc, err := Parse(body, "http://"+placeholderHost+"/"+filepath.ToSlash(rel))
		if err != nil {
			return readResult{reason: SkipNotOutline, err: fmt.Errorf("%w: %s: %v", ErrNoOutline, file, err)}
		}
		return readResult{doc: doc, key: real, toc: file, size: info.Size()}
	}
	w.keyFor = func(address string) (string, string, bool) {
		file, ok := mapToRoot(site, address)
		return file, file, ok
	}
	w.hostOK = func(address string) bool {
		a, err := url.Parse(address)
		if err != nil {
			return false
		}
		o := strings.ToLower(a.Scheme + "://" + a.Host)
		return w.origin == "" || a.Host == placeholderHost || o == w.origin
	}
	return w.run(start, "toc.pages")
}

// placeholderHost stands in for the address a local outline came from, so
// relative addresses in it resolve to something that is not a real origin.
const placeholderHost = "site.local"

// keyOf names an outline for loop and repeat detection: the address without
// its fragment, scheme and host in lower case.
func keyOf(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return address
	}
	u.Fragment = ""
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	return u.String()
}

type readResult struct {
	doc    *Doc
	key    string // names the outline for loop detection
	toc    string // what Page.TOC says
	size   int64
	reason string // a Skip reason when doc is nil
	err    error
}

type walker struct {
	f        *Finder
	ctx      context.Context
	lim      Limits
	depthCap int
	pageCap  int
	fileCap  int
	files    int // outline files read so far
	bytes    int64
	out      *Listing
	site     *local.Site // non-nil for a local walk
	origin   string      // the published origin: the start's (remote) or the pages' (local)
	hosts    map[string]bool

	read   func(address string) readResult
	keyFor func(address string) (key, display string, ok bool)
	hostOK func(address string) bool

	seenFile map[string]bool
	onPath   map[string]bool
	seenPage map[string]bool
	stopped  bool
}

func (f *Finder) newWalker(ctx context.Context, lim Limits) *walker {
	return &walker{
		f: f, ctx: ctx, lim: lim,
		depthCap: capOf(lim.Depth, defaultDepth), pageCap: capOf(lim.Pages, defaultPages), fileCap: capOf(lim.Files, defaultFiles),
		out:      &Listing{},
		seenFile: map[string]bool{}, onPath: map[string]bool{}, seenPage: map[string]bool{},
	}
}

func (w *walker) bytesCap() int64 {
	switch {
	case w.lim.Bytes == 0:
		return defaultBytes
	case w.lim.Bytes < 0:
		return math.MaxInt64
	}
	return w.lim.Bytes
}

func (w *walker) skip(address, reason string) {
	w.out.Skipped = append(w.out.Skipped, Skip{Address: address, Reason: reason})
}

func (w *walker) run(start, event string) (*Listing, error) {
	log := debuglog.FromContext(w.ctx)
	began := time.Now()
	var runErr error
	defer func() {
		log.Event(event, "start", start, "pages", len(w.out.Pages), "skipped", len(w.out.Skipped), "files", w.files, "bytes", w.bytes, "complete", w.out.Complete, "ms", time.Since(began), "err", runErr)
	}()
	w.files++
	res := w.read(start)
	if res.doc == nil {
		runErr = res.err
		return nil, res.err
	}
	w.bytes += res.size
	if w.bytes > w.bytesCap() {
		w.skip(start, SkipBytes)
	} else {
		w.seenFile[res.key], w.onPath[res.key] = true, true
		if w.site != nil {
			w.origin = publishedOrigin(res.doc)
		}
		w.visit(res.doc, res.toc, 0)
	}
	w.out.Complete = true
	for _, s := range w.out.Skipped {
		switch s.Reason {
		case SkipDepth, SkipPages, SkipFiles, SkipBytes:
			w.out.Complete = false
		}
	}
	return w.out, nil
}

// publishedOrigin is the origin a local outline's addresses use: the first
// absolute address of a page (an include if no page has one) that is not the
// placeholder.
func publishedOrigin(d *Doc) string {
	pick := func(wantInclude bool) string {
		var found string
		var walk func(ns []*node)
		walk = func(ns []*node) {
			for _, n := range ns {
				if found != "" {
					return
				}
				if n.url != "" && n.include == wantInclude {
					if u, err := url.Parse(n.url); err == nil && u.Host != "" && u.Host != placeholderHost {
						found = strings.ToLower(u.Scheme + "://" + u.Host)
						return
					}
				}
				walk(n.children)
			}
		}
		walk(d.root.children)
		return found
	}
	if o := pick(false); o != "" {
		return o
	}
	return pick(true)
}

// visit lists the pages of one outline in document order, reading each include
// where it stands.
func (w *walker) visit(doc *Doc, tocAddr string, depth int) {
	var walk func(ns []*node)
	walk = func(ns []*node) {
		for _, n := range ns {
			if w.stopped {
				return
			}
			switch {
			case n.include:
				if n.url != "" {
					w.include(n.url, depth)
				}
			case n.url != "":
				w.page(n, tocAddr, depth)
				walk(n.children)
			default:
				walk(n.children)
			}
		}
	}
	walk(doc.root.children)
}

func (w *walker) page(n *node, tocAddr string, depth int) {
	if w.site == nil && !w.hostOK(n.url) {
		w.skip(n.url, SkipHost)
		return
	}
	k := Key(n.url)
	if w.seenPage[k] {
		return
	}
	if len(w.out.Pages) >= w.pageCap {
		w.skip(n.url, SkipPages)
		w.stopped = true
		return
	}
	w.seenPage[k] = true
	p := Page{URL: n.url, Title: n.text, TOC: tocAddr, Depth: depth, Group: len(n.children) > 0}
	if w.site != nil {
		if file, ok := mapPageToRoot(w.site, n.url); ok {
			p.Path = file
		} else {
			w.skip(n.url, SkipMissing)
		}
	}
	w.out.Pages = append(w.out.Pages, p)
}

// mapPageToRoot finds the file for a page address: the Markdown form first
// (a.html is a.md, a folder is its index.md), then the address as it stands.
func mapPageToRoot(site *local.Site, address string) (string, bool) {
	if file, ok := mapToRoot(site, Key(address)); ok {
		return file, true
	}
	return mapToRoot(site, address)
}

func (w *walker) include(address string, depth int) {
	if !w.hostOK(address) {
		w.skip(address, SkipHost)
		return
	}
	key, display, ok := w.keyFor(address)
	if !ok {
		w.skip(address, SkipMissing)
		return
	}
	if w.onPath[key] {
		w.skip(address, SkipLoop)
		return
	}
	if w.seenFile[key] {
		return // reached by another route: read already, not a fault
	}
	if depth+1 > w.depthCap {
		w.skip(address, SkipDepth)
		return
	}
	if w.files >= w.fileCap {
		w.skip(address, SkipFiles)
		return
	}
	w.files++
	w.seenFile[key] = true
	res := w.read(display)
	if res.doc == nil {
		w.skip(address, res.reason)
		return
	}
	if w.bytes+res.size > w.bytesCap() {
		w.skip(address, SkipBytes)
		return
	}
	w.bytes += res.size
	w.seenFile[res.key] = true
	if w.onPath[res.key] && res.key != key {
		w.skip(address, SkipLoop)
		return
	}
	w.onPath[key], w.onPath[res.key] = true, true
	w.visit(res.doc, res.toc, depth+1)
	delete(w.onPath, key)
	delete(w.onPath, res.key)
}
