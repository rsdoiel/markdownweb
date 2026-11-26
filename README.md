
# markdownweb

A Go module for the protocol layer of the Markdown web, shared by its readers (cmarkbrowser, cmarkfind) and its writers (antennaApp). Extracted from cmarkbrowser's `internal/` (markdownweb DR-0001).

| Package | Holds |
|---|---|
| `page` | `Fetch` (BonBon's `Accept`, the declared alternate link, the `.md` sibling, `Content-Location`), `FetchOther`, `Classify`, `ReadFile` |
| `toc` | parse `.well-known/toc.opml` (through `rsdoiel/opml`), `Finder` (which toc lists a page, includes followed, with limits), `LookupLocal`, `Mirror` |
| `local` | a site on disk; links confined to the root |
| `reader` | HTML to a Markdown reader view |
| `debuglog` | a nil-safe JSON-lines tracer carried in the context |

```shell
go get github.com/rsdoiel/markdownweb
```

Requires Go 1.27.4 or later, `github.com/rsdoiel/frontmatter`, `github.com/rsdoiel/opml` and `golang.org/x/net`. No Charm, no SQLite, no cgo.

The packages keep the behaviour they had in cmarkbrowser.
