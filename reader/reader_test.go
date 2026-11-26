package reader

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func convert(t *testing.T, html string) Result {
	t.Helper()
	r, err := Convert([]byte(html), "https://example.org/dir/page.html")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestHeadingsParagraphsAndInlineMarkup(t *testing.T) {
	r := convert(t, `<html><head><title>My Page</title></head><body>
<h1>Main title</h1><p>First <strong>bold</strong> and <em>soft</em> and <code>code()</code>.</p>
<h2>Sub</h2><p>Second paragraph.</p></body></html>`)
	for _, want := range []string{"# Main title", "First **bold** and *soft* and `code()`.", "## Sub", "Second paragraph."} {
		if !strings.Contains(r.Markdown, want) {
			t.Errorf("%q not in:\n%s", want, r.Markdown)
		}
	}
	if r.Title != "My Page" {
		t.Errorf("title %q", r.Title)
	}
}

func TestTitleFallsBackToTheFirstHeading(t *testing.T) {
	if r := convert(t, `<body><h1>Only heading</h1><p>x</p></body>`); r.Title != "Only heading" {
		t.Errorf("title %q", r.Title)
	}
}

func TestListsQuotesRulesAndPre(t *testing.T) {
	r := convert(t, `<body><ul><li>one</li><li>two<ul><li>nested</li></ul></li></ul>
<ol><li>first</li><li>second</li></ol><blockquote><p>quoted</p></blockquote><hr>
<pre>line 1
  line 2</pre></body>`)
	for _, want := range []string{"- one", "- two", "  - nested", "1. first", "2. second", "> quoted", "---", "```\nline 1\n  line 2\n```"} {
		if !strings.Contains(r.Markdown, want) {
			t.Errorf("%q not in:\n%s", want, r.Markdown)
		}
	}
}

func TestTableBecomesAPipeTable(t *testing.T) {
	r := convert(t, `<body><table><tr><th>Name</th><th>Qty</th></tr><tr><td>apple | pear</td><td>3</td></tr></table></body>`)
	for _, want := range []string{"| Name | Qty |", "| --- | --- |", `| apple \| pear | 3 |`} {
		if !strings.Contains(r.Markdown, want) {
			t.Errorf("%q not in:\n%s", want, r.Markdown)
		}
	}
}

func TestScriptsStylesAndChromeAreDropped(t *testing.T) {
	r := convert(t, `<html><head><style>.x{}</style><script>var secret=1</script></head><body>
<nav><a href="/home">Home menu</a></nav><script>alert("evil")</script><style>b{}</style>
<noscript>enable js</noscript><form><input value="typed"><button>Submit</button></form>
<svg><text>svgtext</text></svg><iframe src="x">frame</iframe>
<div hidden>hidden text</div><div aria-hidden="true">aria hidden</div><div style="display:none">none</div>
<p>Visible.</p></body></html>`)
	for _, bad := range []string{"secret", "evil", "Home menu", "enable js", "typed", "Submit", "svgtext", "frame", "hidden text", "aria hidden", "none"} {
		if strings.Contains(r.Markdown, bad) {
			t.Errorf("%q should not be in:\n%s", bad, r.Markdown)
		}
	}
	if !strings.Contains(r.Markdown, "Visible.") {
		t.Errorf("visible text lost:\n%s", r.Markdown)
	}
}

func TestLinksAndImagesAreResolvedAndUnsafeOnesDropped(t *testing.T) {
	r := convert(t, `<body><p><a href="other.html">rel</a> <a href="/abs">abs</a> <a href="https://x.org/a?b=1&amp;c=2">ext</a>
<a href="javascript:alert(1)">script link</a> <a href="data:text/html,x">data link</a> <a>no href</a></p>
<p><img src="pic.png" alt="A picture"> <img src="data:image/png;base64,AAAA" alt="inline"> <img src="deco.png" alt=""></p></body>`)
	for _, want := range []string{
		"[rel](https://example.org/dir/other.html)", "[abs](https://example.org/abs)",
		"[ext](https://x.org/a?b=1&c=2)", "script link", "data link", "no href",
		"![A picture](https://example.org/dir/pic.png)",
	} {
		if !strings.Contains(r.Markdown, want) {
			t.Errorf("%q not in:\n%s", want, r.Markdown)
		}
	}
	for _, bad := range []string{"javascript:", "data:", "deco.png"} {
		if strings.Contains(r.Markdown, bad) {
			t.Errorf("%q should not be in:\n%s", bad, r.Markdown)
		}
	}
}

func TestEntitiesAreDecodedAndMarkdownSpecialsAreEscaped(t *testing.T) {
	r := convert(t, `<body><p>Fish &amp; chips&nbsp;cost 5 * 3 = 15, see [the list](not a link) and a_b_c and <b>&lt;tag&gt;</b></p>
<p># not a heading</p><p>1. not a list</p></body>`)
	for _, want := range []string{`Fish & chips cost 5 \* 3 = 15`, `\[the list\](not a link)`, `a\_b\_c`, `\<tag\>`, `\# not a heading`, `1\. not a list`} {
		if !strings.Contains(r.Markdown, want) {
			t.Errorf("%q not in:\n%s", want, r.Markdown)
		}
	}
}

func TestWhitespaceIsCollapsedAndBrBreaksALine(t *testing.T) {
	r := convert(t, "<body><p>  many   spaces\n\n and\tnewlines  </p><p>line one<br>line two</p></body>")
	if !strings.Contains(r.Markdown, "many spaces and newlines") {
		t.Errorf("not collapsed:\n%s", r.Markdown)
	}
	if !strings.Contains(r.Markdown, "line one\\\nline two") {
		t.Errorf("br not kept as a break:\n%s", r.Markdown)
	}
}

func TestTheLargestArticleIsTheContent(t *testing.T) {
	long := strings.Repeat("The real article text goes on and on. ", 20)
	r := convert(t, `<body><header><h1>Site name</h1></header><nav>menu</nav>
<main><article><h2>Related</h2><p>short card</p></article>
<article><h1>The Post</h1><p>`+long+`</p></article>
<article><p>another card</p></article></main><footer>copyright</footer></body>`)
	if !strings.Contains(r.Markdown, "The Post") || !strings.Contains(r.Markdown, "real article text") {
		t.Errorf("article missing:\n%s", r.Markdown)
	}
	for _, bad := range []string{"Site name", "menu", "short card", "another card", "copyright"} {
		if strings.Contains(r.Markdown, bad) {
			t.Errorf("%q came from outside the article:\n%s", bad, r.Markdown)
		}
	}
}

func TestWithoutAnArticleMainThenBodyIsUsed(t *testing.T) {
	r := convert(t, `<body><nav>menu</nav><main><p>main text</p></main><footer>foot</footer></body>`)
	if !strings.Contains(r.Markdown, "main text") || strings.Contains(r.Markdown, "menu") || strings.Contains(r.Markdown, "foot") {
		t.Errorf("main:\n%s", r.Markdown)
	}
	r = convert(t, `<body><p>just a body</p></body>`)
	if !strings.Contains(r.Markdown, "just a body") {
		t.Errorf("body:\n%s", r.Markdown)
	}
}

func TestAPageWithNoTextIsReportedNotShownEmpty(t *testing.T) {
	r := convert(t, `<html><head><title>JS app</title></head><body><div id="root"></div><script>render()</script></body></html>`)
	if strings.TrimSpace(r.Markdown) != "" {
		t.Errorf("markdown %q", r.Markdown)
	}
	if !r.Empty {
		t.Error("Empty not set for a page with no text")
	}
}

func TestOutputIsCappedOnAHugePage(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("<body>")
	for i := 0; i < 200000; i++ {
		sb.WriteString("<p>paragraph of some text to fill the page</p>")
	}
	r := convert(t, sb.String())
	if len(r.Markdown) > MaxOutput+1024 {
		t.Errorf("output %d bytes, cap %d", len(r.Markdown), MaxOutput)
	}
	if !r.Truncated {
		t.Error("Truncated not set")
	}
}

func TestBadBaseIsAnError(t *testing.T) {
	if _, err := Convert([]byte("<p>x</p>"), "://bad"); err == nil {
		t.Error("bad base accepted")
	}
}

// Every heading level must come out as exactly that many # marks on a line of
// its own. (An earlier version computed the level from the difference of two
// atom numbers, which are hashes, not 1 to 6: <h2> became 263,425 # marks and
// a 9 KB page became 1.3 MB.)
func TestEachHeadingLevelIsExactlyThatManyHashMarks(t *testing.T) {
	var html strings.Builder
	html.WriteString("<body>")
	for level := 1; level <= 6; level++ {
		fmt.Fprintf(&html, "<h%d>Level %d title</h%d><p>text %d</p>", level, level, level, level)
	}
	html.WriteString("</body>")
	r := convert(t, html.String())
	for level := 1; level <= 6; level++ {
		want := fmt.Sprintf("(?m)^%s Level %d title$", strings.Repeat("#", level), level)
		if !regexp.MustCompile(want).MatchString(r.Markdown) {
			t.Errorf("h%d: no line matching %s in:\n%s", level, want, r.Markdown)
		}
	}
	for i, line := range strings.Split(r.Markdown, "\n") {
		if len(line) > 200 {
			t.Fatalf("line %d is %d bytes long: %.60s...", i, len(line), line)
		}
	}
}

// Whatever the page holds, the Markdown must not be wildly bigger than the HTML
// it came from. The headings test above found a 150-fold blowup; this guards
// the whole converter against another of that kind.
func TestOutputIsNeverWildlyBiggerThanTheInput(t *testing.T) {
	pages := []string{
		"<body>" + strings.Repeat("<h1>a</h1><h2>b</h2><h3>c</h3><h4>d</h4><h5>e</h5><h6>f</h6>", 50) + "</body>",
		"<body>" + strings.Repeat("<ul><li>x<ul><li>y<ul><li>z</li></ul></li></ul></li></ul>", 50) + "</body>",
		"<body>" + strings.Repeat("<blockquote><blockquote><p>q</p></blockquote></blockquote>", 50) + "</body>",
		"<body><table>" + strings.Repeat("<tr><td>a</td><td>b</td></tr>", 100) + "</table></body>",
		"<body>" + strings.Repeat("<p><a href='/x'>l</a> <img src='i.png' alt='a'> <code>c</code> <b>b</b></p>", 100) + "</body>",
	}
	for i, page := range pages {
		r := convert(t, page)
		if limit := 3*len(page) + 1024; len(r.Markdown) > limit {
			t.Errorf("page %d: %d bytes of HTML became %d bytes of Markdown (limit %d)", i, len(page), len(r.Markdown), limit)
		}
	}
}
