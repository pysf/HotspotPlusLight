`htmx.min.js` is htmx 2.0.4, vendored unmodified from
https://cdn.jsdelivr.net/npm/htmx.org@2.0.4/dist/htmx.min.js (BSD 0-clause licence).
Upgrading = replace the file and change the version here.

`inter-latin-wght-normal.woff2` is Inter (Latin subset, variable weight), from the npm package
`@fontsource-variable/inter` 5.2.5 (https://cdn.jsdelivr.net/npm/@fontsource-variable/inter@5.2.5/files/inter-latin-wght-normal.woff2).
Licence: SIL Open Font License 1.1, see `OFL-Inter.txt`.

`css/` and `js/` are our own files, edited by hand (no build step). `static.go` inlines `css/app.css` imports and hashes everything at startup. See `docs/ui-notes.md`.
