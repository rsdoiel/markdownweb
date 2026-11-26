package toc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rsdoiel/markdownweb/debuglog"
)

// maxSettings caps one bonbon.json.
const maxSettings = 256 << 10

/** Mirror says that the Markdown pages under Folder are the copy of the HTML
 * site at Original, as a site's bonbon.json says with urlMirrorOf.
 *
 * Example:
 *   m := toc.Mirror{Original: "http://scripting.com/", Folder: "https://example.org/md/"}
 *   md := m.Translate("http://scripting.com/2026/11/19.html")
 */
type Mirror struct {
	Original string // urlMirrorOf: the HTML site, e.g. "http://scripting.com/"
	Folder   string // the Markdown site's folder, with a trailing slash
}

/** Translate returns the address in the Markdown mirror of a link that points
 * into the HTML site m mirrors, by BonBon's rule: the same path with .html
 * changed to .md, the site's home page (or index.html) to the folder, a
 * directory to the same directory in the mirror, the fragment kept. It
 * returns "" for a link outside the site or one with no Markdown counterpart
 * (an image, a feed). The host is compared without a leading "www.".
 *
 * Parameters:
 *   link (string) — an absolute address
 *
 * Returns:
 *   string — the address in the mirror, or ""
 *
 * Example:
 *   m := toc.Mirror{Original: "http://scripting.com/", Folder: "https://dave.writersweb.org/scripting/"}
 *   m.Translate("http://scripting.com/2026/11/05.html#a") // "https://dave.writersweb.org/scripting/2026/11/05.md#a"
 */
func (m Mirror) Translate(link string) string {
	orig, err1 := url.Parse(m.Original)
	l, err2 := url.Parse(link)
	if err1 != nil || err2 != nil || orig.Host == "" || l.Host == "" {
		return ""
	}
	if orig.Path == "" {
		orig.Path = "/"
	}
	if l.Path == "" {
		l.Path = "/"
	}
	bare := func(h string) string { return strings.TrimPrefix(strings.ToLower(h), "www.") }
	if bare(l.Hostname()) != bare(orig.Hostname()) || !strings.HasPrefix(l.Path, orig.Path) {
		return ""
	}
	rest := strings.TrimPrefix(l.Path, orig.Path)
	rest = strings.TrimPrefix(rest, "/") // BonBon's roots end in a slash; tolerate one that does not
	var out string
	switch {
	case rest == "" || rest == "index.html":
		out = m.Folder
	case strings.HasSuffix(rest, ".html"):
		out = m.Folder + strings.TrimSuffix(rest, ".html") + ".md"
	case strings.HasSuffix(rest, "/"):
		out = m.Folder + rest
	default:
		return ""
	}
	if l.Fragment != "" {
		out += "#" + l.Fragment
	}
	return out
}

/** Mirrors returns the mirrors the site of page declares. BonBon's rule: only
 * after the origin root's /.well-known/toc.opml answered is bonbon.json read,
 * at the origin root and in the folder of each include outline of that table
 * of contents (same host only), and only the string urlMirrorOf counts. A site
 * with no table of contents, or no bonbon.json, has none. Answers, misses
 * included, are remembered for the session.
 *
 * Parameters:
 *   ctx (context.Context) — cancellation and the debug logger
 *   page (string) — the address of the page being shown
 *
 * Returns:
 *   []Mirror — the root's mirror first, then each included folder's
 *
 * Example:
 *   for _, m := range finder.Mirrors(ctx, view.Page.URL) { dest := m.Translate(link) }
 */
func (f *Finder) Mirrors(ctx context.Context, page string) []Mirror {
	log := debuglog.FromContext(ctx)
	cands := candidates(page)
	if len(cands) == 0 {
		return nil
	}
	root := cands[0]
	budget := maxFetches
	doc := f.get(ctx, root, map[string]bool{}, 0, &budget)
	if doc == nil {
		return nil
	}
	base, _ := url.Parse(root)
	folders := []string{base.Scheme + "://" + base.Host + "/"}
	f.mu.Lock()
	incs := f.cache[root].incs
	f.mu.Unlock()
	for _, inc := range incs {
		u, err := url.Parse(inc)
		i := strings.LastIndex(inc, "/.well-known/")
		if err != nil || u.Host != base.Host || u.Scheme != base.Scheme || i < 0 {
			continue
		}
		folders = append(folders, inc[:i+1])
	}
	var out []Mirror
	seen := map[string]bool{}
	for _, folder := range folders {
		if seen[folder] {
			continue
		}
		seen[folder] = true
		if orig := f.mirrorOf(ctx, folder+".well-known/bonbon.json"); orig != "" {
			out = append(out, Mirror{Original: orig, Folder: folder})
		}
	}
	log.Event("mirror.lookup", "page", page, "mirrors", len(out))
	return out
}

// mirrorOf reads one bonbon.json and returns its urlMirrorOf, or "" for any
// failure or a value that is not a string.
func (f *Finder) mirrorOf(ctx context.Context, address string) string {
	f.mu.Lock()
	if v, ok := f.mirrors[address]; ok {
		f.mu.Unlock()
		return v
	}
	f.mu.Unlock()
	log := debuglog.FromContext(ctx)
	start := time.Now()
	status, result := 0, ""
	var err error
	defer func() {
		f.mu.Lock()
		f.mirrors[address] = result
		f.mu.Unlock()
		log.Event("mirror.fetch", "url", address, "status", status, "mirror_of", result, "ms", time.Since(start), "err", err)
	}()
	res, e := f.fx().Get(ctx, address, "application/json")
	if e != nil {
		err = e
		return ""
	}
	status = res.Status
	if status != http.StatusOK || len(res.Body) > maxSettings {
		return ""
	}
	body := res.Body
	var settings map[string]any
	if err = json.Unmarshal(body, &settings); err != nil {
		return ""
	}
	if s, ok := settings["urlMirrorOf"].(string); ok {
		result = strings.TrimSpace(s)
	}
	return result
}
