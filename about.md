---
title: markdownweb
abstract: |-
  A Go module for the protocol layer of the Markdown web: fetch a page as Markdown (BonBon's Accept, the declared alternate link, the .md sibling), read a site's toc.opml and find a page's neighbours, browse a site on disk, and convert HTML to a Markdown reader view.
authors:
  - family_name: Doiel
    given_name: R. S.
    id: https://orcid.org/0000-0003-0900-6903


maintainer:
  - family_name: Doiel
    given_name: R. S.
    id: https://orcid.org/0000-0003-0900-6903

repository_code: https://github.com/rsdoiel/markdownweb
version: 0.0.2
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go >= 1.27.4

keywords:
  - markdown web
  - bonbon
  - opml
  - toc
  - markdown
  - reader

date_released: 2026-11-26
---

About this software
===================

## markdownweb 0.0.2

- page: PrefersMarkdown (an Accept header asks for Markdown), MarkdownPath (a file path's Markdown copy), DeclaredAlternate (now exported); shared with antennaApp
- page: a blank href in a declared alternate link is ignored (it resolved to the page itself)
- toc: FileName, the .well-known/toc.opml path, used by the local and remote finders

## Authors

- [R. S. Doiel](https://orcid.org/0000-0003-0900-6903)




## Maintainers

- [R. S. Doiel](https://orcid.org/0000-0003-0900-6903)


A Go module for the protocol layer of the Markdown web: fetch a page as Markdown (BonBon's Accept, the declared alternate link, the .md sibling), read a site's toc.opml and find a page's neighbours, browse a site on disk, and convert HTML to a Markdown reader view.

- [License](https://www.gnu.org/licenses/agpl-3.0.txt)
- [Code Repository](https://github.com/rsdoiel/markdownweb)
  - [Issue Tracker](https://github.com/rsdoiel/markdownweb/issues)

## Programming languages

- Go >= 1.27.4




## Software Requirements

- Go >= 1.27.4
- github.com/rsdoiel/frontmatter >= 0.0.1
- github.com/rsdoiel/opml >= 0.0.12
- golang.org/x/net >= 0.55.0


## Software Suggestions

- CMTools >= 0.0.46
- GNU Make >= 3


