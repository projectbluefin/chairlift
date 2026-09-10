// Package dryrun is the single authority for preview (dry-run) mode.
//
// Previously every integration package kept its own package-level dryRun
// flag behind SetDryRun/IsDryRun, and internal/app had to fan out one
// setter call per package at startup. That propagation was fail-open: a
// new integration that forgot to register its setter silently executed
// real mutations while the user believed --dry-run was active. There is
// now exactly one flag; app sets it once and every integration reads it
// here.
package dryrun

import (
	"log"
	"sync/atomic"
)

var enabled atomic.Bool

// Set enables or disables dry-run mode process-wide. Called once from
// internal/app at startup, and from tests.
func Set(mode bool) {
	enabled.Store(mode)
	log.Printf("dry-run mode: %v", mode)
}

// Enabled reports whether dry-run mode is active.
func Enabled() bool {
	return enabled.Load()
}
