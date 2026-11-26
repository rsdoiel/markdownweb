// Package reader turns an HTML page into Markdown so the browser can show a
// page from outside the Markdown web in a readable way, the way a text
// browser's reader view does. It keeps the content (headings, paragraphs,
// lists, links, quotes, code, tables, and images that have alt text) and drops
// what a terminal cannot use or should not trust: scripts, styles, menus,
// forms, hidden elements and every address that is not http, https or mailto.
// A page whose text is built by JavaScript has little in it; Convert says so.
package reader

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// MaxOutput caps the Markdown a page can produce, in bytes.
const MaxOutput = 2 << 20

/** Result is a converted page.
 *
 * Fields:
 *   Title (string) — the page title, or its first heading
 *   Markdown (string) — the content as Markdown, with absolute addresses
 *   Empty (bool) — the page had no text to show (a script-built page)
 *   Truncated (bool) — the output was cut at MaxOutput
 *
 * Example:
 *   r, err := reader.Convert(html, "https://example.org/a.html")
 *   if err == nil && !r.Empty { fmt.Println(r.Title) }
 */
type Result struct {
	Title     string
	Markdown  string
	Empty     bool
	Truncated bool
}

/** Convert reads an HTML page and returns its content as Markdown. Relative
 * links and images are resolved against base. The content is the largest
 * article on the page, else its main element, else its body.
 *
 * Parameters:
 *   src ([]byte) — the HTML
 *   base (string) — the address the page was fetched from
 *
 * Returns:
 *   Result — the converted page
 *   error — base is not an address, or the HTML cannot be read
 *
 * Example:
 *   r, err := reader.Convert(body, "https://x.org/post.html")
 */
func Convert(src []byte, base string) (Result, error) {
	b, err := url.Parse(base)
	if err != nil || b.Host == "" && b.Scheme == "" {
		return Result{}, fmt.Errorf("reader: %q is not an address", base)
	}
	doc, err := html.Parse(bytes.NewReader(src))
	if err != nil {
		return Result{}, fmt.Errorf("reader: %w", err)
	}
	root, isBody := contentRoot(doc)
	c := &conv{base: b, dropHeader: isBody}
	blocks := c.blocks(root)
	md := strings.Join(blocks, "\n\n")
	r := Result{Title: pageTitle(doc), Truncated: c.full}
	r.Markdown = strings.TrimSpace(md)
	if r.Markdown == "" {
		r.Empty = true
		return r, nil
	}
	r.Markdown += "\n"
	return r, nil
}

// conv holds the state of one conversion.
type conv struct {
	base       *url.URL
	dropHeader bool // a <header> is site chrome when no article was found
	size       int
	full       bool
}

// skipTags are elements with nothing a reader should show.
var skipTags = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Template: true, atom.Svg: true,
	atom.Iframe: true, atom.Canvas: true, atom.Object: true, atom.Embed: true, atom.Head: true,
	atom.Form: true, atom.Select: true, atom.Textarea: true, atom.Input: true, atom.Button: true,
	atom.Nav: true, atom.Aside: true, atom.Footer: true, atom.Dialog: true, atom.Title: true,
}

// blockTags are elements that stand on their own line.
var blockTags = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true, atom.Main: true,
	atom.Header: true, atom.Figure: true, atom.Figcaption: true, atom.Address: true,
	atom.Details: true, atom.Summary: true, atom.H1: true, atom.H2: true, atom.H3: true,
	atom.H4: true, atom.H5: true, atom.H6: true, atom.Ul: true, atom.Ol: true, atom.Li: true,
	atom.Dl: true, atom.Dt: true, atom.Dd: true, atom.Blockquote: true, atom.Pre: true,
	atom.Hr: true, atom.Table: true, atom.Body: true, atom.Html: true, atom.Tr: true,
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

// hidden reports whether an element is marked as not shown.
func hidden(n *html.Node) bool {
	if hasAttr(n, "hidden") || strings.EqualFold(attr(n, "aria-hidden"), "true") {
		return true
	}
	st := strings.ReplaceAll(strings.ToLower(attr(n, "style")), " ", "")
	return strings.Contains(st, "display:none") || strings.Contains(st, "visibility:hidden")
}

