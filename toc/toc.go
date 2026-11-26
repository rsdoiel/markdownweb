// Package toc reads a site's table of contents, the OPML outline BonBon looks
// for at /.well-known/toc.opml, and answers one question about it: what page
// comes before and what page after a given page. antennaApp writes the file
// (its DR-0013); Dave Winer's sites publish their own, so the parser reads
// what both write, including the ISO-8859-1 that Dave's declares.
//
// The rule for "before" and "after" is BonBon's (getTocNeighbors, BonBon
// 0.6.57; markdownweb DR-0004), so a reader built on this package and BonBon
// give the same arrows on a page that names none of its own:
//
//   - The outline is one flat list: every outline with a url except includes,
//     in document order, a page before the outlines under it. A month page
//     with its days under it is a stop, and its first day follows it. An
//     include is read in place (Splice).
//   - Previous is the line above, next the line below. The ends wrap: the
//     first page's previous is the last. Nothing is reordered, whatever the
//     calendar outlines say, and groups do not stop the arrows.
//   - A page listed more than once is one page; its neighbours are the nearest
//     different pages above its first line and below its last.
//   - An included site is a list of its own: a page whose address is under the
//     folder of a top-level include (its url up to "/.well-known/") is read in
//     that include's pages alone and wraps inside them; a page outside every
//     such folder is read in the whole list, the included pages in place.
//     BonBon's getSiteToc.
//   - A page whose front matter names urlPrev and urlNext keeps those; that is
//     the caller's, as BonBon's. Whether a neighbour exists is asked by BonBon
//     with a request per candidate; Stops gives the candidates in that order.
//
// The older leaves-only rule with groups and calendar reversal, which
// antennaApp DR-0013 wrote antenna's own arrows by, is not used here; Entries
// keeps its leaves-only list for callers that want it.
package toc

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/rsdoiel/opml"
)

/** Entry is a page the outline lists.
 *
 * Fields:
 *   Text (string) — the outline's text
 *   URL (string) — its address, absolute
 *   Group (bool) — set by AllPages for a page that has outlines under it (a
 *     month page with its days); always false in what Entries returns
 *
 * Example:
 *   for _, e := range doc.Entries() { fmt.Println(e.Text, e.URL) }
 */
type Entry struct {
	Text  string
	URL   string // absolute
	Group bool   // AllPages only: the outline has outlines under it
}

type node struct {
	text, typ, name, url string // url is absolute, "" when none
	include              bool   // type="include": url is another outline, not a page
	children             []*node
	folder               string // a spliced top-level include: the folder its site is published in (see siteFolder)
}

/** Doc is a parsed table of contents. Make one with Parse.
 *
 * Example:
 *   doc, err := toc.Parse(body, "https://example.org/.well-known/toc.opml")
 *   prev, next, ok := doc.Neighbours("https://example.org/a.html")
 */
type Doc struct {
	root *node
}

/** Parse reads a table of contents. Addresses in it are resolved against base,
 * the address the file was fetched from. Encodings other than UTF-8 are read:
 * ISO-8859-1 (also latin1 and US-ASCII) and windows-1252.
 *
 * Parameters:
 *   src ([]byte) — the OPML document
 *   base (string) — the address it came from
 *
 * Returns:
 *   *Doc — the outline; an outline with nothing in it is a Doc with no entries
 *   error — the document is not well-formed XML, is not an opml document, or
 *     declares an encoding that is not supported
 *
 * Example:
 *   d, err := toc.Parse(body, "https://example.org/.well-known/toc.opml")
 */
