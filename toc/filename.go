package toc

/** FileName is where a site keeps its table of contents, relative to the
 * folder it covers, in slash form. Readers join it to a root or a folder;
 * writers create it there. Join with filepath.FromSlash for a disk path.
 *
 * Example:
 *   u := "https://x.org/" + toc.FileName // https://x.org/.well-known/toc.opml
 */
const FileName = ".well-known/toc.opml"
