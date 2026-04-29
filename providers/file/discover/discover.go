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
	"strings"

	kongfig "github.com/pmarschik/kongfig"
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

// xdgDiscoverer searches XDG config directories for a config file.
type xdgDiscoverer struct{}

// XDG returns a Discoverer that searches:
//  1. $XDG_CONFIG_HOME/<app>/config.<ext>
//  2. ~/.config/<app>/config.<ext>
//
// The app name is read from ctx via [kongfig.AppName]. Returns no results if no
// app name is set in ctx.
func XDG() *xdgDiscoverer { return &xdgDiscoverer{} } //nolint:revive // returning concrete type allows callers to chain methods

func (*xdgDiscoverer) Name() string { return "xdg" }

func (*xdgDiscoverer) Discover(ctx context.Context, exts []string) (string, error) {
	app := kongfig.AppName(ctx)
	if app == "" {
		return "", nil
	}

	var dirs []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		dirs = append(dirs, filepath.Join(xdg, app))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config", app))
	}

	for _, dir := range dirs {
		if p := findFile(dir, "config", exts); p != "" {
			return p, nil
		}
	}
	return "", nil
}

// DisplayPath formats the found path for human-friendly display.
// Short mode (default): returns a concise token ($xdg or ~/.config).
// Long mode ([WithLongDisplayPaths]): returns the full path.
// Returns "" if the path is not under any known XDG directory.
func (*xdgDiscoverer) DisplayPath(ctx context.Context, foundPath string) string {
	long := DisplayPathIsLong(ctx)
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		if symPathContains(xdg, foundPath) {
			if long {
				return symPath(xdg, "$XDG_CONFIG_HOME", foundPath)
			}
			return "$xdg"
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		configDir := filepath.Join(home, ".config")
		if symPathContains(configDir, foundPath) {
			if long {
				return symPath(configDir, "~/.config", foundPath)
			}
			return "~/.config"
		}
	}
	return ""
}

// workdirDiscoverer searches the current working directory.
type workdirDiscoverer struct{}

// Workdir returns a Discoverer that searches ./config.<ext>.
func Workdir() *workdirDiscoverer { return &workdirDiscoverer{} } //nolint:revive // returning concrete type allows callers to chain methods

func (*workdirDiscoverer) Name() string { return "workdir" }

func (*workdirDiscoverer) Discover(_ context.Context, exts []string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if p := findFile(wd, "config", exts); p != "" {
		return p, nil
	}
	return "", nil
}

// DisplayPath formats the found path relative to the working directory.
// Short mode (default): returns "$workdir". Long mode ([WithLongDisplayPaths]): returns the relative path.
// Returns "" if the relative path cannot be determined.
func (*workdirDiscoverer) DisplayPath(ctx context.Context, foundPath string) string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(wd, foundPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	if !DisplayPathIsLong(ctx) {
		return "$workdir"
	}
	return "./" + rel
}

// gitRootDiscoverer searches the git repository root by walking up the filesystem.
type gitRootDiscoverer struct {
	startDir string // override for os.Getwd(); set via FromDir
	maxDepth int
}

// GitRoot returns a Discoverer that searches <git-root>/config.<ext>.
// It walks up from the current directory looking for a .git entry (directory or file).
// Returns ("", nil) if not inside a git repository within maxDepth parent directories.
// maxDepth <= 0 uses the default (20).
func GitRoot(maxDepth ...int) *gitRootDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	d := &gitRootDiscoverer{}
	if len(maxDepth) > 0 {
		d.maxDepth = maxDepth[0]
	}
	return d
}

// FromDir overrides the starting directory for the upward search.
// By default os.Getwd() is used.
func (d *gitRootDiscoverer) FromDir(dir string) *gitRootDiscoverer {
	d.startDir = dir
	return d
}

func (*gitRootDiscoverer) Name() string { return "git-root" }

func (d *gitRootDiscoverer) wd() (string, error) {
	if d.startDir != "" {
		return d.startDir, nil
	}
	return os.Getwd()
}

func (d *gitRootDiscoverer) Discover(_ context.Context, exts []string) (string, error) {
	wd, err := d.wd()
	if err != nil {
		return "", err
	}
	root := findVCSRoot(wd, ".git", d.maxDepth)
	if root == "" {
		return "", nil
	}
	if p := findFile(root, "config", exts); p != "" {
		return p, nil
	}
	return "", nil
}