func (c *conv) skip(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	if skipTags[n.DataAtom] || hidden(n) {
		return true
	}
	return n.DataAtom == atom.Header && c.dropHeader
}

// textLen is the number of text characters under n, outside skipped elements.
func textLen(n *html.Node) int {
	if n.Type == html.ElementNode && (skipTags[n.DataAtom] || hidden(n)) {
		return 0
	}
	if n.Type == html.TextNode {
		return len(strings.TrimSpace(n.Data))
	}
	total := 0
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		total += textLen(ch)
	}
	return total
}

// findAll returns the elements with the given tag under n, in document order.
func findAll(n *html.Node, tag atom.Atom) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.ElementNode && x.DataAtom == tag {
			out = append(out, x)
		}
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return out
}

// contentRoot picks the part of the page to show: the article with the most
// text (when it has a paragraph's worth), else <main>, else <body>. The bool is
// true when it fell back to <body>.
func contentRoot(doc *html.Node) (*html.Node, bool) {
	var best *html.Node
	bestLen := 0
	for _, a := range findAll(doc, atom.Article) {
		if l := textLen(a); l > bestLen {
			best, bestLen = a, l
		}
	}
	if best != nil && bestLen >= 200 {
		return best, false
	}
	if m := findAll(doc, atom.Main); len(m) > 0 && textLen(m[0]) > 0 {
		return m[0], false
	}
	if b := findAll(doc, atom.Body); len(b) > 0 {
		return b[0], true
	}
	return doc, true
}

// pageTitle is the <title>, else the first heading.
func pageTitle(doc *html.Node) string {
	if t := findAll(doc, atom.Title); len(t) > 0 {
		if s := collapse(plainText(t[0])); s != "" {
			return s
		}
	}
	if h := findAll(doc, atom.H1); len(h) > 0 {
		return collapse(plainText(h[0]))
	}
	return ""
}

func plainText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			sb.WriteString(x.Data)
		}
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return sb.String()
}

// collapse turns every run of white space into one space and trims the ends.
func collapse(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, " ", " ")), " ")
}

// blocks converts the children of n into Markdown blocks.
func (c *conv) blocks(n *html.Node) []string {
	var out []string
	var buf strings.Builder
	flush := func() {
		if p := strings.TrimSpace(trimBreaks(buf.String())); p != "" {
			out = append(out, escapeStart(p))
		}
		buf.Reset()
	}
	add := func(blocks ...string) {
		for _, b := range blocks {
			if b = strings.TrimSpace(b); b != "" && !c.full {
				c.size += len(b) + 2
				if c.size > MaxOutput {
					c.full = true
					return
				}
				out = append(out, b)
			}
		}
	}
	for ch := n.FirstChild; ch != nil && !c.full; ch = ch.NextSibling {
		if c.skip(ch) {
			continue
		}
		if ch.Type == html.ElementNode && blockTags[ch.DataAtom] {
			flush()
			switch ch.DataAtom {
			case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
				if t := strings.TrimSpace(c.inline(ch)); t != "" {
					add(strings.Repeat("#", headingLevel(ch.DataAtom)) + " " + t)
				}
			case atom.Ul, atom.Ol:
				add(c.list(ch, 0))
			case atom.Blockquote:
				inner := strings.Join(c.blocks(ch), "\n\n")
				if inner != "" {
					add(prefixLines(inner, "> "))
				}
			case atom.Pre:
				add(fence(plainText(ch)))
			case atom.Hr:
				add("---")
			case atom.Table:
				add(c.table(ch))
			case atom.P, atom.Li, atom.Dt, atom.Dd, atom.Summary, atom.Figcaption, atom.Address, atom.Tr:
				if t := strings.TrimSpace(c.inline(ch)); t != "" {
					add(escapeStart(t))
				}
			default: // div, section, article, main, header, figure, details...
				add(c.blocks(ch)...)
			}
			continue
		}
		buf.WriteString(c.inlineNode(ch, buf.String()))
	}
	flush()
	return out
}

