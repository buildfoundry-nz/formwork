# .husky/ — git hooks

## TODO: fold these hook notes into docs/lockdown-enumeration.md

Markdown is documentation, not a hook: a `#` here opens a heading, not a shell
comment, and the repo marker ban exempts docs.

The heading above is what makes this file load-bearing for the `.husky/*.md`
exclusion, and its POSITION is part of the pin. Two things had to be true for
the marker to be reachable at all:

1. It has to be heading-shaped. A marker in bare prose carries no `#` on its
   own line, so the pattern cannot see it with or without the exclusion and
   the fixture stays green either way — the state this file was in when it was
   written.
2. Nothing above it may contain an apostrophe. The rule projects this file
   through a SHELL lexer, where an apostrophe opens a string that runs to the
   next one or to EOF, blanking every line after it. Ordinary markdown prose
   is full of them, which is a second and independent reason a `#` in here
   cannot be read as a shell comment.

Delete the exclusion and this fixture turns red.
