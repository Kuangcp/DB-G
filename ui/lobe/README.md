# Vendored Lobe UI theme

`theme.go`, `tokens.go` and `tokens_gen.go` are copied from
[`lobehub/lobe-mygo`](https://github.com/lobehub/lobe-mygo) `pkg/theme`
(MIT, see `LICENSE`) at commit `5b3ecde`, because the module has no published
tag yet and Go proxies cannot resolve that pseudo-version.

`tokens_gen.go` is generated there from `@lobehub/ui`; do not hand-edit it.
To update, copy the three files from the lobe-mygo checkout and keep the
package name `theme` (db-g imports it as `lobe "db-g/ui/lobe"`). Only
`theme.go` is adapted to db-g's `ui.Theme` field mapping.
