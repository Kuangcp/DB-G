package theme

// Vendored from github.com/lobehub/lobe-mygo pkg/theme (MIT, see LICENSE in
// this directory) so release builds need no unpublished module version.
// Tokens are generated from @lobehub/ui 6.15.2 by scripts/gen-tokens.ts.

func Light() *Tokens {
	t := lightTokens
	return &t
}

func Dark() *Tokens {
	t := darkTokens
	return &t
}

func ColorTokenNames() []string {
	return append([]string(nil), colorTokenNames...)
}
