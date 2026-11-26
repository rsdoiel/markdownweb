package local

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/rsdoiel/markdownweb/page"
)

// tree makes files under a temp folder and returns its real path.
func tree(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# "+f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func TestADirectoryIsTheSiteRootAndStartsAtItsIndex(t *testing.T) {
	root := tree(t, "index.md", "a.md")
	s, start, err := Open(root)
	if err != nil || s.Root != root || start != filepath.Join(root, "index.md") {
		t.Fatalf("%+v %q %v", s, start, err)
	}
	t.Chdir(root)
	if s, start, err = Open("./"); err != nil || s.Root != root || start != filepath.Join(root, "index.md") {
		t.Errorf("./ gave %+v %q %v", s, start, err)
	}
}

func TestADirectoryWithNoIndexIsNoInput(t *testing.T) {
	root := tree(t, "a.md")
	if _, _, err := Open(root); !errors.Is(err, page.ErrNoInput) {
		t.Errorf("%v", err)
	}
	if _, _, err := Open(filepath.Join(root, "nope")); !errors.Is(err, page.ErrNoInput) {
		t.Errorf("a missing path: %v", err)
	}
}

func TestAFilesSiteRootIsTheNearestFolderWithATableOfContents(t *testing.T) {
	root := tree(t, ".well-known/toc.opml", "blog/2026/a.md", "other/b.md")
	s, start, err := Open(filepath.Join(root, "blog", "2026", "a.md"))
	if err != nil || s.Root != root || start != filepath.Join(root, "blog", "2026", "a.md") {
		t.Errorf("%+v %q %v", s, start, err)
	}
	plain := tree(t, "x/y/c.md")
	if s, _, err = Open(filepath.Join(plain, "x", "y", "c.md")); err != nil || s.Root != filepath.Join(plain, "x", "y") {
		t.Errorf("with no toc the root is the file's folder: %+v %v", s, err)
	}
}

func TestAnUnreadableStartKeepsItsPermissionClass(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	root := tree(t, "index.md")
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o755)
	_, _, err := Open(root)
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("%v", err)
	}
}

func TestInsideRefusesWhatLeavesTheRoot(t *testing.T) {
	root := tree(t, "a.md", "blog/b.md")
	outside := tree(t, "secret.md")
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skip("no symlinks here: " + err.Error())
	}
	if err := os.Symlink(filepath.Join(root, "blog"), filepath.Join(root, "inner")); err != nil {
		t.Fatal(err)
	}
	s := &Site{Root: root}
	for _, ok := range []string{"a.md", "blog/b.md", "blog/../a.md", "inner/b.md", "blog/missing.md", "new/dir/page.md"} {
		if _, err := s.Inside(filepath.Join(root, ok)); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"../a.md", "blog/../../x.md", "escape/secret.md", "escape", "../" + filepath.Base(root) + "-sibling/a.md"} {
		if _, err := s.Inside(filepath.Join(root, bad)); !errors.Is(err, ErrOutside) {
			t.Errorf("%s allowed (%v)", bad, err)
		}
	}
	if _, err := s.Inside("/etc/passwd"); !errors.Is(err, ErrOutside) {
		t.Errorf("/etc/passwd: %v", err)
	}
}

func TestLinksResolveAsPathsAndSlashMeansTheSiteRoot(t *testing.T) {
	root := tree(t, "index.md", "blog/a.md", "blog/b.md", "blog/2026/c.md", "about.md")
	s := &Site{Root: root}
	page := filepath.Join(root, "blog", "a.md")
	for dest, want := range map[string]string{
		"b.md":          "blog/b.md",
		"b.md#top":      "blog/b.md",
		"2026/c.md":     "blog/2026/c.md",
		"../about.md":   "about.md",
		"/about.md":     "about.md", // the site root, not the file system root
		"/":             "index.md",
		"2026/":         "blog/2026/index.md",
		"../":           "index.md",
		"../about.html": "about.md", // the web rule: no about.html, so its .md copy
		"#top":          "blog/a.md",
		"missing.md":    "blog/missing.md",
		"/blog":         "blog/index.md",
	} {
		l, err := s.ResolveLink(page, dest)
		if err != nil || l.URL != "" || l.Path != filepath.Join(root, filepath.FromSlash(want)) {
			t.Errorf("%q gave %+v, %v; want %s", dest, l, err, want)
		}
	}
}

func TestAnHTMLLinkGoesToItsMarkdownCopyEvenWhenTheHTMLExists(t *testing.T) {
	root := tree(t, "a.md", "a.html", "only.html")
	s := &Site{Root: root}
	l, err := s.ResolveLink(filepath.Join(root, "a.md"), "a.html")
	if err != nil || l.Path != filepath.Join(root, "a.md") {
		t.Errorf("a generated site has both; the Markdown wins: %+v %v", l, err)
	}
	// with no Markdown copy the link stays as written
	if l, _ = s.ResolveLink(filepath.Join(root, "a.md"), "only.html"); l.Path != filepath.Join(root, "only.html") {
		t.Errorf("%+v", l)
	}
}

func TestWebLinksStayWebAndOtherSchemesAreRefused(t *testing.T) {
	root := tree(t, "a.md")
	s := &Site{Root: root}
	page := filepath.Join(root, "a.md")
	l, err := s.ResolveLink(page, "https://x.org/b.md#frag")
	if err != nil || l.URL != "https://x.org/b.md" || l.Path != "" {
		t.Errorf("%+v %v", l, err)
	}
	for _, bad := range []string{"file:///etc/passwd", "FILE:///etc/passwd", "file:b.md", "javascript:alert(1)", "mailto:a@b.c", "//evil.example/x.md", "ftp://x.org/a"} {
		if _, err := s.ResolveLink(page, bad); !errors.Is(err, ErrScheme) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestAddressRoundTrips(t *testing.T) {
	p := filepath.Join(string(filepath.Separator), "site", "my notes", "a.md")
	got, ok := PathOf(Address(p))
	if !ok || got != p {
		t.Errorf("%q %v", got, ok)
	}
	if _, ok := PathOf("https://x.org/a"); ok {
		t.Error("a web address is not a path")
	}
}