// headingLevel is the level, 1 to 6, of a heading element. It is a lookup, not
// arithmetic on the atoms: atom values are hashes, not consecutive numbers, and
// subtracting them once turned <h2> into 263,425 # marks.
func headingLevel(a atom.Atom) int {
	switch a {
	case atom.H1:
		return 1
	case atom.H2:
		return 2
	case atom.H3:
		return 3
	case atom.H4:
		return 4
	case atom.H5:
		return 5
	case atom.H6:
		return 6
	}
	return 1
}

// list renders a ul or ol with nested lists indented under their item.
func (c *conv) list(n *html.Node, depth int) string {
	var lines []string
	num := 0
	for li := n.FirstChild; li != nil && !c.full; li = li.NextSibling {
		if li.Type != html.ElementNode || li.DataAtom != atom.Li || c.skip(li) {
			continue
		}
		num++
		marker := "- "
		if n.DataAtom == atom.Ol {
			marker = fmt.Sprintf("%d. ", num)
		}
		indent := strings.Repeat("  ", depth)
		var text strings.Builder
		var nested []string
		for ch := li.FirstChild; ch != nil; ch = ch.NextSibling {
			if c.skip(ch) {
				continue
			}
			if ch.Type == html.ElementNode && (ch.DataAtom == atom.Ul || ch.DataAtom == atom.Ol) {
				nested = append(nested, c.list(ch, depth+1))
				continue
			}
			text.WriteString(c.inlineNode(ch, text.String()))
		}
		item := strings.TrimSpace(trimBreaks(text.String()))
		if item == "" && len(nested) == 0 {
			continue
		}
		lines = append(lines, indent+marker+escapeStart(item))
		lines = append(lines, nested...)
		c.size += len(item) + 4
		if c.size > MaxOutput {
			c.full = true
		}
	}
	return strings.Join(lines, "\n")
}

// table renders rows and cells as a pipe table; the first row is the header.
func (c *conv) table(n *html.Node) string {
	var rows [][]string
	for _, tr := range findAll(n, atom.Tr) {
		var cells []string
		for ch := tr.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type == html.ElementNode && (ch.DataAtom == atom.Td || ch.DataAtom == atom.Th) && !c.skip(ch) {
				cell := strings.TrimSpace(c.inline(ch)) // inline text already escapes |
				cells = append(cells, strings.ReplaceAll(cell, "\n", " "))
			}
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	}
	if len(rows) == 0 {
		return ""
	}
	width := 0
	for _, r := range rows {
		width = max(width, len(r))
	}
	line := func(r []string) string {
		for len(r) < width {
			r = append(r, "")
		}
		return "| " + strings.Join(r, " | ") + " |"
	}
	sep := make([]string, width)
	for i := range sep {
		sep[i] = "---"
	}
	out := []string{line(rows[0]), line(sep)}
	for _, r := range rows[1:] {
		out = append(out, line(r))
	}
	return strings.Join(out, "\n")
}

// inline converts everything under n to inline Markdown text.
func (c *conv) inline(n *html.Node) string {
	var sb strings.Builder
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if c.skip(ch) {
			continue
		}
		sb.WriteString(c.inlineNode(ch, sb.String()))
	}
	return strings.TrimSpace(trimBreaks(sb.String()))
}

