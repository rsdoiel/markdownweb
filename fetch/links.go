package fetch

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// The bounds of ParseLinks (markdownweb DR-0005 item 3).
const (
	maxLinks     = 16   // links kept from one response
	maxLinkValue = 2048 // bytes of one link value
	maxLinkTitle = 128  // bytes of a title
)

/** Link is one link of a response's Link header (RFC 8288), as far as a
 * Markdown-web client needs it.
 *
 * Fields:
 *   Target (string)   — the address, resolved against the response's final address; always http or https
 *   Rel    ([]string) — the relation types, lower case; nil when the link names none
 *   Type   (string)   — the media type the link says its target has, or ""
 *   Title  (string)   — the link's title, at most 128 bytes, with no control characters, or ""
 *
 * Example:
 *   l := fetch.Link{Target: "http://127.0.0.1:8900/opensearch.xml", Rel: []string{"search"},
 *       Type: "application/opensearchdescription+xml", Title: "cmarkfind"}
 */
type Link struct {
	Target string
	Rel    []string
	Type   string
	Title  string
}

/** ParseLinks reads the values of a response's Link headers. It reads the subset
 * of RFC 8288 that is needed: comma-separated values, each a target in angle
 * brackets followed by ;-separated parameters, of which rel, type and title are
 * kept (quoted or token form, the first of each) and the rest ignored. A value
 * that does not parse is skipped, never an error; a target that is not http or
 * https once resolved is dropped. At most 16 links are kept, a value over 2 KiB
 * is skipped, and a title is cut at 128 bytes and stripped of control
 * characters, so what comes back is bounded and safe to show whatever a server
 * sent.
 *
 * Parameters:
 *   header ([]string) — the values of every Link header, as http.Header.Values returns them
 *   base   (*url.URL) — the address a relative target resolves against; nil allows only absolute targets
 *
 * Returns:
 *   []Link — the links in the order given, nil when there are none
 *
 * Example:
 *   base, _ := url.Parse("http://127.0.0.1:8900/search?q=x")
 *   links := fetch.ParseLinks([]string{`</opensearch.xml>; rel="search"`}, base)
 *   // links[0].Target == "http://127.0.0.1:8900/opensearch.xml"
 */
func ParseLinks(header []string, base *url.URL) []Link {
	var out []Link
	for _, h := range header {
		for i := 0; i < len(h) && len(out) < maxLinks; {
			end := valueEnd(h, i)
			v := strings.TrimSpace(h[i:end])
			i = end + 1
			if v == "" || len(v) > maxLinkValue {
				continue
			}
			if l, ok := parseLink(v, base); ok {
				out = append(out, l)
			}
		}
		if len(out) >= maxLinks {
			break
		}
	}
	return out
}

// valueEnd returns the index of the comma that ends the link value starting at
// i, or len(h): a comma inside <...> or a quoted string does not end it.
func valueEnd(h string, i int) int {
	inAngle, inQuote := false, false
	for ; i < len(h); i++ {
		c := h[i]
		switch {
		case inQuote:
			if c == '\\' {
				i++
			} else if c == '"' {
				inQuote = false
			}
		case inAngle:
			if c == '>' {
				inAngle = false
			}
		case c == '"':
			inQuote = true
		case c == '<':
			inAngle = true
		case c == ',':
			return i
		}
	}
	return len(h)
}

// parseLink reads one value: <target>; param; param.
func parseLink(v string, base *url.URL) (Link, bool) {
	if v[0] != '<' {
		return Link{}, false
	}
	close := strings.IndexByte(v, '>')
	if close < 0 {
		return Link{}, false
	}
	ref := v[1:close]
	if ref == "" || strings.ContainsAny(ref, " \t<\"") || hasControl(ref) {
		return Link{}, false
	}
	u, err := url.Parse(ref)
	if err != nil {
		return Link{}, false
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Link{}, false
	}
	l := Link{Target: u.String()}
	var haveRel, haveType, haveTitle bool
	for _, p := range splitParams(v[close+1:]) {
		name, val, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		val = unquote(strings.TrimSpace(val))
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "rel":
			if !haveRel {
				haveRel = true
				l.Rel = strings.Fields(strings.ToLower(val))
			}
		case "type":
			if !haveType {
				haveType = true
				l.Type = strings.TrimSpace(val)
			}
		case "title":
			if !haveTitle {
				haveTitle = true
				l.Title = cleanTitle(val)
			}
		}
	}
	return l, true
}

// splitParams splits ;param;param on semicolons outside quotes.
func splitParams(s string) []string {
	var out []string
	inQuote := false
	start := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case inQuote:
			if c == '\\' {
				i++
			} else if c == '"' {
				inQuote = false
			}
		case c == '"':
			inQuote = true
		case c == ';':
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// unquote removes the quotes of a quoted-string and its backslash escapes.
func unquote(s string) string {
	if len(s) < 2 || s[0] != '"' {
		return s
	}
	s = strings.TrimSuffix(s[1:], `"`)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func hasControl(s string) bool {
	for _, r := range s {
		if isControl(r) {
			return true
		}
	}
	return false
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) }

// cleanTitle drops control characters and invalid UTF-8 and cuts the rest at
// maxLinkTitle bytes on a character boundary.
func cleanTitle(s string) string {
	var b strings.Builder
	for _, r := range s {
		if isControl(r) || r == utf8.RuneError {
			continue
		}
		if b.Len()+utf8.RuneLen(r) > maxLinkTitle {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}
