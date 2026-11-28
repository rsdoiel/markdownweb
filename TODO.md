# TODO

- v0.0.6 is released. Move cmarkfind, cmarkbrowser and antennaApp from the pseudo-version `v0.0.6-0.20261127212007-3ec0cd8a5c56` to `v0.0.6`, and run each suite.
- cmarkbrowser's anchor fixes (`d2233d1`) have no `CHANGES.md` entry of their own yet.
- cmarkbrowser does not recognise a Setext heading that spans several lines; a fragment link to one opens the page at the top.
- `toc.Includes` resolves against the base and keeps its own include walk; `opml.Includes` returns raw addresses. Left as is in M1.
