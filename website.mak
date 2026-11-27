
#
# Makefile for the markdownweb website: pandoc builds an HTML page beside each Markdown
# page, the Markdown pages are published as they are, for BonBon browsers and
# cmarkbrowser, and toc.bash writes /.well-known/toc.opml.
#
# The pages are listed by name, not globbed, so the site does not change with
# whatever .md files happen to sit in the directory. README.md becomes index.html
# and index.md.
#
# Written by hand from knowledge's website.mak; do not regenerate it with cmt.
#
PROJECT = markdownweb

# Where the site is published (GitHub Pages, gh-pages branch).
BASE_URL = https://rsdoiel.github.io/markdownweb/

PANDOC = $(shell which pandoc)

# Every published page, in the order of the table of contents, README.md first:
# it is the home page. The files in NO_PUBLISH are not pages.
PAGE_ORDER = README.md CHANGES.md about.md

# Internal notes stay unpublished. index.md is a copy of README.md.
NO_PUBLISH = TODO.md index.md

MD_PAGES = $(filter-out $(NO_PUBLISH),$(shell ls -1 *.md))

MISSING = $(filter-out $(PAGE_ORDER),$(MD_PAGES))

HTML_PAGES = $(patsubst README.html,index.html,$(patsubst %.md,%.html,$(PAGE_ORDER)))

TOC = .well-known/toc.opml

build: check-order $(HTML_PAGES) index.md $(TOC) .nojekyll

check-order:
	@if [ -n "$(MISSING)" ]; then echo "website.mak: add to PAGE_ORDER: $(MISSING)" >&2; exit 1; fi

# Pandoc is optional; without it the existing pages are left as they are.
# Each HTML page names its Markdown page, for a reader that finds the HTML first.
$(HTML_PAGES): .FORCE
	@if [ -z "$(PANDOC)" ]; then echo "website: pandoc not found, skipping $@" >&2; else \
		SRC="$(patsubst index.html,README.html,$@)"; SRC="$${SRC%.html}"; \
		PUBLISHED="$${SRC}.md"; if [ "$${SRC}" = "README" ]; then PUBLISHED="index.md"; fi; \
		$(PANDOC) --metadata title="$${SRC}" -V markdown-source="$${PUBLISHED}" \
			-s --to html5 "$${SRC}.md" -o "$@" \
			--lua-filter=links-to-html.lua \
			--lua-filter=add-col-scope.lua \
			--template=page.tmpl; fi

# A BonBon browser asks for the site's address and gets index.md.
index.md: README.md
	cp README.md index.md

# GitHub Pages hides folders that start with a dot (and so /.well-known/)
# unless the site says it is not a Jekyll site.
.nojekyll:
	touch .nojekyll

$(TOC): $(MD_PAGES) toc.bash
	mkdir -p $(dir $(TOC))
	./toc.bash $(BASE_URL) $(PROJECT) $(PAGE_ORDER) > $(TOC).tmp
	mv $(TOC).tmp $(TOC)

clean:
	@rm -f $(HTML_PAGES)

.FORCE:
