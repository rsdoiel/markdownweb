package page

import (
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
	"time"

	"github.com/rsdoiel/markdownweb/debuglog"
)

/** Kind says what a link that is not Markdown and not HTML points at. HTML has
 * its own path, the reader view. The kinds are BonBon's: XML formats and text
 * are shown as source, an image and any other file are noted, not shown.
 *
 * Example:
 *   k := page.Classify("application/rss+xml", "/feed.xml")
 *   if k == page.KindXML { fmt.Println("source") }
 */
type Kind string

const (
	KindXML   Kind = "xml"   // RSS, Atom, OPML, any XML: shown as source
	KindText  Kind = "text"  // plain text, code, JSON, YAML, CSV: shown as source
	KindImage Kind = "image" // a picture a terminal cannot show: noted
	KindFile  Kind = "file"  // anything else, an archive or a PDF: noted, not downloaded
)

// maxSource caps the source shown: a log file or a data dump larger than this
// is cut, and the page says so.
const maxSource = 1 << 20

/** Classify says what a non-Markdown, non-HTML answer is, by the server's
 * content type first and the path's extension when the type is generic or
 * missing, as BonBon does (pageRequests.js getPageKind).
 *
 * Parameters:
 *   contentType (string) — the Content-Type header, "" for none
 *   p (string) — the path of the address, for its extension
 *
 * Returns:
 *   Kind — KindXML, KindText, KindImage or KindFile
 *
 * Example:
 *   page.Classify("application/rss+xml", "/feed") // page.KindXML
 */
func Classify(contentType, p string) Kind {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mt = strings.ToLower(strings.TrimSpace(contentType))
	}
	ext := strings.ToLower(path.Ext(p))
	has := func(exts ...string) bool {
		for _, e := range exts {
			if ext == e {
				return true
			}
		}
		return false
	}
	switch {
	case mt == "text/xml" || mt == "application/xml" || strings.HasSuffix(mt, "+xml") || mt == "text/x-opml" || mt == "application/x-opml" || has(".xml", ".rss", ".opml", ".atom", ".rdf"):
		return KindXML
	case strings.HasPrefix(mt, "image/") || has(".gif", ".png", ".jpg", ".jpeg", ".webp", ".svg", ".ico"):
		return KindImage
	case strings.HasPrefix(mt, "text/") || mt == "application/json" || mt == "application/javascript" || mt == "application/x-javascript" || mt == "application/yaml" ||
		has(".txt", ".js", ".css", ".json", ".yaml", ".yml", ".py", ".sh", ".c", ".h", ".php", ".rb", ".go", ".rs", ".java", ".ts", ".sql", ".csv", ".ini", ".toml"):
		return KindText
	}
	return KindFile
}

/** IsOther reports whether err from Fetch is a good answer that is neither
 * Markdown nor HTML, so FetchOther can show it as source or note it.
 *
 * Example:
 *   if page.IsOther(err) { p, err = page.FetchOther(ctx, addr) }
 */
func IsOther(err error) bool {
	var nm *notMarkdown
	return errors.As(err, &nm) && !nm.html
}

/** FetchOther gets a link that is not a Markdown page or an HTML page and makes
 * a page of it: XML and text are shown as source (a fenced block, control
 * characters removed, cut at 1 MB), an image and any other file are noted with
 * their type, size and address and are not downloaded. The returned page has
 * Kind set; for source, Raw is the text, so a copy can be edited.
 *
 * Parameters:
 *   ctx (context.Context) — cancellation and deadline
 *   rawurl (string) — an http or https address
 *
 * Returns:
 *   *Page — the page; its URL is the final address
 *   error — wraps ErrNoInput (404 or 410), ErrUnavailable or ErrData
 *
 * Example:
 *   p, err := page.FetchOther(ctx, "https://x.org/feed.xml")
 */
func FetchOther(ctx context.Context, rawurl string) (*Page, error) {
	start := time.Now()
	u, err := url.Parse(rawurl)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("%w: %q is not an http or https address", ErrData, rawurl)
	}
	resp, err := Fetcher().Open(ctx, rawurl, "*/*")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return nil, fmt.Errorf("%w: %s: %s", ErrNoInput, rawurl, resp.Status)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: %s: %s", ErrData, rawurl, resp.Status)
	}
	ct := resp.Header.Get("Content-Type")
	final := resp.Request.URL.String()
	kind := Classify(ct, resp.Request.URL.Path)
	var body []byte
	if kind == KindXML || kind == KindText {
		body, err = io.ReadAll(io.LimitReader(resp.Body, maxSource+1))
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrUnavailable, err)
		}
	}
	p := otherPage(kind, final, ct, resp.ContentLength, body)
	debuglog.FromContext(ctx).Event("fetch.other", "url", rawurl, "final", final, "content_type", ct, "kind", string(p.Kind), "bytes", len(body), "length", resp.ContentLength, "ms", time.Since(start))
	return p, nil
}

