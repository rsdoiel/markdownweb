
# markdownweb

A Go module for the protocol layer of the Markdown web, shared by its readers (cmarkbrowser, cmarkfind) and its writers (antennaApp). Extracted from cmarkbrowser's `internal/` (markdownweb DR-0001).

| Package | Holds |
|---|---|
| `fetch` | the polite HTTP client: pause per host, named User-Agent, bounded redirects, body size and `Retry-After`, opt-in `robots.txt` |
| `page` | `Fetch` (BonBon's `Accept`, the declared alternate link, the `.md` sibling, `Content-Location`), `FetchOther`, `Classify`, `ReadFile` |
| `toc` | parse `.well-known/toc.opml` (through `rsdoiel/opml`), `Finder` (which toc lists a page, includes followed, with limits; `Pages` lists every page), `Neighbours` (previous and next by BonBon's rule), `LookupLocal`, `Mirror` |
| `slug` | a Markdown heading's fragment identifier (GitHub's rule), one heading at a time or a whole page with `-1`, `-2` for repeats |
| `local` | a site on disk; links confined to the root |
| `reader` | HTML to a Markdown reader view |
| `debuglog` | a nil-safe JSON-lines tracer carried in the context |

```shell
go get github.com/rsdoiel/markdownweb
```

Requires Go 1.27.4 or later, `github.com/rsdoiel/frontmatter`, `github.com/rsdoiel/opml`, `github.com/temoto/robotstxt` and `golang.org/x/net`. No Charm, no SQLite, no cgo.

The packages keep the behaviour they had in cmarkbrowser, except `toc.Neighbours`, which since v0.0.4 follows BonBon's rule, and `slug`, which is new in v0.0.6 (see `CHANGES.md`).
