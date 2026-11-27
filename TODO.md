
# TODO

- v0.0.5 prepared: `fetch.GetIf` for conditional requests (cmarkfind remote fill). Next: cmarkfind and cmarkbrowser bump go.mod to it.
- `toc.Includes` resolves against the base and keeps its own include walk; `opml.Includes` returns raw addresses. Left as is in M1.
- Add the heading slug rule (the fragment identifier of a Markdown heading), as a function others import. cmarkfind needs it now: its index writes heading anchors and has its own `index.Slug` (index/excerpt.go) using the GitHub rule: lower case; letters, marks, digits, `-` and `_` kept; each space becomes `-`; other characters dropped. Repeats on one page get `-1`, `-2`, so the function needs a page-level form (a slugger that remembers what it has issued) as well as the single-heading form. cmarkbrowser should use it to resolve `#fragment` links, and antennaApp and other experiments can use it for heading links. The earlier design records said markdownweb already had this rule; it did not. When it lands: cmarkfind drops `index.Slug` for it, and its test table moves here.
