
# Change log

## Unreleased

- Extracted `page`, `toc`, `local`, `reader` and `debuglog` from cmarkbrowser's `internal/`, with their tests. No change of behaviour.
- `toc` reads OPML through `rsdoiel/opml` (its character-set handling replaces the copy in `toc`).
- Every exported symbol has a `/** */` block with an example.
