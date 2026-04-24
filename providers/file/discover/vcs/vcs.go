// Package vcs provides [file.Discoverer] implementations that use real VCS
// tooling to find repository roots:
//   - [GitRoot] uses go-git, correctly handling worktrees, submodules, and common-dir
//   - [JujutsuRoot] forks `jj root` to get the Jujutsu workspace root
//
// Prefer these over the dirwalk-based discoverers in the parent package when
// accuracy matters (e.g. git worktrees, jj colocated repos).
//
// Ordering is caller-controlled — pass discoverers in the order they should be tried:
//
//	file.DiscoverAll(ctx, parsers,
//	    vcs.JujutsuRoot(),   // jj first (wins in colocated repos)
//	    vcs.GitRoot(),  // git fallback
//	    discover.XDG(), // global fallback
//	)
package vcs

import (
	"os"
	"path/filepath"
	"strings"
)

// Option configures a VCS discoverer.
type Option func(*opts)

type opts struct {
	startDir string
}

// WithStartDir overrides the starting directory used by the discoverer.
// By default os.Getwd() is used.
func WithStartDir(dir string) Option {
	return func(o *opts) { o.startDir = dir }
}

func resolveOpts(options []Option) opts {
	var o opts
	for _, fn := range options {
		fn(&o)
	}
	return o
}

func (o *opts) wd() (string, error) {
	if o.startDir != "" {
		return o.startDir, nil
	}
	return os.Getwd()
}

// findFile searches dir for <name><ext> for each ext; returns the first found path.
func findFile(dir, name string, exts []string) string {
	for _, ext := range exts {
		candidate := filepath.Join(dir, name+ext)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// relDisplay returns a human-readable relative path for display, or "".
func relDisplay(root, prefix, foundPath string) string {
	if rel, err := filepath.Rel(root, foundPath); err == nil && !strings.HasPrefix(rel, "..") {
		return prefix + "/" + rel
	}
	return ""
}
