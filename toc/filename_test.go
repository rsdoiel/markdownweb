package toc

import "testing"

func TestFileNameIsTheWellKnownPath(t *testing.T) {
	if FileName != ".well-known/toc.opml" {
		t.Errorf("FileName = %q", FileName)
	}
}
