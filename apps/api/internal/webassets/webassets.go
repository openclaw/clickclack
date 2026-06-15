package webassets

import "embed"

// Dist is replaced by the root pnpm build script after the Svelte app builds.
//
// The all: prefix is required so files whose names begin with "_" or "."
// are embedded too. SvelteKit/Rollup can emit chunk filenames with a leading
// underscore (e.g. _MajmMwB.js); without all: those are silently dropped from
// the binary and the server returns the SPA fallback HTML for them, which
// breaks module loading with a MIME-type error and a blank screen.
//
//go:embed all:dist
var Dist embed.FS
