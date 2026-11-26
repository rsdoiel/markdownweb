// Package toc reads a site's table of contents, the OPML outline BonBon looks
// for at /.well-known/toc.opml, and answers one question about it: what page
// comes before and what page after a given page. antennaApp writes the file
// (its DR-0013); Dave Winer's sites publish their own, so the parser reads
// what both write, including the ISO-8859-1 that Dave's declares.
//
// The rule for "before" and "after" is antenna's (DR-0013 5), which agrees with
// Dave Winer's explicit urlPrev and urlNext:
//
//   - Only outlines that are pages count: a url and no outlines under them. An
//     outline that has both (a month page with its days under it) is an index,
//     not a step in the sequence; an include outline is not a page.
//   - A group with no type and no url (antenna's Pages, Posts) is a list of its
//     own: arrows stop at its ends. Outlines of another kind (calendarYear and
//     the like, as in Dave's file) belong to the list around them.
//   - A list with calendar outlines in it is newest first, unless the numbers
//     in their name attributes run upward (antenna writes oldest first). Newest
//     first, the previous page is the next line down (the older one) and the
//     next page is the line above. In any other list the previous page is the
//     line above.
//
// What BonBon itself does with the lines of an outline, for its own arrows, is
// not documented and could not be tested.
package toc

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/rsdoiel/opml"
)

/** Entry is a page the outline lists.
 *
 * Fields:
 *   Text (string) — the outline's text
 *   URL (string) — its address, absolute
 *
 * Example:
 *   for _, e := range doc.Entries() { fmt.Println(e.Text, e.URL) }
 */
type Entry struct {
	Text string
	URL  string // absolute
}

type node struct {
	text, typ, name, url string // url is absolute, "" when none
	include              bool   // type="include": url is another outline, not a page
	children             []*node
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

// leaf is a page of the outline with where it sits.
type leaf struct {
	Entry
	scope   *node // the list it belongs to
	reverse bool  // newest first
	key     string
}

func isCalendar(typ string) bool {
	switch strings.ToLower(typ) {
	case "calendaryear", "calendarmonth", "calendarday":
		return true
	}
	return false
}

// calendarOrder says which way the calendar outlines around c run, from the
// numbers in the name attributes of c's siblings of the same type: -1 when they
// rise (2025 then 2026: oldest first, as antenna writes since BonBon reads the
// lines downward, antennaApp DR-0013 amendment), +1 when they fall (Dave
// Winer's newest first), 0 when there are not two different numbers to compare.
func calendarOrder(parent, c *node) int {
	prev := -1
	for _, sib := range parent.children {
		if sib.typ != c.typ {
			continue
		}
		v, err := strconv.Atoi(strings.TrimSpace(sib.name))
		if err != nil {
			continue
		}
		if prev >= 0 && v != prev {
			if v < prev {
				return 1
			}
			return -1
		}
		prev = v
	}
	return 0
}

// leaves lists the pages of the outline in document order.
func (d *Doc) leaves() []leaf {
	var out []leaf
	// dir is the order a level of calendar outlines settled on, which the levels
	// below it that cannot tell for themselves (one month in a year) inherit.
	var walk func(n, scope *node, reverse bool, dir int)
	walk = func(n, scope *node, reverse bool, dir int) {
		for _, c := range n.children {
			childScope, childReverse, childDir := scope, reverse, dir
			if c.typ == "" && c.url == "" && !c.include && len(c.children) > 0 {
				childScope, childReverse, childDir = c, false, 0 // a plain group is a list of its own
			}
			if isCalendar(c.typ) {
				if d := calendarOrder(n, c); d != 0 {
					childDir = d
				}
				childReverse = childDir != -1 // undecided: Dave's newest first
			}
			if c.url != "" && len(c.children) == 0 && !c.include {
				out = append(out, leaf{Entry: Entry{Text: c.text, URL: c.url}, scope: childScope, reverse: childReverse, key: Key(c.url)})
			}
			walk(c, childScope, childReverse, childDir)
		}
	}
	walk(d.root, d.root, false, 0)
	return out
}

/** Entries returns the pages the outline lists, in document order.
 *
 * Returns:
 *   []Entry — the pages, none for an empty outline
 *
 * Example:
 *   for _, e := range doc.Entries() { fmt.Println(e.URL) }
 */
func (d *Doc) Entries() []Entry {
	var out []Entry
	for _, l := range d.leaves() {
		out = append(out, l.Entry)
	}
	return out
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

/** Neighbours returns the page before and the page after a page in the
 * outline, by the rule in the package comment. An empty string means there is
 * none: the page is first or last in its list.
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
	want := Key(page)
	return d.neighboursWhere(func(l leaf) bool { return l.key == want })
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
	want := "/" + strings.TrimPrefix(Key("http://x/" + strings.TrimPrefix(rel, "/"))[len("http://x"):], "/")
	return d.neighboursWhere(func(l leaf) bool {
		u, err := url.Parse(l.key)
		return err == nil && strings.HasSuffix(u.Path, want)
	})
}

func (d *Doc) neighboursWhere(match func(leaf) bool) (prev, next string, ok bool) {
	all := d.leaves()
	at := -1
	for i, l := range all {
		if match(l) {
			at = i
			break
		}
	}
	if at < 0 {
		return "", "", false
	}
	// the list the page is in, in document order
	var list []leaf
	pos := 0
	for i, l := range all {
		if l.scope == all[at].scope {
			if i == at {
				pos = len(list)
			}
			list = append(list, l)
		}
	}
	before, after := "", ""
	if pos > 0 {
		before = list[pos-1].URL
	}
	if pos+1 < len(list) {
		after = list[pos+1].URL
	}
	if all[at].reverse {
		return after, before, true // newest first: previous is the older, the line below
	}
	return before, after, true
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
