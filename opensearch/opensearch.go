// Package opensearch reads an OpenSearch 1.1 description, the document a
// search service publishes to say how it is searched, and expands the URL
// templates in it, for a Markdown-web client that finds a service by a Link
// header or a site's well-known files (markdownweb DR-0005). It reads the
// subset a client needs, bounded for a hostile document: 64 KiB, 16 Url
// elements, 16 levels of nesting, no DOCTYPE and no entities.
package opensearch

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/rsdoiel/markdownweb/fetch"
)

// The bounds (markdownweb DR-0005).
const (
	// MaxDescription is the most bytes of a description that are read.
	MaxDescription = 64 << 10
	maxUrls        = 16
	maxDepth       = 16
	maxExpanded    = 8000
	nsURI          = "http://a9.com/-/spec/opensearch/1.1/"
	// DescriptionType is the media type of a description.
	DescriptionType = "application/opensearchdescription+xml"
)

// Errors. Each is also a fetch.ErrData, the workspace's class for content that is wrong.
var (
	// ErrBad marks a description that cannot be used: not well formed, not in the
	// OpenSearch namespace, over a bound, or missing what the specification requires.
	ErrBad = fmt.Errorf("%w: bad OpenSearch description", fetch.ErrData)
	// ErrUnknownParam marks a template with a required parameter this client does not know.
	ErrUnknownParam = fmt.Errorf("%w: template has a required parameter this client does not know", fetch.ErrData)
	// ErrTooLong marks an expanded address over 8,000 bytes.
	ErrTooLong = fmt.Errorf("%w: expanded address is too long", fetch.ErrData)
	// ErrCrossOrigin marks a service on another origin than a restricted Policy allows.
	ErrCrossOrigin = fmt.Errorf("%w: search service is on another origin", fetch.ErrData)
	// ErrNotFound marks a description address that answers 404 or 410 (workspace class no_input).
	ErrNotFound = errors.New("no such search description")
	// ErrNoSearch marks a description with no usable Url element.
	ErrNoSearch = fmt.Errorf("%w: no usable search URL", fetch.ErrData)
)

/** Url is one Url element of a description: a template for a kind of response.
 *
 * Fields:
 *   Type     (string) — the media type the service answers with, e.g. "text/markdown"
 *   Template (string) — an http or https address with {searchTerms} and optional {count?}, {startIndex?}
 *   Rel      (string) — the relation; empty means results
 *
 * Example:
 *   u := opensearch.Url{Type: "text/markdown", Template: "http://127.0.0.1:8900/search?q={searchTerms}"}
 */
type Url struct {
	Type     string
	Template string
	Rel      string
}

/** Description is what a service says about its search.
 *
 * Fields:
 *   ShortName   (string) — the service's name
 *   Description (string) — a sentence about it
 *   Urls        ([]Url)  — its usable Url elements, in document order
 *
 * Example:
 *   d, err := opensearch.Parse(r)
 */
type Description struct {
	ShortName   string
	Description string
	Urls        []Url
}

/** Parse reads one description. It reads at most 64 KiB, requires the root
 * OpenSearchDescription in the OpenSearch 1.1 namespace and a ShortName and a
 * Description, refuses a DOCTYPE and any entity but the five predefined, allows
 * 16 Url elements and 16 levels of nesting, and drops a Url that has no type or
 * template, whose template is not http or https with a host, or has no
 * {searchTerms}. Unknown elements and attributes are ignored.
 *
 * Parameters:
 *   r (io.Reader) — the document
 *
 * Returns:
 *   *Description — the description
 *   error        — wraps ErrBad
 *
 * Example:
 *   d, err := opensearch.Parse(strings.NewReader(xmlText))
 */
