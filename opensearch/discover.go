package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"strings"

	"github.com/rsdoiel/markdownweb/fetch"
)

// The names of the places a service is found (Service.Source).
const (
	SourceLink          = "link"
	SourceCmarkfindFile = "cmarkfind.json"
	SourceBonbonFile    = "bonbon.json"
)

// maxFile caps one well-known file, as cmarkfind's own reader does.
const maxFile = 64 << 10

/** Service is a search service a client may offer the reader.
 *
 * Fields:
 *   Title       (string) — the name the offering link gave, at most 128 bytes, no control characters; "" when it gave none
 *   Source      (string) — SourceLink, SourceCmarkfindFile or SourceBonbonFile
 *   Description (string) — the address of the OpenSearch description; "" for a bonbon.json service, which has none
 *   Url         (Url)    — the search template, set only for a bonbon.json service (type text/markdown)
 *   CrossOrigin (bool)   — the service is on another origin than the offering site's
 *
 * Example:
 *   svc := opensearch.Service{Source: opensearch.SourceLink, Description: "http://127.0.0.1:8900/opensearch.xml", Title: "cmarkfind"}
 */
type Service struct {
	Title       string
	Source      string
	Description string
	Url         Url
	CrossOrigin bool
}

// key identifies a service for removing duplicates.
func (s Service) key() string {
	if s.Description != "" {
		return s.Description
	}
	return s.Url.Template
}

// admit applies the policy to a service whose search is at addr: false when a
// restricted policy refuses it, and CrossOrigin set when it is elsewhere.
func admit(s *Service, addr string, want string, p Policy) bool {
	s.CrossOrigin = origin(addr) != want
	return !s.CrossOrigin || p.AnyOrigin
}

/** FromLinks returns the services a page's Link headers offer: links whose rel
 * has "search" and whose type is the OpenSearch description type. It makes no
 * request. A restricted policy drops a service on another origin; AnyOrigin
 * keeps it, marked CrossOrigin.
 *
 * Parameters:
 *   links ([]fetch.Link) — the page's links (fetch.Result.Links, page.Page.Links)
 *   p     (Policy)       — whose services may be used
 *
 * Returns:
 *   []Service — in the order the links came, nil when there are none
 *
 * Example:
 *   svcs := opensearch.FromLinks(p.Links, opensearch.Policy{Origin: "http://127.0.0.1:8900"})
 */
func FromLinks(links []fetch.Link, p Policy) []Service {
	want := origin(p.Origin)
	var out []Service
	seen := map[string]bool{}
	for _, l := range links {
		if mt, _, _ := mime.ParseMediaType(l.Type); mt != DescriptionType || !hasRel(l.Rel, "search") || seen[l.Target] {
			continue
		}
		s := Service{Title: l.Title, Source: SourceLink, Description: l.Target}
		if !admit(&s, l.Target, want, p) {
			continue
		}
		seen[l.Target] = true
		out = append(out, s)
	}
	return out
}

func hasRel(rels []string, want string) bool {
	for _, r := range rels {
		if r == want {
			return true
		}
	}
	return false
}

/** Discover returns the services a site offers, in order of preference and
 * without duplicates: those of the page's Link headers (FromLinks), then the
 * "search" key of /.well-known/cmarkfind.json, then the "urlSearch" key of
 * /.well-known/bonbon.json. The two files are read from the site root, once
 * each, through the fetcher, at most 64 KiB; a missing file (404 or 410) is not
 * an error, and a file that is bad or unreachable is reported in the error
 * while the services found elsewhere are still returned. Discover never
 * requests a description; a client does that with Fetch when the reader
 * searches. With a restricted policy the fetcher must have been built with
 * SameOrigin and the site must be at Policy.Origin, or nothing is requested.
 *
 * Parameters:
 *   ctx   (context.Context) — cancels the requests
 *   f     (*fetch.Fetcher)  — the fetcher
 *   site  (string)          — the address of the page that was read; its scheme and host are the site root
 *   links ([]fetch.Link)    — that page's links, may be nil
 *   p     (Policy)          — whose services may be used
 *
 * Returns:
 *   []Service — the services, the first being the default
 *   error     — ErrBad (a fetcher or address a restricted policy cannot use, or a malformed file),
 *     ErrCrossOrigin, or a fetch error; joined when more than one file failed
 *
 * Example:
 *   svcs, err := opensearch.Discover(ctx, f, "http://127.0.0.1:8900/search?q=x", page.Links,
 *       opensearch.Policy{Origin: "http://127.0.0.1:8900"})
 */