func Parse(src []byte, base string) (*Doc, error) {
	b, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("toc: %q is not an address: %w", base, err)
	}
	o, err := opml.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("toc: %w", err)
	}
	var build func(xs []*opml.Outline) []*node
	build = func(xs []*opml.Outline) []*node {
		var out []*node
		for _, x := range xs {
			n := &node{text: x.Text, typ: x.Type, name: otherAttr(x, "name"), include: strings.EqualFold(x.Type, "include")}
			if a := strings.TrimSpace(x.URL); a != "" {
				if ref, err := url.Parse(a); err == nil {
					n.url = b.ResolveReference(ref).String()
				}
			}
			n.children = build(x.Outline)
			out = append(out, n)
		}
		return out
	}
	var top []*opml.Outline
	if o.Body != nil {
		top = o.Body.Outline
	}
	return &Doc{root: &node{children: build(top)}}, nil
}

/** Entries returns the pages the outline lists that have nothing under them,
 * in document order. This is the older, leaves-only question and is not
 * BonBon's list: a page with outlines under it (a month page) is left out. Use
 * AllPages for BonBon's flatten; Neighbours no longer depends on Entries.
 *
 * Returns:
 *   []Entry — the pages, none for an empty outline
 *
 * Example:
 *   for _, e := range doc.Entries() { fmt.Println(e.URL) }
 */
func (d *Doc) Entries() []Entry {
	var out []Entry
	var walk func(ns []*node)
	walk = func(ns []*node) {
		for _, n := range ns {
			if n.url != "" && len(n.children) == 0 && !n.include {
				out = append(out, Entry{Text: n.text, URL: n.url})
			}
			walk(n.children)
		}
	}
	walk(d.root.children)
	return out
}

/** AllPages returns every outline that names a page, in document order, by
 * BonBon's rule for flattening an outline (and opml's Flatten): every outline
 * with a url except includes, a page before the outlines under it, no
 * reordering. Unlike Entries it keeps a page that has outlines under it, and
 * marks it Group. Entries is left as it is; Neighbours depends on it.
 *
 * Returns:
 *   []Entry — the pages, none for an empty outline
 *
 * Example:
 *   for _, e := range doc.AllPages() { if !e.Group { fmt.Println(e.URL) } }
 */
func (d *Doc) AllPages() []Entry {
	return pagesOf(d.root.children)
}

// pagesOf flattens outlines as AllPages does.
func pagesOf(ns []*node) []Entry {
	var out []Entry
	var walk func(ns []*node)
	walk = func(ns []*node) {
		for _, n := range ns {
			if n.url != "" && !n.include {
				out = append(out, Entry{Text: n.text, URL: n.url, Group: len(n.children) > 0})
			}
			walk(n.children)
		}
	}
	walk(ns)
	return out
}

// siteFolder is the folder an included outline describes: the include's
// address up to and including "/.well-known/"'s slash, "" when it has none
// (BonBon's getSiteToc: such an include does not make a site of its own).
func siteFolder(includeURL string) string {
	i := strings.LastIndex(includeURL, "/.well-known/")
	if i < 0 {
		return ""
	}
	return includeURL[:i+1]
}

// linesFor is the list a page at address is read in. BonBon (getSiteToc): a
// page whose address is under the folder of a top-level include that Splice
// filled in is in that site's list alone, so the arrows wrap inside it; the
// last such include wins. Any other page is in the whole outline's list, the
// included sites in place.
func (d *Doc) linesFor(address string) []Entry {
	var site *node
	for _, c := range d.root.children {
		if c.folder != "" && strings.HasPrefix(address, c.folder) {
			site = c
		}
	}
	if site != nil {
		return pagesOf(site.children)
	}
	return d.AllPages()
}

/** Includes returns the addresses of the include outlines, which are other
 * outlines to read (not pages), resolved against the base given to Parse.
 *
 * Returns:
 *   []string — the include addresses, in document order
 *
 * Example:
 *   for _, inc := range doc.Includes() { fetchAndSplice(inc) }
 */
func (d *Doc) Includes() []string {
	var out []string
	var walk func(n *node)
	walk = func(n *node) {
		for _, c := range n.children {
			if c.include && c.url != "" {
				out = append(out, c.url)
			}
			walk(c)
		}
	}
	walk(d.root)
	return out
}

