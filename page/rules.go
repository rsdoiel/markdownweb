package page

import (
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

/** PrefersMarkdown reports whether an Accept header asks for Markdown in
 * preference to HTML: text/markdown is named with a quality above zero, and
 * either no HTML is acceptable (text/html, "text/*" and a bare star type all absent or
 * q=0) or Markdown has the higher quality. A tie goes to HTML, and a wildcard
 * never counts as asking for Markdown, so a browser and a wildcard-only Accept
 * get HTML. A server uses it to decide whether to answer with the .md copy.
 *
 * Parameters:
 *   accept (string) — the Accept request header
 *
 * Returns:
 *   bool — true when Markdown is preferred
 *
 * Example:
 *   page.PrefersMarkdown("text/markdown, text/x-opml") // true (BonBon)
 *   page.PrefersMarkdown("text/html, text/markdown")   // false (a tie)
 */
func PrefersMarkdown(accept string) bool {
	var qMarkdown, qHTML float64
	for _, entry := range strings.Split(accept, ",") {
		parts := strings.Split(entry, ";")
		mediaType := strings.ToLower(strings.TrimSpace(parts[0]))
		q := 1.0
		for _, param := range parts[1:] {
			k, v, ok := strings.Cut(strings.TrimSpace(param), "=")
			if ok && strings.EqualFold(strings.TrimSpace(k), "q") {
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					q = f
				}
			}
		}
		switch mediaType {
		case "text/markdown":
			qMarkdown = max(qMarkdown, q)
		case "text/html", "text/*", "*/*":
			qHTML = max(qHTML, q)
		}
	}
	return qMarkdown > 0 && (qHTML <= 0 || qMarkdown > qHTML)
}

/** MarkdownPath returns the Markdown path for a file path: x.html and x.htm
 * become x.md, x.md and x.markdown stay, anything else has none. The path is
 * cleaned and made relative. It is the file-system counterpart of
 * MarkdownSibling, which works on addresses and also maps directories.
 *
 * Parameters:
 *   p (string) — a file path, in either slash style
 *
 * Returns:
 *   string — the cleaned relative Markdown path, or ""
 *
 * Example:
 *   page.MarkdownPath("/blog/a.html") // "blog/a.md"
 *   page.MarkdownPath("a.txt")        // ""
 */
func MarkdownPath(p string) string {
	p = strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(p)), "/")
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown":
		return p
	case ".html", ".htm":
		return strings.TrimSuffix(p, path.Ext(p)) + ".md"
	}
	return ""
}