func Parse(r io.Reader) (*Description, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxDescription+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBad, err)
	}
	if len(raw) > MaxDescription {
		return nil, fmt.Errorf("%w: over %d bytes", ErrBad, MaxDescription)
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.Strict = true
	dec.Entity = nil
	d := &Description{}
	depth, urls := 0, 0
	rootSeen, rootClosed := false, false
	var text *string // the element whose text is being read
	var buf strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBad, err)
		}
		switch t := tok.(type) {
		case xml.Directive:
			return nil, fmt.Errorf("%w: a DOCTYPE or declaration is not allowed", ErrBad)
		case xml.StartElement:
			if rootClosed {
				return nil, fmt.Errorf("%w: more than one document", ErrBad)
			}
			depth++
			if depth > maxDepth {
				return nil, fmt.Errorf("%w: nested more than %d levels", ErrBad, maxDepth)
			}
			if depth == 1 {
				if t.Name.Space != nsURI || t.Name.Local != "OpenSearchDescription" {
					return nil, fmt.Errorf("%w: the root is not OpenSearchDescription in the OpenSearch 1.1 namespace", ErrBad)
				}
				rootSeen = true
				continue
			}
			if depth != 2 || t.Name.Space != nsURI {
				continue
			}
			switch t.Name.Local {
			case "ShortName":
				text = &d.ShortName
				buf.Reset()
			case "Description":
				text = &d.Description
				buf.Reset()
			case "Url":
				urls++
				if urls > maxUrls {
					return nil, fmt.Errorf("%w: more than %d Url elements", ErrBad, maxUrls)
				}
				var u Url
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "type":
						u.Type = strings.TrimSpace(a.Value)
					case "template":
						u.Template = strings.TrimSpace(a.Value)
					case "rel":
						u.Rel = strings.TrimSpace(a.Value)
					}
				}
				if usable(u) {
					d.Urls = append(d.Urls, u)
				}
			}
		case xml.CharData:
			if text != nil && depth == 2 {
				buf.Write(t)
			}
		case xml.EndElement:
			if depth == 2 && text != nil {
				*text = strings.TrimSpace(buf.String())
				text = nil
			}
			depth--
			if depth == 0 {
				rootClosed = true
			}
		}
	}
	if !rootSeen {
		return nil, fmt.Errorf("%w: no OpenSearchDescription element", ErrBad)
	}
	if d.ShortName == "" || d.Description == "" {
		return nil, fmt.Errorf("%w: ShortName and Description are required", ErrBad)
	}
	return d, nil
}

// usable reports whether a Url has a type, and a template that is an http or
// https address with a host and a {searchTerms}.
func usable(u Url) bool {
	if u.Type == "" || u.Template == "" || !strings.Contains(u.Template, "{searchTerms}") {
		return false
	}
	p, err := url.Parse(u.Template)
	if err != nil {
		return false
	}
	s := strings.ToLower(p.Scheme)
	return (s == "http" || s == "https") && p.Host != ""
}

/** Url returns the first Url of a media type that is a results URL: its rel is
 * absent or has the token "results". Parameters of a type (a charset) are
 * ignored in the comparison.
 *
 * Parameters:
 *   mediaType (string) — e.g. "text/markdown"
 *
 * Returns:
 *   Url  — the template
 *   bool — false when there is none
 *
 * Example:
 *   u, ok := d.Url("text/markdown")
 */
func (d *Description) Url(mediaType string) (Url, bool) {
	want, _, _ := mime.ParseMediaType(mediaType)
	for _, u := range d.Urls {
		got, _, _ := mime.ParseMediaType(u.Type)
		if got != want {
			continue
		}
		if u.Rel == "" {
			return u, true
		}
		for _, r := range strings.Fields(strings.ToLower(u.Rel)) {
			if r == "results" {
				return u, true
			}
		}
	}
	return Url{}, false
}

/** Origin returns the scheme, lower-cased host and port of the template, which
 * is where a query made with it goes.
 *
 * Returns:
 *   string — e.g. "http://127.0.0.1:8900"; "" when the template does not parse
 *
 * Example:
 *   fmt.Printf("queries go to %s\n", u.Origin())
 */
func (u Url) Origin() string {
	p, err := url.Parse(u.Template)
	if err != nil {
		return ""
	}
	return strings.ToLower(p.Scheme) + "://" + strings.ToLower(p.Host)
}

var param = regexp.MustCompile(`\{([^{}]*)\}`)

/** Expand fills a template. {searchTerms} becomes terms, trimmed and
 * percent-encoded for where it stands (a query value before the "?", a path
 * segment after); {count} and {startIndex} become the numbers when they are
 * above zero and nothing otherwise; inputEncoding, outputEncoding, language and
 * every other optional parameter ({x?}) become nothing; any other required
 * parameter is an error.
 *
 * Parameters:
 *   terms (string) — what the reader typed
 *   count (int)    — results wanted, 0 for the service's own number
 *   start (int)    — the first result wanted, 0 for the first
 *
 * Returns:
 *   string — the address to load
 *   error  — ErrUnknownParam, or ErrTooLong when the address is over 8,000 bytes
 *
 * Example:
 *   addr, err := u.Expand("otters & tea", 0, 0)
 */
func (u Url) Expand(terms string, count, start int) (string, error) {
	q := strings.IndexByte(u.Template, '?')
	var bad string
	out := param.ReplaceAllStringFunc(u.Template, func(m string) string {
		pos := strings.Index(u.Template, m)
		name := strings.TrimSuffix(m[1:len(m)-1], "?")
		optional := strings.HasSuffix(m, "?}")
		switch name {
		case "searchTerms":
			t := strings.TrimSpace(terms)
			if q >= 0 && pos > q {
				return url.QueryEscape(t)
			}
			return url.PathEscape(t)
		case "count":
			if count > 0 {
				return strconv.Itoa(count)
			}
			return ""
		case "startIndex":
			if start > 0 {
				return strconv.Itoa(start)
			}
			return ""
		case "inputEncoding", "outputEncoding", "language":
			return ""
		}
		if !optional && bad == "" {
			bad = name
		}
		return ""
	})
	if bad != "" {
		return "", fmt.Errorf("%w: {%s}", ErrUnknownParam, bad)
	}
	if len(out) > maxExpanded {
		return "", fmt.Errorf("%w: %d bytes", ErrTooLong, len(out))
	}
	p, err := url.Parse(out)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return "", fmt.Errorf("%w: expanded template is not an http or https address", ErrBad)
	}
	return out, nil
}

