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
version: 0.0.4
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
  - robots.txt
  - fetcher

date_released: 2026-11-26
---

About this software
===================

## markdownweb 0.0.4

- toc: Finder.Pages and PagesLocal list every page a table of contents names, flattened as BonBon does, includes read in place, within limits on depth, pages, files, bytes and hosts; loops skipped and reported (markdownweb DR-0003). Doc.AllPages and Entry.Group do the same at the document level
- toc: BEHAVIOUR CHANGE, Neighbours and NeighboursByPath follow BonBon's rule: one flat list, previous the line above, next the line below, the ends wrap, no calendar reversal, a page with outlines under it is a stop, an included site is a list of its own (markdownweb DR-0004). Matches BonBon 0.6.57 on 220 of 220 pages of rsdoiel.github.io. Entries keeps its leaves-only list
- toc: new Doc.Stops, StopsOf, StopsByPath, Doc.NeighboursOf, Result.Before and Result.After, ErrNoOutline and the Skip reasons; LookupLocal passes over a neighbour with no file
- toc: NewFinderWith(*fetch.Fetcher); the Finder reads outlines and bonbon.json through fetch, so it sends the program's User-Agent and honours its pause and robots.txt settings

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
- github.com/temoto/robotstxt >= 1.1.2
- golang.org/x/net >= 0.55.0


## Software Suggestions

- CMTools >= 0.0.46
- GNU Make >= 3


