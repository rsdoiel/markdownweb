
# TODO

- v0.0.5 prepared: `fetch.GetIf` for conditional requests (cmarkfind remote fill). Next: cmarkfind and cmarkbrowser bump go.mod to it.
- `toc.Includes` resolves against the base and keeps its own include walk; `opml.Includes` returns raw addresses. Left as is in M1.
- `slug` package is in (unreleased). Next: cmarkfind drops `index.Slug` and uses `slug.Slugger` in index/build.go (its test table is copied here); cmarkbrowser resolves `#fragment` links with it; antennaApp heading links.