func Discover(ctx context.Context, f *fetch.Fetcher, site string, links []fetch.Link, p Policy) ([]Service, error) {
	u, err := url.Parse(site)
	if err != nil || (strings.ToLower(u.Scheme) != "http" && strings.ToLower(u.Scheme) != "https") || u.Host == "" {
		return nil, fmt.Errorf("%w: %q is not an http or https address", ErrBad, site)
	}
	root := strings.ToLower(u.Scheme) + "://" + u.Host
	want := origin(p.Origin)
	if p.AnyOrigin && want == "" {
		want = origin(root)
	}
	if !p.AnyOrigin {
		if want == "" {
			return nil, fmt.Errorf("%w: a restricted policy needs the offering origin", ErrBad)
		}
		if !f.SameOrigin() {
			return nil, fmt.Errorf("%w: a restricted policy needs a fetcher built with SameOrigin", ErrBad)
		}
		if origin(root) != want {
			return nil, fmt.Errorf("%w: %s is not at %s", ErrCrossOrigin, root, p.Origin)
		}
	}
	out := FromLinks(links, p)
	seen := map[string]bool{}
	for _, s := range out {
		seen[s.key()] = true
	}
	add := func(s Service) {
		if !seen[s.key()] {
			seen[s.key()] = true
			out = append(out, s)
		}
	}
	var errs []error

	if body, err := readFile(ctx, f, root, "/.well-known/cmarkfind.json"); err != nil {
		errs = append(errs, err)
	} else if body != nil {
		val, err := searchKey(body)
		if err != nil {
			errs = append(errs, fmt.Errorf("%w (%s/.well-known/cmarkfind.json)", err, root))
		} else if addr, ok := resolveAddress(root, val); ok {
			s := Service{Source: SourceCmarkfindFile, Description: addr}
			if admit(&s, addr, want, p) {
				add(s)
			}
		}
	}

	if body, err := readFile(ctx, f, root, "/.well-known/bonbon.json"); err != nil {
		errs = append(errs, err)
	} else if body != nil {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(body, &m); err != nil {
			errs = append(errs, fmt.Errorf("%w: bonbon.json is not a JSON object (%s/.well-known/bonbon.json)", ErrBad, root))
		} else {
			var val string
			if json.Unmarshal(m["urlSearch"], &val) == nil {
				if addr, ok := resolveAddress(root, val); ok && !strings.Contains(addr, "#") {
					sep := "?"
					if strings.Contains(addr, "?") {
						sep = "&"
					}
					s := Service{Source: SourceBonbonFile, Url: Url{Type: "text/markdown", Template: addr + sep + "q={searchTerms}"}}
					if usable(s.Url) && admit(&s, s.Url.Template, want, p) {
						add(s)
					}
				}
			}
		}
	}
	return out, errors.Join(errs...)
}

// readFile reads root+path: nil and no error when the site has no such file,
// an error for any other failure, including an answer from another origin.
func readFile(ctx context.Context, f *fetch.Fetcher, root, path string) ([]byte, error) {
	resp, err := f.Open(ctx, root+path, "application/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 404 || resp.StatusCode == 410:
		return nil, nil
	case resp.StatusCode != 200:
		return nil, fmt.Errorf("%w: %s%s answered HTTP %d", fetch.ErrUnavailable, root, path, resp.StatusCode)
	}
	if origin(resp.Request.URL.String()) != origin(root) {
		return nil, fmt.Errorf("%w: %s%s was served from %s", ErrCrossOrigin, root, path, resp.Request.URL.Host)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxFile+1))
	if err != nil {
		return nil, fmt.Errorf("%w: reading %s%s: %v", fetch.ErrUnavailable, root, path, err)
	}
	if len(b) > maxFile {
		return nil, fmt.Errorf("%w: %s%s is over %d bytes", ErrBad, root, path, maxFile)
	}
	return b, nil
}

// searchKey reads the "search" string of a cmarkfind.json: an object with no
// repeated key (cmarkfind's rule), whose other keys are ignored. "" when the
// key is absent or is not a string.
func searchKey(b []byte) (string, error) {
	bad := func(why string) (string, error) {
		return "", fmt.Errorf("%w: cmarkfind.json %s", ErrBad, why)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return bad("is not a JSON object")
	}
	seen := map[string]bool{}
	val := ""
	for dec.More() {
		t, err := dec.Token()
		key, ok := t.(string)
		if err != nil || !ok {
			return bad("is not valid JSON")
		}
		if seen[key] {
			return bad(fmt.Sprintf("repeats the key %q", key))
		}
		seen[key] = true
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return bad("is not valid JSON")
		}
		if key == "search" {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				val = s
			}
		}
	}
	if _, err := dec.Token(); err != nil {
		return bad("is not valid JSON")
	}
	if _, err := dec.Token(); err != io.EOF {
		return bad("has more than one value")
	}
	return val, nil
}

// resolveAddress makes the address a site file names: an absolute path on the
// site root or an http or https address with a host, with no control character
// or backslash. ok is false for anything else, which a reader ignores.
func resolveAddress(root, v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	for _, r := range v {
		if isControl(r) || r == '\\' || r == ' ' {
			return "", false
		}
	}
	if strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "//") {
		v = root + v
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || (strings.ToLower(u.Scheme) != "http" && strings.ToLower(u.Scheme) != "https") {
		return "", false
	}
	return v, true
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) }
