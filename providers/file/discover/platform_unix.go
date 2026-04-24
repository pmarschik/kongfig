//go:build !windows && !darwin

package discover

import (
	"context"
	"os"
	"path/filepath"
)

// platformUserDirs returns <base>/<app> subdirectories to search for user config
// files on Linux and other Unix-like systems.
// Search order: $XDG_CONFIG_HOME/<app>, ~/.config/<app>.
func platformUserDirs(app string) []string {
	var dirs []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		dirs = append(dirs, filepath.Join(xdg, app))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config", app))
	}
	return dirs
}

// platformSystemDirs returns <base>/<app> subdirectories to search for system
// config files on Linux and other Unix-like systems.
// Search order: /etc/<app>.
func platformSystemDirs(app string) []string {
	return []string{"/etc/" + app}
}

// platformUserDisplayPath returns a symbolic display path for foundPath.
// Short mode (default): $xdg or ~/.config (token only).
// Long mode ([WithLongDisplayPaths]): $XDG_CONFIG_HOME/<path> or ~/.config/<path>.
func platformUserDisplayPath(ctx context.Context, app, foundPath string) string {
	long := displayPathIsLong(ctx)

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		if symPathContains(xdg, foundPath) {
			if long {
				p := symPath(xdg, "$XDG_CONFIG_HOME", foundPath)
				return p
			}
			return "$xdg"
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	configDir := filepath.Join(home, ".config")
	if symPathContains(configDir, foundPath) {
		if long {
			p := symPath(configDir, "~/.config", foundPath)
			return p
		}
		return "~/.config"
	}
	_ = app // app is not needed for Unix short paths
	return ""
}

// platformSystemDisplayPath returns a display path for system config paths.
// On Unix, system paths are absolute and returned as-is.
func platformSystemDisplayPath(_ context.Context, _, foundPath string) string {
	return foundPath
}
