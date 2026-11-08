# Documentation and release notes

README, docs/*.md and skills/gthok/SKILL.md are embedded in the binary. Maintainer
notes in docs/notes are kept in the repository, not searched as public docs.
`gthok docs` lists sections; `docs <id>` prints exact Markdown; `docs --all`
prints the corpus. Search is offline BM25 (k1=1.2, b=0.75), weights title 3,
description 2, body 1, top five JSON hits, ties by section ID. Preserve these
ranking constants when editing documentation.

Source URLs use github.com/mkusaka/gthok at the matching release tag; dev/unknown
versions use main. Test the embedded corpus after heading changes: headings
become stable section IDs. Regenerate JSON Schemas with
`go generate ./internal/schema`, never hand-edit generated JSON.

Release archive contract: gthok_<version>_<os>_<arch>.tar.gz with gthok, README.md
and LICENSE. Check packaging/action implementation agrees before release; docs
are not a substitute for validating a published artifact.
