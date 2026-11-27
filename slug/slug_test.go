package slug

import "testing"

func TestOf(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Soil", "soil"},
		{"Hello, World!", "hello-world"},
		{"Soil & Seeds", "soil--seeds"},
		{"  Padded  heading ", "padded--heading"},
		{"State-of-the-art", "state-of-the-art"},
		{"Ünïcode Ünder", "ünïcode-ünder"},
		{"Section 2.1", "section-21"},
		{"snake_case name", "snake_case-name"},
		{"!!!", ""},
		{"", ""},
	} {
		if got := Of(c.in); got != c.want {
			t.Errorf("Of(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSluggerRepeats(t *testing.T) {
	s := New()
	var got []string
	for _, h := range []string{"Notes", "Notes", "Soil", "Notes"} {
		got = append(got, s.Next(h))
	}
	want := []string{"notes", "notes-1", "soil", "notes-2"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
			break
		}
	}
}

func TestSluggerSkipsTakenSuffix(t *testing.T) {
	// a heading literally named "Notes 1" already holds notes-1
	s := New()
	got := []string{s.Next("Notes"), s.Next("Notes 1"), s.Next("Notes")}
	want := []string{"notes", "notes-1", "notes-2"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
			break
		}
	}
}

func TestSluggerEmptyIsNotRemembered(t *testing.T) {
	s := New()
	if a, b := s.Next("!!!"), s.Next("???"); a != "" || b != "" {
		t.Errorf("got %q %q, want empty", a, b)
	}
}

func TestSluggerReset(t *testing.T) {
	s := New()
	s.Next("A")
	s.Reset()
	if got := s.Next("A"); got != "a" {
		t.Errorf("after Reset got %q", got)
	}
}