/** Policy says whose search service may be used (markdownweb DR-0005 item 6).
 * The zero value is the restricted policy.
 *
 * Fields:
 *   Origin    (string) — the origin of the page or site that offered the service, e.g. "http://127.0.0.1:8900"
 *   AnyOrigin (bool)   — false: the description and every template must be at Origin; true: any http or https address
 *
 * Example:
 *   p := opensearch.Policy{Origin: "https://example.org"}                  // the site's own search
 *   q := opensearch.Policy{Origin: "https://example.org", AnyOrigin: true} // a provider it names
 */
type Policy struct {
	Origin    string
	AnyOrigin bool
}

// origin returns the scheme, lower-cased host and port with the scheme's
// default port made explicit, of an address, or "".
func origin(s string) string {
	p, err := url.Parse(s)
	if err != nil || p.Host == "" {
		return ""
	}
	scheme := strings.ToLower(p.Scheme)
	port := p.Port()
	if port == "" {
		port = "80"
		if scheme == "https" {
			port = "443"
		}
	}
	return scheme + "://" + strings.ToLower(p.Hostname()) + ":" + port
}

/** Fetch gets a description through the polite fetcher, requires status 200 and
 * the description's media type, reads at most 64 KiB, parses it and applies the
 * policy. With a restricted policy the fetcher must have been built with
 * SameOrigin (a redirect off the site is then refused before it is followed),
 * the description's address and final address must be at Policy.Origin, and a
 * Url on another origin is dropped; nothing is requested from another origin.
 * With AnyOrigin any http or https address is allowed. Neither sends cookies or
 * a Referer.
 *
 * Parameters:
 *   ctx    (context.Context) — cancels the request
 *   f      (*fetch.Fetcher)  — the fetcher
 *   target (string)          — the description's address
 *   p      (Policy)          — whose service may be used
 *
 * Returns:
 *   *Description — the description, with only the Urls the policy allows
 *   error        — ErrBad (also for a fetcher a restricted policy cannot use), ErrCrossOrigin,
 *     ErrNoSearch, ErrNotFound (404 or 410), or a fetch error (ErrData, ErrUnavailable)
 *
 * Example:
 *   f := fetch.New(fetch.Options{SameOrigin: true})
 *   d, err := opensearch.Fetch(ctx, f, "http://127.0.0.1:8900/opensearch.xml", opensearch.Policy{Origin: "http://127.0.0.1:8900"})
 */
func Fetch(ctx context.Context, f *fetch.Fetcher, target string, p Policy) (*Description, error) {
	t, err := url.Parse(target)
	if err != nil || (strings.ToLower(t.Scheme) != "http" && strings.ToLower(t.Scheme) != "https") || t.Host == "" {
		return nil, fmt.Errorf("%w: %q is not an http or https address", ErrBad, target)
	}
	want := origin(p.Origin)
	if !p.AnyOrigin {
		if want == "" {
			return nil, fmt.Errorf("%w: a restricted policy needs the offering origin", ErrBad)
		}
		if !f.SameOrigin() {
			return nil, fmt.Errorf("%w: a restricted policy needs a fetcher built with SameOrigin", ErrBad)
		}
		if origin(target) != want {
			return nil, fmt.Errorf("%w: %s is not at %s", ErrCrossOrigin, target, p.Origin)
		}
	}
	resp, err := f.Open(ctx, target, DescriptionType)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 404 || resp.StatusCode == 410:
		return nil, fmt.Errorf("%w: %s: %s", ErrNotFound, target, resp.Status)
	case resp.StatusCode != 200:
		return nil, fmt.Errorf("%w: %s: %s", fetch.ErrData, target, resp.Status)
	}
	if !p.AnyOrigin && origin(resp.Request.URL.String()) != want {
		return nil, fmt.Errorf("%w: %s redirected to %s", ErrCrossOrigin, target, resp.Request.URL)
	}
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt != DescriptionType {
		return nil, fmt.Errorf("%w: %s is served as %q, not %s", ErrBad, target, resp.Header.Get("Content-Type"), DescriptionType)
	}
	d, err := Parse(resp.Body)
	if err != nil {
		return nil, err
	}
	if !p.AnyOrigin {
		keep := d.Urls[:0]
		for _, u := range d.Urls {
			if origin(u.Template) == want {
				keep = append(keep, u)
			}
		}
		d.Urls = keep
	}
	if len(d.Urls) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoSearch, target)
	}
	return d, nil
}