// DisplayPath formats the found path relative to the git repository root.
// Short mode (default): returns "$git-root". Long mode ([WithLongDisplayPaths]): returns the relative path.
func (d *gitRootDiscoverer) DisplayPath(ctx context.Context, foundPath string) string {
	wd, err := d.wd()
	if err != nil {
		return ""
	}
	root := findVCSRoot(wd, ".git", d.maxDepth)
	if root == "" {
		return ""
	}
	rel, err := filepath.Rel(root, foundPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	if !DisplayPathIsLong(ctx) {
		return "$git-root"
	}
	return "(git root)/" + rel
}

// jujutsuRootDiscoverer searches the Jujutsu repository root by walking up the filesystem.
type jujutsuRootDiscoverer struct {
	startDir string // override for os.Getwd(); set via FromDir
	maxDepth int
}

// JujutsuRoot returns a Discoverer that searches <jj-root>/config.<ext>.
// It walks up from the current directory looking for a .jj directory.
// Returns ("", nil) if not inside a Jujutsu repository within maxDepth parent directories.
// maxDepth <= 0 uses the default (20).
func JujutsuRoot(maxDepth ...int) *jujutsuRootDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	d := &jujutsuRootDiscoverer{}
	if len(maxDepth) > 0 {
		d.maxDepth = maxDepth[0]
	}
	return d
}

// FromDir overrides the starting directory for the upward search.
// By default os.Getwd() is used.
func (d *jujutsuRootDiscoverer) FromDir(dir string) *jujutsuRootDiscoverer {
	d.startDir = dir
	return d
}

func (*jujutsuRootDiscoverer) Name() string { return "jj-root" }

func (d *jujutsuRootDiscoverer) wd() (string, error) {
	if d.startDir != "" {
		return d.startDir, nil
	}
	return os.Getwd()
}

func (d *jujutsuRootDiscoverer) Discover(_ context.Context, exts []string) (string, error) {
	wd, err := d.wd()
	if err != nil {
		return "", err
	}
	root := findVCSRoot(wd, ".jj", d.maxDepth)
	if root == "" {
		return "", nil
	}
	if p := findFile(root, "config", exts); p != "" {
		return p, nil
	}
	return "", nil
}

// DisplayPath formats the found path relative to the Jujutsu repository root.
// Short mode (default): returns "$jj-root". Long mode ([WithLongDisplayPaths]): returns the relative path.
func (d *jujutsuRootDiscoverer) DisplayPath(ctx context.Context, foundPath string) string {
	wd, err := d.wd()
	if err != nil {
		return ""
	}
	root := findVCSRoot(wd, ".jj", d.maxDepth)
	if root == "" {
		return ""
	}
	rel, err := filepath.Rel(root, foundPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	if !DisplayPathIsLong(ctx) {
		return "$jj-root"
	}
	return "(jj root)/" + rel
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

// execDirDiscoverer searches the directory of the running executable.
type execDirDiscoverer struct{}

// ExecDir returns a Discoverer that searches the directory containing the running
// executable for a config file. If AppName is set in ctx via [kongfig.WithAppName],
// it searches <execdir>/<app>.<ext> first, then falls back to <execdir>/config.<ext>.
// If no AppName is set, only <execdir>/config.<ext> is searched.
//
// The executable path is resolved via [filepath.EvalSymlinks] so symlinked binaries
// (e.g. in /usr/local/bin) find config files next to the real binary.
//
// Returns ("", nil) if os.Executable fails or no config file is found.
func ExecDir() *execDirDiscoverer { return &execDirDiscoverer{} } //nolint:revive // returning concrete type allows callers to chain methods

func (*execDirDiscoverer) Name() string { return "execdir" }

func (*execDirDiscoverer) dir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe // best effort: use unresolved path
	}
	return filepath.Dir(resolved), nil
}

func (d *execDirDiscoverer) Discover(ctx context.Context, exts []string) (string, error) {
	dir, err := d.dir()
	if err != nil {
		return "", nil //nolint:nilerr // os.Executable failure is not an application error
	}
	if app := kongfig.AppName(ctx); app != "" {
		if p := findFile(dir, app, exts); p != "" {
			return p, nil
		}
	}
	if p := findFile(dir, "config", exts); p != "" {
		return p, nil
	}
	return "", nil
}

// DisplayPath formats the found path relative to the executable directory.
// Short mode (default): returns "$exec-dir". Long mode ([WithLongDisplayPaths]): returns the relative path.
func (d *execDirDiscoverer) DisplayPath(ctx context.Context, foundPath string) string {
	dir, err := d.dir()
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(dir, foundPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	if !DisplayPathIsLong(ctx) {
		return "$exec-dir"
	}
	return "(exec dir)/" + rel
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
