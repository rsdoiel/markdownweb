// Package local is browsing a Markdown site straight from a folder, with no
// server and no protocol (cmarkbrowser DR-0001). The person gives a path; the
// folder the site lives in is the site root, and a page in it reaches only
// files inside that root. A page from the network never gets a site root.
package local

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/rsdoiel/markdownweb/page"
)

// ErrOutside marks a path that is not inside the site root, after ".." is
// resolved and symlinks are evaluated.
var ErrOutside = errors.New("outside the site root")

// ErrScheme marks a link whose scheme a local page may not follow: only http
// and https go to the network, and a file: address is never followed.
var ErrScheme = errors.New("an address with this scheme is not followed")

/** Site is a folder browsed as a Markdown site. Make one with Open.
 *
 * Fields:
 *   Root (string) — the site root, absolute, symlinks evaluated
 *
 * Example:
 *   site, start, err := local.Open("./mysite")
 *   fmt.Println(site.Root, start)
 */
type Site struct {
	Root string
}

/** Open turns the path given on the command line into a site and the file to
 * start from. A directory is the site root and starts at its index.md; a file
 * starts at itself, and its site root is the nearest folder at or above it that
 * holds .well-known/toc.opml, else the folder the file is in. A file: address
 * is not a path and is a usage error for the caller to report.
 *
 * Parameters:
 *   arg (string) — a path, absolute or relative to the working directory
 *
 * Returns:
 *   *Site — the site root
 *   string — the absolute path of the page to show first
 *   error — wraps page.ErrNoInput (missing path, or a directory with no
 *     index.md) or, for a permission failure, fs.ErrPermission as well
 *
 * Example:
 *   site, start, err := local.Open("./") // the current directory, at ./index.md
 */
func Open(arg string) (*Site, string, error) {
	abs, err := filepath.Abs(arg)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", page.ErrNoInput, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", page.ErrNoInput, err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", page.ErrNoInput, err)
	}
	if info.IsDir() {
		start := filepath.Join(real, "index.md")
		if _, err := os.Stat(start); errors.Is(err, fs.ErrNotExist) {
			return nil, "", fmt.Errorf("%w: %s has no index.md to start from", page.ErrNoInput, real)
		} else if err != nil {
			return nil, "", fmt.Errorf("%w: %w", page.ErrNoInput, err)
		}
		return &Site{Root: real}, start, nil
	}
	root := filepath.Dir(real)
	for dir := root; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".well-known", "toc.opml")); err == nil {
			root = dir
			break
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return &Site{Root: root}, real, nil
}

/** Inside reports the real path of p when it lies inside the site root. ".." is
 * resolved and symlinks are evaluated, for the part of p that exists and for
 * the nearest folder that does when p itself is missing, so neither a "../.."
 * nor a symlink in the site can lead out of it. The comparison is on whole path
 * elements, never on a text prefix.
 *
 * Parameters:
 *   p (string) — an absolute path
 *
 * Returns:
 *   string — the real path, when inside
 *   error — ErrOutside, wrapped with the path
 *
 * Example:
 *   real, err := site.Inside("/site/blog/../../etc/passwd") // "", ErrOutside
 */
func (s *Site) Inside(p string) (string, error) {
	real := realPath(filepath.Clean(p))
	rel, err := filepath.Rel(s.Root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: %s", ErrOutside, p)
	}
	return real, nil
}

// realPath evaluates symlinks in the longest part of p that exists and appends
// the rest, which is already clean.
func realPath(p string) string {
	rest := ""
	for cur := p; ; {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

/** Link is where a link in a local page goes.
 *
 * Fields:
 *   Path (string) — for a path link, the absolute path of the file; "" for a web link
 *   URL (string) — for a web link, the address, "" for a path link
 *
 * Example:
 *   link, err := site.ResolveLink(pagePath, "../about.html")
 *   if err == nil && link.Path != "" { fmt.Println(link.Path) }
 */
type Link struct {
	Path string
	URL  string
}

/** ResolveLink says where a link written in the page at pagePath goes. A link
 * with no scheme is a path: relative to the page's folder, or, starting with
 * "/", relative to the site root (as on the web, not the file system root). A
 * link to an HTML page goes to its .md copy when there is one (a generated site
 * has both; this browser reads Markdown), and a link to a folder to its
 * index.md (the same rule page.Fetch uses on the web). An
 * http or https link is a web link, left as it is. Any other scheme,
 * file: included, is refused with ErrScheme. Whether a path is inside the
 * site root is Inside's question, asked when the link is followed.
 *
 * Parameters:
 *   pagePath (string) — the absolute path of the page the link is in
 *   dest (string) — the link destination as written
 *
 * Returns:
 *   Link — the path, or the web address
 *   error — ErrScheme, or a destination that does not parse
 *
 * Example:
 *   l, err := site.ResolveLink("/site/blog/a.md", "../b.html#top") // Path "/site/b.md" when b.html is absent
 */
func (s *Site) ResolveLink(pagePath, dest string) (Link, error) {
	u, err := url.Parse(strings.TrimSpace(dest))
	if err != nil {
		return Link{}, fmt.Errorf("%s: %w", dest, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		u.Fragment = ""
		return Link{URL: u.String()}, nil
	case "":
	default:
		return Link{}, fmt.Errorf("%w: %s", ErrScheme, dest)
	}
	if u.Host != "" { // //host/path: a web address with no scheme, not a path
		return Link{}, fmt.Errorf("%w: %s", ErrScheme, dest)
	}
	p := u.Path
	if p == "" {
		return Link{Path: pagePath}, nil // "#top": the page itself
	}
	var target string
	if strings.HasPrefix(p, "/") {
		target = filepath.Join(s.Root, filepath.FromSlash(path.Clean(p)))
	} else {
		target = filepath.Join(filepath.Dir(pagePath), filepath.FromSlash(p))
	}
	if strings.HasSuffix(p, "/") {
		target = filepath.Join(target, "index.md")
	} else if info, err := os.Stat(target); err == nil && info.IsDir() {
		target = filepath.Join(target, "index.md")
	} else if ext := strings.ToLower(filepath.Ext(target)); ext == ".html" || ext == ".htm" {
		// A generated site has both x.html and x.md; this browser reads Markdown,
		// so the .md copy wins, as the web rule tries it when the server sends HTML.
		if sib := page.MarkdownSibling(Address(target)); sib != "" {
			if sp, ok := PathOf(sib); ok {
				if _, err := os.Stat(sp); err == nil {
					target = sp
				}
			}
		}
	}
	return Link{Path: target}, nil
}

/** Address is the file: form of a local path, which the browser keeps inside as
 * a page's identity. It is never shown to the person, typed, or followed from
 * a link.
 *
 * Example:
 *   local.Address("/site/a.md") // "file:///site/a.md"
 */
func Address(p string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(p)}).String()
}

/** PathOf is the path of an address made by Address, and whether it was one.
 *
 * Example:
 *   p, ok := local.PathOf("file:///site/a.md") // "/site/a.md", true
 */
func PathOf(addr string) (string, bool) {
	u, err := url.Parse(addr)
	if err != nil || !strings.EqualFold(u.Scheme, "file") {
		return "", false
	}
	return filepath.FromSlash(u.Path), true
}
