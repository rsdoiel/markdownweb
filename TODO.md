# TODO

- v0.0.6 is prepared (`be2098f`, the `slug` package) and pushed, not tagged. Tag it, then move cmarkfind, cmarkbrowser and antennaApp from the pseudo-version `v0.0.6-0.20261127212007-3ec0cd8a5c56` to `v0.0.6` (the module proxy can lag a few minutes).
- cmarkbrowser's anchor fixes (`d2233d1`) have no `CHANGES.md` entry of their own yet.
- cmarkbrowser does not recognise a Setext heading that spans several lines; a fragment link to one opens the page at the top.
- `toc.Includes` resolves against the base and keeps its own include walk; `opml.Includes` returns raw addresses. Left as is in M1.