/** OtherFromFile makes a page of a local file that is not Markdown, for a link
 * in a local site (cmarkbrowser DR-0001): by its extension, as FetchOther does
 * by the path of an address. Only a file inside the site root may be given;
 * the caller checks that.
 *
 * Parameters:
 *   p (string) — the absolute path of the file
 *
 * Returns:
 *   *Page — the page; its URL is the file: address, as ReadFile's
 *   error — wraps ErrNoInput for a missing or unreadable file
 *
 * Example:
 *   pg, err := page.OtherFromFile("/site/feed.xml")
 */
func OtherFromFile(p string) (*Page, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoInput, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a file", ErrNoInput, p)
	}
	kind := Classify("", p)
	var body []byte
	if kind == KindXML || kind == KindText {
		if body, err = io.ReadAll(io.LimitReader(f, maxSource+1)); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrNoInput, err)
		}
	}
	pg := otherPage(kind, (&url.URL{Scheme: "file", Path: filepath.ToSlash(p)}).String(), "", info.Size(), body)
	return pg, nil
}

// otherPage builds the page for a source or a note. A source with a NUL byte is
// not text and becomes a note.
func otherPage(kind Kind, address, contentType string, size int64, body []byte) *Page {
	u, _ := url.Parse(address)
	name := safeName(path.Base(u.Path))
	meta := map[string]any{"title": name}
	if kind == KindXML || kind == KindText {
		if strings.IndexByte(string(body[:min(len(body), 8000)]), 0) >= 0 {
			kind = KindFile
		}
	}
	if kind == KindXML || kind == KindText {
		truncated := len(body) > maxSource
		if truncated {
			body = body[:maxSource]
		}
		text := clean(string(body))
		var sb strings.Builder
		fmt.Fprintf(&sb, "# %s\n\n*Source, shown as text (%s).*\n\n", name, describe(contentType, kind))
		fence := strings.Repeat("`", max(3, longestRun(text, '`')+1))
		lang := ""
		if kind == KindXML {
			lang = "xml"
		}
		sb.WriteString(fence + lang + "\n" + strings.TrimRight(text, "\n") + "\n" + fence + "\n")
		if truncated {
			fmt.Fprintf(&sb, "\n*Cut at %d bytes.*\n", maxSource)
		}
		return &Page{URL: address, Meta: meta, Body: sb.String(), Raw: text, Kind: kind}
	}
	what := "A file"
	note := "It was not downloaded."
	if kind == KindImage {
		what = "An image"
		note = "A terminal cannot show it; open the address in a viewer or a browser."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n%s (%s", name, what, describe(contentType, kind))
	if size >= 0 {
		fmt.Fprintf(&sb, ", %s", humanSize(size))
	}
	fmt.Fprintf(&sb, "). %s\n\nAddress: %s\n", note, codeSpan(address))
	return &Page{URL: address, Meta: meta, Body: sb.String(), Kind: kind}
}

// describe names a type: the server's, else the kind.
func describe(contentType string, kind Kind) string {
	if mt, _, err := mime.ParseMediaType(contentType); err == nil && mt != "" {
		return mt
	}
	return string(kind)
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// clean makes text safe to put in a terminal: valid UTF-8, CRLF as a newline,
// and no control characters except tab and newline, so nothing in a file can
// move the cursor, set a title or start an escape sequence. The C1 controls,
// DEL and the bidirectional overrides that make text read as something it is
// not are dropped too.
func clean(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			sb.WriteRune(r)
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f, r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x2028, r == 0x2029, r == 0xfeff:
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// safeName is a file name fit for a heading: letters, digits, dot, dash and
// underscore, the rest replaced; case kept.
func safeName(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			sb.WriteRune(r)
		default:
			sb.WriteByte('_')
		}
	}
	if sb.Len() == 0 || sb.String() == "." || sb.String() == ".." {
		return "file"
	}
	return sb.String()
}

// longestRun is the length of the longest run of c in s.
func longestRun(s string, c rune) int {
	best, cur := 0, 0
	for _, r := range s {
		if r == c {
			cur++
			best = max(best, cur)
		} else {
			cur = 0
		}
	}
	return best
}

// codeSpan wraps s as a Markdown code span that s cannot break out of.
func codeSpan(s string) string {
	s = clean(s)
	d := strings.Repeat("`", longestRun(s, '`')+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		s = " " + s + " "
	}
	return d + s + d
}
