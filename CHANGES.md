
# Change log

## Unreleased

- `opensearch.Service`, `FromLinks` and `Discover`: the search services a site offers, in order of preference: the page's `Link` headers (no request), the `search` key of `/.well-known/cmarkfind.json`, and the `urlSearch` key of `/.well-known/bonbon.json` (a Markdown search with `?q=` appended). The files are read from the site root, once each, at most 64 KiB; a missing file is not an error and a bad one is reported while the others are still used. A restricted policy drops a service on another origin; `AnyOrigin` keeps it, marked `CrossOrigin`. A description is never requested by `Discover`.
- New package `opensearch`: `Parse` reads an OpenSearch 1.1 description (at most 64 KiB, 16 `Url` elements, 16 levels, no DOCTYPE or entities, the OpenSearch namespace required, a `Url` with no type, no http(s) template or no `{searchTerms}` dropped); `Description.Url` picks a results template by media type; `Url.Expand` fills a template (terms percent-encoded for where they stand, optional parameters empty, an unknown required one or an address over 8,000 bytes refused); `Url.Origin` says where a query goes; `Policy` and `Fetch` get a description either restricted to the offering origin (the default; needs a `SameOrigin` fetcher) or from any http(s) origin. `fetch.Fetcher.SameOrigin` reports the option.
- `fetch.Link` and `fetch.ParseLinks`: the response's `Link` headers (RFC 8288, the subset needed: target, `rel`, `type`, `title`), resolved against the final address, at most 16 kept, a value over 2 KiB skipped, a title cut at 128 bytes and stripped of control characters, a target that is not http or https dropped. `fetch.Result.Links` and `page.Page.Links` carry them for `Get`, `GetIf`, `page.Fetch` and `page.FetchReader`; nil when there is no header. For a client that finds a search service from the header.
- `fetch.Options.SameOrigin`: a redirect to another origin (scheme, host or port) is refused before it is followed, with `fetch.ErrCrossOrigin` (also an `ErrData`), for a caller that reads files named by a published manifest (cmarkfind's remote index). Off by default; the redirect limit still applies.

## v0.0.6, 2026-11-27

- New package `slug`: `slug.Of` (the fragment identifier of one heading, GitHub's rule) and `slug.Slugger` (`New`, `Next`, `Reset`) for a whole page, where a repeated heading gets `-1`, `-2`, skipping any suffix already taken. For cmarkfind, cmarkbrowser's `#fragment` links and antennaApp's heading links.

## v0.0.5, 2026-11-26

- `fetch.Fetcher.GetIf` and `fetch.Validators`: a conditional request (If-None-Match, If-Modified-Since) with the same pause, retry and robots rules as `Get`. `fetch.Result` gains `ETag`, `LastModified` and `NotModified` (true on a 304); `Get` fills the first two. Asked for by cmarkfind's remote fill (cmarkfind DR-0001).

## v0.0.4, 2026-11-26

- `toc.Finder.Pages` and `PagesLocal` (markdownweb DR-0003): every page a table of contents names, flattened as BonBon does (each outline with a url except includes, in document order, a page before the outlines under it), includes read in place. Returns a `Listing` of `Page`s (address, title, the outline it came from, include depth, `Group`, and `Path` for a local site) with what was `Skipped` and whether the list is `Complete`. `Limits`: include depth (default 1), pages (10,000), files (50), bytes (32 MiB), and named extra hosts; never any host. Loops are skipped and reported, a diamond is read once, a page is listed once whatever form its address takes.
- `toc.Doc.AllPages` and `Entry.Group`: the same flatten at the document level. `Entries` is unchanged.
- `toc.NewFinderWith(*fetch.Fetcher)`. The Finder no longer has its own `http.Client`: every outline and `bonbon.json` read goes through `fetch.Fetcher`, `page.Fetcher()` by default (read at each request, so a program may install its fetcher after making a Finder). `Lookup` therefore sends the program's User-Agent, pauses per host when asked to, and finds no outline where robots.txt forbids it, if the program turned robots on. With the default fetcher nothing else changes.
- `toc.ErrNoOutline`, and the `Skip*` reasons.
- **Behaviour change (markdownweb DR-0004): `Neighbours` and `NeighboursByPath` follow BonBon's rule.** The outline is one flat list (`AllPages`): previous is the line above, next the line below, the ends wrap, groups do not stop the arrows, nothing is reordered whatever the calendar outlines say, a page with outlines under it is a stop. A page listed more than once has the nearest different page on each side. A page under the folder of a top-level include (its url up to `/.well-known/`) is read in that site's list alone and wraps inside it, as BonBon's `getSiteToc` does. Checked against BonBon 0.6.57's own function on `rsdoiel.github.io`: 220 of 220 pages agree. The leaves-only rule with calendar reversal is gone; `Entries` keeps its leaves-only list.
- New: `Doc.Stops`, `StopsOf`, `StopsByPath` (the candidates for each neighbour, nearest first, wrapping, for a caller that checks that one answers), `Doc.NeighboursOf` (a page known by more than one address), `Result.Before` and `Result.After`.
- `LookupLocal` passes over a neighbour that has no file for the next one, where it used to drop it.

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
