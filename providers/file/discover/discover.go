// Package discover provides [file.Discoverer] implementations for common config
// file search strategies: XDG base dirs, working directory, git repository root,
// and explicit paths.
//
// App name context:
//
//	ctx = kongfig.WithAppName(ctx, "myapp")
//	kf.MustLoad(fileprovider.Discover(ctx, discover.XDG(), yaml.Default))
package discover

import (
	"context"
	"os"
	"path/filepath"
	"slices"
)

// defaultMaxDepth is the default number of parent directories searched when
// looking for a VCS root marker (.git, .jj).
const defaultMaxDepth = 20

// findVCSRoot walks up from start looking for a directory containing marker.
// Returns the first matching directory path, or "" if not found within maxDepth steps.
func findVCSRoot(start, marker string, maxDepth int) string {
	if maxDepth <= 0 {
		maxDepth = defaultMaxDepth
	}
	dir := start
	for range maxDepth {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // filesystem root
		}
		dir = parent
	}
	return ""
}

// XDG returns a Discoverer that searches:
//  1. $XDG_CONFIG_HOME/<app>/config.<ext>
//  2. ~/.config/<app>/config.<ext>
//
// The app name is read from ctx via [kongfig.AppName]. Returns no results if no
// app name is set in ctx.
func XDG() *compositeDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	return Compose("xdg", XDGDirs(), LocateAppDir())
}

// Workdir returns a Discoverer that searches ./config.<ext>.
func Workdir() *compositeDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	return Compose("workdir", WorkdirDirs(), LocateConfigBase())
}

// ExecDir returns a Discoverer that searches the directory containing the running
// executable for a config file. If AppName is set in ctx via [kongfig.WithAppName],
// it searches <execdir>/<app>.<ext> first, then falls back to <execdir>/config.<ext>.
// If no AppName is set, only <execdir>/config.<ext> is searched.
//
// The executable path is resolved via [filepath.EvalSymlinks] so symlinked binaries
// (e.g. in /usr/local/bin) find config files next to the real binary.
//
// Returns ("", nil) if os.Executable fails or no config file is found.
func ExecDir() *compositeDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	return Compose("execdir", ExecDirs(), LocateFirst(LocateAppFlat(), LocateConfigBase()))
}

// vcsRootDiscoverer searches a VCS repository root by walking up the filesystem.
// Shared implementation for git and Jujutsu; instantiated via [GitRoot] and [JujutsuRoot].
type vcsRootDiscoverer struct {
	marker   string // VCS marker directory/file to search for (e.g. ".git", ".jj")
	name     string // discoverer name (e.g. "git-root", "jj-root")
	short    string // short display label (e.g. "$git-root", "$jj-root")
	long     string // long display label (e.g. "(git root)", "(jj root)")
	startDir string // override for os.Getwd(); set via FromDir
	maxDepth int
}

// GitRoot returns a Discoverer that searches <git-root>/config.<ext>.
// It walks up from the current directory looking for a .git entry (directory or file).
// Returns ("", nil) if not inside a git repository within maxDepth parent directories.
// maxDepth <= 0 uses the default (20).
func GitRoot(maxDepth ...int) *vcsRootDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	d := &vcsRootDiscoverer{marker: ".git", name: "git-root", short: "$git-root", long: "(git root)"}
	if len(maxDepth) > 0 {
		d.maxDepth = maxDepth[0]
	}
	return d
}

// JujutsuRoot returns a Discoverer that searches <jj-root>/config.<ext>.
// It walks up from the current directory looking for a .jj directory.
// Returns ("", nil) if not inside a Jujutsu repository within maxDepth parent directories.
// maxDepth <= 0 uses the default (20).
func JujutsuRoot(maxDepth ...int) *vcsRootDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	d := &vcsRootDiscoverer{marker: ".jj", name: "jj-root", short: "$jj-root", long: "(jj root)"}
	if len(maxDepth) > 0 {
		d.maxDepth = maxDepth[0]
	}
	return d
}