/** Splice makes the outlines of child part of d, in place of the include
 * outline that names it: the include becomes a plain group holding them, so
 * its pages are one list.
 *
 * Parameters:
 *   includeURL (string) — the address of the include outline (as in Includes)
 *   child (*Doc) — the outline that address held
 *
 * Example:
 *   for _, inc := range d.Includes() { d.Splice(inc, fetched[inc]) }
 */
func (d *Doc) Splice(includeURL string, child *Doc) {
	var walk func(n *node)
	walk = func(n *node) {
		for _, c := range n.children {
			if c.include && c.url == includeURL {
				c.children = append([]*node(nil), child.root.children...)
				if n == d.root {
					c.folder = siteFolder(c.url)
				}
				c.include, c.url, c.typ = false, "", ""
				continue
			}
			walk(c)
		}
	}
	walk(d.root)
}

/** Key is the form of an address two outlines and a browser agree on: scheme and
 * host in lower case, no fragment, a folder as its index.md, and .html as .md,
 * since a page is listed by its Markdown and may be shown by its HTML address.
 *
 * Parameters:
 *   address (string) — an absolute address
 *
 * Returns:
 *   string — the comparison form
 *
 * Example:
 *   toc.Key("HTTPS://Example.org/a.html#x") // "https://example.org/a.md"
 */
func Key(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return address
	}
	p := u.Path
	switch {
	case p == "" || strings.HasSuffix(p, "/"):
		p += "index.md"
	case strings.HasSuffix(strings.ToLower(p), ".html"):
		p = p[:len(p)-5] + ".md"
	case strings.HasSuffix(strings.ToLower(p), ".htm"):
		p = p[:len(p)-4] + ".md"
	}
	k := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + p
	if u.RawQuery != "" {
		k += "?" + u.RawQuery
	}
	return k
}

/** Neighbours returns the page before and the page after a page, by
 * BonBon's rule (markdownweb DR-0004): the outline is one flat list (AllPages),
 * previous is the line above, next the line below, and the ends wrap, so the
 * first page's previous is the last. A page listed more than once (a folder
 * and its readme, an .html and its .md) has the nearest different page on each
 * side. A site with one page has no neighbours: both are "". Whether a
 * neighbour can be fetched is not known here; Stops gives the candidates for a
 * caller that asks, as BonBon does.
 *
 * Parameters:
 *   page (string) — the address being shown; its .html, folder and fragment
 *     forms match the Markdown address the outline lists
 *
 * Returns:
 *   string — the previous page's address, or ""
 *   string — the next page's address, or ""
 *   bool — false when the outline does not list the page at all
 *
 * Example:
 *   prev, next, ok := d.Neighbours("https://example.org/blog/2026/11/19/a.html")
 */
func (d *Doc) Neighbours(page string) (prev, next string, ok bool) {
	return d.NeighboursOf(page)
}

/** NeighboursOf is Neighbours for a page known by more than one address: the
 * address it was asked for and the one the server said it is
 * (Content-Location, as for a folder served by its readme). The page is every
 * line that matches any of them.
 *
 * Parameters:
 *   addrs (...string) — the page's addresses
 *
 * Returns:
 *   string, string, bool — as Neighbours; false too when no address is given
 *
 * Example:
 *   prev, next, ok := d.NeighboursOf(view.Page.URL, view.Page.ContentLocation)
 */
func (d *Doc) NeighboursOf(addrs ...string) (prev, next string, ok bool) {
	before, after, ok := d.StopsOf(addrs...)
	return firstOf(before), firstOf(after), ok
}

/** NeighboursByPath is Neighbours for a page known only by its path under a
 * site root, as a local page is: an entry matches when its address's path ends
 * with "/" and rel (a.html matching a.md as in Key), whatever its scheme and
 * host, since the outline lists the published site's addresses.
 *
 * Parameters:
 *   rel (string) — the page's path below the site root, with "/" separators
 *
 * Returns:
 *   string, string, bool — as Neighbours; the addresses are the outline's own
 *
 * Example:
 *   prev, next, ok := d.NeighboursByPath("blog/2026/11/19/a.md")
 */
