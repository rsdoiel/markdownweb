package page

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileParsesMarkdownAndFrontMatter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	os.WriteFile(path, []byte("---\ntitle: My Notes\n---\n# Heading\n\n[a link](https://example.org/x)\n"), 0o644)
	p, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title() != "My Notes" || !strings.Contains(p.Body, "# Heading") {
		t.Errorf("page %+v", p)
	}
	if want := "file://" + path; p.URL != want {
		t.Errorf("URL %q, want %q", p.URL, want)
	}
	if p.Reader {
		t.Error("a file is not a reader view")
	}
}

func TestReadFileClassifiesFailures(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadFile(filepath.Join(dir, "missing.md")); !errors.Is(err, ErrNoInput) {
		t.Errorf("missing: %v, want ErrNoInput", err)
	}
	if _, err := ReadFile(dir); !errors.Is(err, ErrData) {
		t.Errorf("a directory: %v, want ErrData", err)
	}
	bin := filepath.Join(dir, "bin.dat")
	os.WriteFile(bin, []byte("abc\x00def"), 0o644)
	if _, err := ReadFile(bin); !errors.Is(err, ErrData) {
		t.Errorf("a binary file: %v, want ErrData", err)
	}
	bad := filepath.Join(dir, "bad.md")
	os.WriteFile(bad, []byte("---\ntitle: [unclosed\n---\nx\n"), 0o644)
	if _, err := ReadFile(bad); !errors.Is(err, ErrData) {
		t.Errorf("bad front matter: %v, want ErrData", err)
	}
}

func TestReadFileCapsTheSize(t *testing.T) {
	big := filepath.Join(t.TempDir(), "big.md")
	os.WriteFile(big, []byte(strings.Repeat("x", maxBody+10)), 0o644)
	if _, err := ReadFile(big); !errors.Is(err, ErrData) {
		t.Errorf("err %v, want ErrData", err)
	}
}

// Fetch is for the network. A file address is refused there, so a link in a
// remote page can never make the browser read a local file.
func TestFetchRefusesFileAddresses(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "secret.md")
	os.WriteFile(secret, []byte("TOP SECRET"), 0o644)
	for _, addr := range []string{"file://" + secret, "file:///etc/hostname", secret} {
		p, err := Fetch(context.Background(), addr)
		if err == nil || p != nil {
			t.Errorf("%s: Fetch read a local file: %v %v", addr, p, err)
		}
		if err != nil && strings.Contains(err.Error(), "SECRET") {
			t.Errorf("%s: the error leaks file content", addr)
		}
	}
	if _, err := FetchReader(context.Background(), "file://"+secret); err == nil {
		t.Error("FetchReader read a local file")
	}
}
