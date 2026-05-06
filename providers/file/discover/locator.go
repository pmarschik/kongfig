package discover

import (
	"context"
	"os"
	"path/filepath"

	kongfig "github.com/pmarschik/kongfig"
)

// DirEntry is a base directory with display labels.
type DirEntry struct {
	Path  string // absolute base path (no appname component)
	Short string // short display token: "$xdg", "~/.config", "$git-root"
	Long  string // symbolic prefix for long mode: "$XDG_CONFIG_HOME", "~/.config", "(git root)"
}

// DirProvider yields base directories to probe.
type DirProvider func(ctx context.Context) ([]DirEntry, error)

// FileLocator finds a config file within a single directory.
// Returns "" when not found.
type FileLocator func(ctx context.Context, dir string, exts []string) string

// LocateConfigBase returns a FileLocator that probes <dir>/<kongfig.ConfigBase(ctx)>.<ext>.
func LocateConfigBase() FileLocator {
	return func(ctx context.Context, dir string, exts []string) string {
		return findFile(dir, kongfig.ConfigBase(ctx), exts)
	}
}

// LocateAppFlat returns a FileLocator that probes <dir>/<app>.<ext>.
// If [kongfig.HiddenFiles] is set, also probes <dir>/.<app>.<ext>.
// Returns "" if AppName is not set in ctx.
func LocateAppFlat() FileLocator {
	return func(ctx context.Context, dir string, exts []string) string {
		app := kongfig.AppName(ctx)
		if app == "" {
			return ""
		}
		if p := findFile(dir, app, exts); p != "" {
			return p
		}
		if kongfig.HiddenFiles(ctx) {
			if p := findFile(dir, "."+app, exts); p != "" {
				return p
			}
		}
		return ""
	}
}

// LocateAppDir returns a FileLocator that probes <dir>/<app>/<configbase>.<ext>.
// If [kongfig.HiddenFiles] is set, also probes <dir>/.<app>/<configbase>.<ext>.
// Returns "" if AppName is not set in ctx.
func LocateAppDir() FileLocator {
	return func(ctx context.Context, dir string, exts []string) string {
		app := kongfig.AppName(ctx)
		if app == "" {
			return ""
		}
		base := kongfig.ConfigBase(ctx)
		if p := findFile(filepath.Join(dir, app), base, exts); p != "" {
			return p
		}
		if kongfig.HiddenFiles(ctx) {
			if p := findFile(filepath.Join(dir, "."+app), base, exts); p != "" {
				return p
			}
		}
		return ""
	}
}

// LocateNames returns a FileLocator that probes <dir>/<name><ext> for each name
// and each extension, trying names in order. Unlike [LocateConfigBase] and
// [LocateAppFlat], LocateNames uses the exact names given without deriving them
// from context.
//
//	discover.Compose("local", discover.WorkdirDirs(), discover.LocateNames(".yard.local", ".yard-demo-config"))
func LocateNames(names ...string) FileLocator {
	return func(_ context.Context, dir string, exts []string) string {
		for _, name := range names {
			if p := findFile(dir, name, exts); p != "" {
				return p
			}
		}
		return ""
	}
}

// LocateFirst returns a FileLocator that tries each locator in order and
// returns the first non-empty result.
func LocateFirst(locs ...FileLocator) FileLocator {
	return func(ctx context.Context, dir string, exts []string) string {
		for _, loc := range locs {
			if p := loc(ctx, dir, exts); p != "" {
				return p
			}
		}
		return ""
	}
}

// XDGDirs returns a DirProvider that yields XDG config base directories.
// Returns [$XDG_CONFIG_HOME (if set), ~/.config (if home available)].
func XDGDirs() DirProvider {
	return func(_ context.Context) ([]DirEntry, error) {
		var entries []DirEntry
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			entries = append(entries, DirEntry{xdg, "$xdg", "$XDG_CONFIG_HOME"})
		}
		if home, err := os.UserHomeDir(); err == nil {
			entries = append(entries, DirEntry{filepath.Join(home, ".config"), "~/.config", "~/.config"})
		}
		return entries, nil
	}
}

// WorkdirDirs returns a DirProvider that yields the current working directory.
func WorkdirDirs() DirProvider {
	return func(_ context.Context) ([]DirEntry, error) {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		return []DirEntry{{wd, "$workdir", "."}}, nil
	}
}

// GitRootDirs returns a DirProvider that walks up from cwd looking for a .git
// directory. Returns a single entry for the git root, or nil if not found.
func GitRootDirs(maxDepth int) DirProvider {
	return func(_ context.Context) ([]DirEntry, error) {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		root := findVCSRoot(wd, ".git", maxDepth)
		if root == "" {
			return nil, nil
		}
		return []DirEntry{{root, "$git-root", "(git root)"}}, nil
	}
}

// JJRootDirs returns a DirProvider that walks up from cwd looking for a .jj
// directory. Returns a single entry for the jj root, or nil if not found.
func JJRootDirs(maxDepth int) DirProvider {
	return func(_ context.Context) ([]DirEntry, error) {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		root := findVCSRoot(wd, ".jj", maxDepth)
		if root == "" {
			return nil, nil
		}
		return []DirEntry{{root, "$jj-root", "(jj root)"}}, nil
	}
}

// ExecDirs returns a DirProvider that yields the directory of the running executable.
// The executable path is resolved via [filepath.EvalSymlinks].
func ExecDirs() DirProvider {
	return func(_ context.Context) ([]DirEntry, error) {
		exe, err := os.Executable()
		if err != nil {
			return nil, nil // ignored: os.Executable failure is not an application error
		}
		resolved, err := filepath.EvalSymlinks(exe)
		if err != nil {
			resolved = exe
		}
		dir := filepath.Dir(resolved)
		return []DirEntry{{dir, "$exec-dir", "(exec dir)"}}, nil
	}
}
