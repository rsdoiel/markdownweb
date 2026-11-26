/** Package markdownweb is the protocol layer of the Markdown web, shared by
 * its readers (cmarkbrowser, cmarkfind) and its writers (antennaApp).
 *
 * The code is in subpackages:
 *
 *   page      fetch a page as Markdown: BonBon's Accept header, the declared
 *             alternate link, the .md sibling, Content-Location
 *   toc       read a site's .well-known/toc.opml and find a page's neighbours
 *   local     a site on disk, links confined to its root
 *   reader    HTML to a Markdown reader view
 *   debuglog  a nil-safe JSON-lines tracer carried in the context
 *
 * It imports nothing from Charm or SQLite and uses no cgo. Front matter is
 * read with github.com/rsdoiel/frontmatter and OPML with github.com/rsdoiel/opml.
 *
 * Example:
 *   p, err := page.Fetch(ctx, "https://example.org/blog/a.html")
 */
package markdownweb
