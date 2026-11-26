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
version: 0.0.3
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

## markdownweb 0.0.3

- fetch: new package, a polite HTTP client: pause per host, named User-Agent, bounded redirects, body size and waits, Retry-After, and opt-in robots.txt with Crawl-delay (github.com/temoto/robotstxt); Get reads a body, Open returns the response (markdownweb DR-0002)
- page: Fetch, FetchOther and FetchReader go through a shared fetcher; page.Fetcher and page.SetFetcher let a crawler install its own. The default sends User-Agent markdownweb/VERSION (it was Go's), keeps the 30 s timeout and ten redirects, and neither pauses nor reads robots.txt
- page: ErrData and ErrUnavailable are now fetch.ErrData and fetch.ErrUnavailable; new ErrForbidden
- antennaApp harvests through fetch

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