// FromDir overrides the starting directory for the upward search.
// By default os.Getwd() is used.
func (d *vcsRootDiscoverer) FromDir(dir string) *vcsRootDiscoverer {
	d.startDir = dir
	return d
}

// MaxDepth sets the maximum number of parent directories to walk.
func (d *vcsRootDiscoverer) MaxDepth(n int) *vcsRootDiscoverer {
	d.maxDepth = n
	return d
}

func (d *vcsRootDiscoverer) Name() string { return d.name }

func (d *vcsRootDiscoverer) wd() (string, error) {
	if d.startDir != "" {
		return d.startDir, nil
	}
	return os.Getwd()
}

func (d *vcsRootDiscoverer) dirEntries(_ context.Context) ([]DirEntry, error) {
	wd, err := d.wd()
	if err != nil {
		return nil, err
	}
	root := findVCSRoot(wd, d.marker, d.maxDepth)
	if root == "" {
		return nil, nil
	}
	return []DirEntry{{root, d.short, d.long}}, nil
}

func (d *vcsRootDiscoverer) Discover(ctx context.Context, exts []string) (string, error) {
	entries, err := d.dirEntries(ctx)
	if err != nil {
		return "", err
	}
	loc := LocateConfigBase()
	for _, entry := range entries {
		if p := loc(ctx, entry.Path, exts); p != "" {
			return p, nil
		}
	}
	return "", nil
}

// DisplayPath formats the found path relative to the VCS repository root.
// Short mode (default): returns the short label (e.g. "$git-root"). Long mode ([WithLongDisplayPaths]): returns the relative path.
func (d *vcsRootDiscoverer) DisplayPath(ctx context.Context, foundPath string) string {
	entries, err := d.dirEntries(ctx)
	if err != nil {
		return ""
	}
	return displayPathFromEntries(ctx, entries, foundPath)
}

// explicitDiscoverer wraps a user-provided path.
type explicitDiscoverer struct{ path string }

// Explicit returns a Discoverer for a known file path with extension matching.
// When parsers provide extensions via [ParserNamer], the file's extension must
// match one of them; otherwise Discover returns empty (no error). Use [ExplicitBase]
// when you know the location but want extension inference. Source label is "explicit.<format>".
func Explicit(path string) *explicitDiscoverer { return &explicitDiscoverer{path: path} } //nolint:revive // returning concrete type allows callers to chain methods

func (*explicitDiscoverer) Name() string { return "explicit" }

func (d *explicitDiscoverer) Discover(_ context.Context, exts []string) (string, error) {
	if len(exts) > 0 && !slices.Contains(exts, filepath.Ext(d.path)) {
		return "", nil
	}
	if info, err := os.Stat(d.path); err == nil && !info.IsDir() {
		return d.path, nil
	}
	return "", nil
}

// explicitBaseDiscoverer searches for a config file at a known base path plus extension.
type explicitBaseDiscoverer struct{ base string }

// ExplicitBase returns a Discoverer that probes <base>.<ext> for each extension
// the parser reports. Use this when the file location is known but the format
// should be inferred from the parsers passed to [file.Discover].
// Source label is "explicit.<format>".
//
// Example: ExplicitBase("/etc/myapp/config") tries /etc/myapp/config.yaml,
// /etc/myapp/config.toml, etc. depending on which parsers are passed.
func ExplicitBase(base string) *explicitBaseDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	return &explicitBaseDiscoverer{base: base}
}

func (*explicitBaseDiscoverer) Name() string { return "explicit" }

func (d *explicitBaseDiscoverer) Discover(_ context.Context, exts []string) (string, error) {
	if len(exts) == 0 {
		return "", nil
	}
	if p := findFile(filepath.Dir(d.base), filepath.Base(d.base), exts); p != "" {
		return p, nil
	}
	return "", nil
}

// findFile searches dir for <name><ext> for each ext; returns first found path.
func findFile(dir, name string, exts []string) string {
	for _, ext := range exts {
		candidate := filepath.Join(dir, name+ext)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}