// inlineNode converts one node as inline text. before is what is already in
// the line, so a space is not doubled.
func (c *conv) inlineNode(n *html.Node, before string) string {
	switch n.Type {
	case html.TextNode:
		t := strings.ReplaceAll(n.Data, " ", " ")
		if strings.TrimSpace(t) == "" {
			if before == "" || strings.HasSuffix(before, " ") || strings.HasSuffix(before, "\n") {
				return ""
			}
			return " "
		}
		lead := unicode.IsSpace([]rune(t)[0])
		trail := unicode.IsSpace([]rune(t)[len([]rune(t))-1])
		s := escapeInline(strings.Join(strings.Fields(t), " "))
		if lead && before != "" && !strings.HasSuffix(before, " ") && !strings.HasSuffix(before, "\n") {
			s = " " + s
		}
		if trail {
			s += " "
		}
		return s
	case html.ElementNode:
	default:
		return ""
	}
	switch n.DataAtom {
	case atom.Br:
		return "\\\n"
	case atom.Strong, atom.B:
		return wrap("**", c.inline(n))
	case atom.Em, atom.I:
		return wrap("*", c.inline(n))
	case atom.Code, atom.Tt, atom.Kbd, atom.Samp:
		return code(plainText(n))
	case atom.A:
		text := c.inline(n)
		href := c.safeAddress(attr(n, "href"), true)
		if text == "" || href == "" {
			return text
		}
		return "[" + text + "](" + mdDest(href) + ")"
	case atom.Img:
		alt := collapse(attr(n, "alt"))
		src := c.safeAddress(attr(n, "src"), false)
		if alt == "" || src == "" {
			return "" // a picture with no alt text is decoration
		}
		return "![" + escapeInline(alt) + "](" + mdDest(src) + ")"
	}
	// any other element, block-level ones inside inline text included, is flattened
	inner := c.inline(n)
	if blockTags[n.DataAtom] && inner != "" {
		return " " + inner + " "
	}
	return inner
}

// safeAddress resolves href against the page address and returns it when it
// is http or https (or mailto, when allowMail), else "".
func (c *conv) safeAddress(href string, allowMail bool) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	abs := c.base.ResolveReference(u)
	switch abs.Scheme {
	case "http", "https":
		return abs.String()
	case "mailto":
		if allowMail {
			return abs.String()
		}
	}
	return ""
}

// escapeInline backslash-escapes the characters Markdown would take as markup
// or HTML, so page text can never become a link, emphasis or a tag.
func escapeInline(s string) string {
	var sb strings.Builder
	for i, r := range s {
		switch r {
		case '\\', '`', '*', '_', '[', ']', '<', '>', '~', '|':
			sb.WriteByte('\\')
		case '&':
			if hasEntity(s[i:]) {
				sb.WriteByte('\\')
			}
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

func hasEntity(s string) bool {
	end := strings.IndexByte(s, ';')
	if end < 2 || end > 12 {
		return false
	}
	for _, r := range s[1:end] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '#' {
			return false
		}
	}
	return true
}

// escapeStart escapes the first characters of a paragraph that would start a
// heading, quote, list, rule or numbered list.
func escapeStart(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '#', '>', '-', '+', '=':
		return `\` + s
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i < len(s) && (s[i] == '.' || s[i] == ')') {
		return s[:i] + `\` + s[i:]
	}
	return s
}

// mdDest writes an address as a link destination.
func mdDest(a string) string {
	a = strings.NewReplacer(" ", "%20", "<", "%3C", ">", "%3E", "\n", "%0A").Replace(a)
	if strings.ContainsAny(a, "()") {
		return "<" + a + ">"
	}
	return a
}

func wrap(mark, s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	return mark + s + mark
}

// code writes s as an inline code span, using a fence longer than any run of
// backticks in it.
func code(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	fence, run, longest := "`", 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence = strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}

// fence writes s as a fenced code block.
func fence(s string) string {
	s = strings.Trim(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if strings.TrimSpace(s) == "" {
		return ""
	}
	f := "```"
	for strings.Contains(s, f) {
		f += "`"
	}
	return f + "\n" + s + "\n" + f
}

func prefixLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(prefix+l, " ")
	}
	return strings.Join(lines, "\n")
}

// trimBreaks removes hard breaks and spaces from the end of a paragraph.
func trimBreaks(s string) string {
	for {
		t := strings.TrimRight(s, " \t\n")
		t = strings.TrimSuffix(t, "\\")
		if t == s {
			return strings.TrimRight(s, " \t")
		}
		s = t
	}
}
