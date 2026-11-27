// Package slug is the fragment identifier of a Markdown heading: the anchor
// a link such as page.html#soil-and-seeds points at. The rule is GitHub's,
// which most Markdown renderers follow. It lets the readers (cmarkbrowser,
// cmarkfind) and the writers (antennaApp) agree on where a link lands.
package slug

import (
	"strconv"
	"strings"
	"unicode"
)

/** Of returns the slug of one heading, with no regard to other headings on
 * the page. The rule: lower case; letters, marks, digits, `-` and `_` kept;
 * each space becomes `-`; every other character dropped. Leading and trailing
 * white space is trimmed first. A heading with nothing to keep gives "".
 *
 * Parameters:
 *   heading (string) — the heading's plain text, markup already removed
 *
 * Returns:
 *   string — the slug, possibly empty
 *
 * Example:
 *   s := slug.Of("Soil & Seeds") // "soil--seeds"
 */
func Of(heading string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case r == ' ':
			sb.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r):
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

/** Slugger issues the slugs of one page's headings, in document order. A
 * slug already issued gets `-1`, `-2` and so on, the first free one, as GitHub
 * does. Use one Slugger per page; it is not safe for concurrent use.
 */
type Slugger struct {
	used map[string]bool
	next map[string]int
}

/** New returns an empty Slugger.
 *
 * Returns:
 *   *Slugger — a Slugger that has issued nothing
 *
 * Example:
 *   s := slug.New()
 */
func New() *Slugger {
	return &Slugger{used: map[string]bool{}, next: map[string]int{}}
}

/** Next returns the unique slug for the next heading of the page. A heading
 * with no slug gives "" and is not remembered.
 *
 * Parameters:
 *   heading (string) — the heading's plain text
 *
 * Returns:
 *   string — the slug, unique on this page, or ""
 *
 * Example:
 *   s := slug.New()
 *   s.Next("Notes") // "notes"
 *   s.Next("Notes") // "notes-1"
 */
func (s *Slugger) Next(heading string) string {
	base := Of(heading)
	if base == "" {
		return ""
	}
	out := base
	for k := s.next[base]; s.used[out]; k++ {
		if k == 0 {
			k = 1
		}
		out = base + "-" + strconv.Itoa(k)
		s.next[base] = k + 1
	}
	s.used[out] = true
	return out
}

/** Reset forgets everything issued, so the Slugger can serve another page.
 *
 * Example:
 *   s.Reset()
 */
func (s *Slugger) Reset() {
	s.used = map[string]bool{}
	s.next = map[string]int{}
}
