
# Change log

## v0.0.3, 2026-11-26

- New package `fetch` (markdownweb DR-0002): a polite HTTP client with a pause per host, a named User-Agent, bounded redirects, body size and waits, `Retry-After`, and opt-in `robots.txt` (`github.com/temoto/robotstxt`) with `Crawl-delay`. `Get` reads the body; `Open` returns the response for a caller that reads part of it.
- `page.Fetch`, `page.FetchOther` and `page.FetchReader` now go through a shared fetcher: `page.Fetcher` and `page.SetFetcher`. The default sends `User-Agent: markdownweb/VERSION` (it was Go's default), keeps the 30 s timeout and ten redirects, and does not pause or read `robots.txt`.
- `page.ErrData` and `page.ErrUnavailable` are the same values as `fetch.ErrData` and `fetch.ErrUnavailable`; new `page.ErrForbidden`.
- antennaApp harvests through `fetch` (antennaApp's harvest keeps its own pause, User-Agent and exit classes).

## v0.0.2, 2026-11-26

- `page.PrefersMarkdown`: whether an Accept header asks for Markdown in preference to HTML (moved from antennaApp's preview server).
- `page.MarkdownPath`: the Markdown path for a file path, `x.html` and `x.htm` to `x.md` (moved from antennaApp's `markdownPathFor`).
- `page.DeclaredAlternate` is exported (it was `declaredAlternate`).
- `toc.FileName`: the `.well-known/toc.opml` path, used by the local and remote finders.
- Fixed: a declared alternate whose `href` was blank resolved to the page itself and was returned as its own alternate; it is now ignored.
- antennaApp's reading of the declared alternate now follows the shared rule: an alternate on another host is skipped and the next one is tried, where antennaApp gave up at the first.

## v0.0.1, 2026-11-26

- Extracted `page`, `toc`, `local`, `reader` and `debuglog` from cmarkbrowser's `internal/`, with their tests. No change of behaviour.
- `toc` reads OPML through `rsdoiel/opml` (its character-set handling replaces the copy in `toc`).
- Every exported symbol has a `/** */` block with an example.
