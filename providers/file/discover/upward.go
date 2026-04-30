package discover

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// upwardDiscoverer walks up the directory tree, trying a FileLocator at each level.
type upwardDiscoverer struct {
	match    FileLocator
	startDir string
	maxDepth int
}

// UpwardFunc returns an upwardDiscoverer that uses the given FileLocator at each
// directory level while walking upward.
func UpwardFunc(match FileLocator) *upwardDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	return &upwardDiscoverer{match: match}
}

// FromDir overrides the starting directory for the upward search.
// By default os.Getwd() is used.
func (d *upwardDiscoverer) FromDir(dir string) *upwardDiscoverer {
	d.startDir = dir
	return d
}

// MaxDepth sets the maximum number of parent directories to walk.
// Values <= 0 use the default (20).
func (d *upwardDiscoverer) MaxDepth(n int) *upwardDiscoverer {
	d.maxDepth = n
	return d
}

// Name returns "upward".
func (*upwardDiscoverer) Name() string { return "upward" }

func (d *upwardDiscoverer) wd() (string, error) {
	if d.startDir != "" {
		return d.startDir, nil
	}
	return os.Getwd()
}

// Discover walks upward from the start directory, trying match at each level.
// Returns the first found path, or ("", nil) if nothing was found within maxDepth.
func (d *upwardDiscoverer) Discover(ctx context.Context, exts []string) (string, error) {
	wd, err := d.wd()
	if err != nil {
		return "", err
	}
	depth := d.maxDepth
	if depth <= 0 {
		depth = defaultMaxDepth
	}
	dir := wd
	for range depth {
		if p := d.match(ctx, dir, exts); p != "" {
			return p, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // filesystem root
		}
		dir = parent
	}
	return "", nil
}

// DisplayPath formats the found path relative to the start directory.
// Short mode: returns "$upward". Long mode: returns a relative path from startDir/cwd.
func (d *upwardDiscoverer) DisplayPath(ctx context.Context, foundPath string) string {
	wd, err := d.wd()
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(wd, foundPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	if !DisplayPathIsLong(ctx) {
		return "$upward"
	}
	return "./" + rel
}

// UpwardConfigBase returns an upwardDiscoverer that uses LocateConfigBase.
func UpwardConfigBase() *upwardDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	return UpwardFunc(LocateConfigBase())
}

// UpwardAppDir returns an upwardDiscoverer that uses LocateAppDir.
func UpwardAppDir() *upwardDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	return UpwardFunc(LocateAppDir())
}

// UpwardAppFlat returns an upwardDiscoverer that uses LocateAppFlat.
func UpwardAppFlat() *upwardDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	return UpwardFunc(LocateAppFlat())
}

// UpwardApp returns an upwardDiscoverer that tries LocateAppDir then LocateAppFlat.
func UpwardApp() *upwardDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	return UpwardFunc(LocateFirst(LocateAppDir(), LocateAppFlat()))
}
