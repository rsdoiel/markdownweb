package toc

import (
	"context"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/rsdoiel/markdownweb/debuglog"
	"github.com/rsdoiel/markdownweb/local"
)

/** LookupLocal is Lookup for a page in a local site (cmarkbrowser DR-0001): the
 * table of contents is read from the files under the site root, nothing is
 * fetched from the network. The site root's .well-known/toc.opml is read first,
 * then the ones in the folders down to the page, nearest first, with the
 * outlines it includes merged under the same limits as Lookup. An outline
 * lists the published site's addresses, so an entry matches the page when its
 * path ends with the page's path below the root, whatever its scheme and host;
 * the neighbours come back as absolute paths of files that exist inside the
 * root, and one that does not is dropped.
 *
 * Parameters:
 *   ctx (context.Context) — cancellation and the debug logger
 *   root (string) — the site root, absolute
 *   pagePath (string) — the absolute path of the page being shown
 *
 * Returns:
 *   Result — Prev and Next are paths, TOC is the outline's path
 *
 * Example:
 *   r := finder.LookupLocal(ctx, "/site", "/site/blog/a.md")
 */
func (f *Finder) LookupLocal(ctx context.Context, root, pagePath string) Result {
	log := debuglog.FromContext(ctx)
	site := &local.Site{Root: root}
	relPage, err := filepath.Rel(root, pagePath)
	if err != nil || strings.HasPrefix(relPage, "..") {
		return Result{}
	}
	rel := filepath.ToSlash(relPage)
	cands := []string{filepath.Join(root, filepath.FromSlash(FileName))}
	for dir := filepath.Dir(pagePath); dir != root && len(cands) < maxProbes && strings.HasPrefix(dir, root); dir = filepath.Dir(dir) {
		cands = append(cands, filepath.Join(dir, filepath.FromSlash(FileName)))
	}
	var res Result
	for _, c := range cands {
		budget := maxFetches
		doc := f.getLocal(site, c, map[string]bool{}, 0, &budget)
		if doc == nil {
			continue
		}
		if prev, next, ok := doc.NeighboursByPath(rel); ok {
			res = Result{Prev: toLocal(site, prev), Next: toLocal(site, next), TOC: c, Found: true}
			break
		}
	}
	log.Event("toc.lookup", "local", true, "page", pagePath, "toc", res.TOC, "found", res.Found, "prev", res.Prev, "next", res.Next)
	return res
}

// toLocal maps an address from an outline to a file under the root, "" when
// there is none.
func toLocal(site *local.Site, address string) string {
	if address == "" {
		return ""
	}
	p, ok := mapToRoot(site, address)
	if !ok {
		return ""
	}
	return p
}

// mapToRoot finds the file under the site root that an outline address names:
// the longest suffix of its path that is a file inside the root.
func mapToRoot(site *local.Site, address string) (string, bool) {
	u, err := url.Parse(address)
	if err != nil {
		return "", false
	}
	p := path.Clean("/" + u.Path)
	if strings.HasSuffix(u.Path, "/") || p == "/" {
		p = path.Join(p, "index.md")
	}
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i := range segs {
		cand := filepath.Join(site.Root, filepath.FromSlash(strings.Join(segs[i:], "/")))
		info, err := os.Stat(cand)
		if err != nil || info.IsDir() {
			continue
		}
		if real, err := site.Inside(cand); err == nil {
			return real, true
		}
	}
	return "", false
}

// getLocal reads one outline file and merges its includes; nil for any
// failure. Everything read is cached, failures too.
func (f *Finder) getLocal(site *local.Site, file string, inProgress map[string]bool, depth int, budget *int) *Doc {
	key := "local:" + file
	f.mu.Lock()
	if c, ok := f.cache[key]; ok {
		f.mu.Unlock()
		return c.doc
	}
	f.mu.Unlock()
	if inProgress[key] || *budget <= 0 {
		return nil
	}
	*budget--
	inProgress[key] = true
	defer delete(inProgress, key)

	doc := readLocalOutline(site, file)
	if doc != nil && depth < maxIncludeDepth {
		for _, inc := range doc.Includes() {
			target, ok := mapToRoot(site, inc)
			if !ok {
				continue
			}
			if child := f.getLocal(site, target, inProgress, depth+1, budget); child != nil {
				doc.Splice(inc, child)
			}
		}
	}
	f.mu.Lock()
	f.cache[key] = &fetched{doc: doc}
	f.mu.Unlock()
	return doc
}

// readLocalOutline reads and parses one outline file inside the root.
func readLocalOutline(site *local.Site, file string) *Doc {
	real, err := site.Inside(file)
	if err != nil {
		return nil
	}
	info, err := os.Stat(real)
	if err != nil || info.IsDir() || info.Size() > maxTOC {
		return nil
	}
	body, err := os.ReadFile(real)
	if err != nil {
		return nil
	}
	rel, _ := filepath.Rel(site.Root, real)
	doc, err := Parse(body, "http://site.local/"+filepath.ToSlash(rel))
	if err != nil {
		return nil
	}
	return doc
}