func (d *Doc) NeighboursByPath(rel string) (prev, next string, ok bool) {
	before, after, ok := d.StopsByPath(rel)
	return firstOf(before), firstOf(after), ok
}

/** Stops returns the candidates for the pages before and after a page, in the
 * order BonBon tries them: nearest first, wrapping around the ends, the page
 * itself left out, a page that is listed twice once. A caller that can ask
 * whether an address answers (BonBon sends one request, remembered) takes the
 * first that does; Neighbours is the first of each without asking.
 *
 * Parameters:
 *   page (string) — the address being shown, as for Neighbours
 *
 * Returns:
 *   []string — candidates for the previous page, going up the list
 *   []string — candidates for the next page, going down it
 *   bool — false when the outline does not list the page
 *
 * Example:
 *   before, after, ok := d.Stops("https://example.org/a.html")
 */
func (d *Doc) Stops(page string) (before, after []string, ok bool) {
	return d.StopsOf(page)
}

/** StopsOf is Stops for a page known by more than one address; see NeighboursOf.
 *
 * Parameters:
 *   addrs (...string) — the page's addresses
 *
 * Returns:
 *   []string, []string, bool — as Stops
 *
 * Example:
 *   before, after, ok := d.StopsOf(addr, contentLocation)
 */
func (d *Doc) StopsOf(addrs ...string) (before, after []string, ok bool) {
	want := map[string]bool{}
	for _, a := range addrs {
		want[Key(a)] = true
	}
	if len(want) == 0 {
		return nil, nil, false
	}
	return stopsIn(d.linesFor(addrs[0]), func(address string) bool { return want[Key(address)] })
}

/** StopsByPath is Stops for a page known only by its path under a site root;
 * see NeighboursByPath for how a path matches.
 *
 * Parameters:
 *   rel (string) — the page's path below the site root, with "/" separators
 *
 * Returns:
 *   []string, []string, bool — as Stops; the addresses are the outline's own
 *
 * Example:
 *   before, after, ok := d.StopsByPath("blog/a.md")
 */
func (d *Doc) StopsByPath(rel string) (before, after []string, ok bool) {
	want := "/" + strings.TrimPrefix(Key("http://x/" + strings.TrimPrefix(rel, "/"))[len("http://x"):], "/")
	match := func(address string) bool {
		u, err := url.Parse(Key(address))
		return err == nil && strings.HasSuffix(u.Path, want)
	}
	// A local page has no published address to compare with a site's folder, so
	// it is in an included site's list when that site lists it.
	for _, c := range d.root.children {
		if c.folder == "" {
			continue
		}
		if before, after, ok := stopsIn(pagesOf(c.children), match); ok {
			return before, after, true
		}
	}
	return stopsIn(d.AllPages(), match)
}

func firstOf(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// stopsIn is BonBon's lookup in one list: the lines that are the page are those
// match accepts; from the first of them go up, from the last go down, wrapping,
// skipping the page and any address already offered.
func stopsIn(lines []Entry, match func(address string) bool) (before, after []string, ok bool) {
	first, last := -1, -1
	for i, l := range lines {
		if match(l.URL) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return nil, nil, false
	}
	n := len(lines)
	walk := func(from, dir int) []string {
		var out []string
		seen := map[string]bool{}
		for step := 1; step < n; step++ {
			l := lines[((from+dir*step)%n+n)%n]
			if match(l.URL) || seen[Key(l.URL)] {
				continue
			}
			seen[Key(l.URL)] = true
			out = append(out, l.URL)
		}
		return out
	}
	return walk(first, -1), walk(last, 1), true
}

// otherAttr returns an attribute opml has no field for, such as the name
// antenna writes on calendar outlines.
func otherAttr(x *opml.Outline, name string) string {
	for _, a := range x.OtherAttr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
