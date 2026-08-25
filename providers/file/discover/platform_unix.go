//go:build !windows && !darwin

package discover

import (
	"os"
	"path/filepath"
)

// platformUserBaseDirs returns user-level config base directories on Linux/Unix,
// without the appname component.
// Search order: $XDG_CONFIG_HOME when that variable is set, otherwise ~/.config.
func platformUserBaseDirs() []DirEntry {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return []DirEntry{{xdg, "$xdg", "$XDG_CONFIG_HOME"}}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return []DirEntry{{filepath.Join(home, ".config"), "~/.config", "~/.config"}}
	}
	return nil
}

// platformSystemBaseDirs returns system-level config base directories on Linux/Unix,
// without the appname component.
func platformSystemBaseDirs() []DirEntry {
	return []DirEntry{{"/etc", "/etc", "/etc"}}
}

// platformUserDirs returns <base>/<app> subdirectories to search for user config
// files on Linux and other Unix-like systems.
// Search order: $XDG_CONFIG_HOME/<app> when that variable is set, otherwise
// ~/.config/<app>.
// XDG_CONFIG_HOME names the single directory user config lives in, so it replaces
// the home directory search rather than being tried ahead of it. ~/.config is the
// fallback the XDG spec defines for an unset variable.
func platformUserDirs(app string) []string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return []string{filepath.Join(xdg, app)}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return []string{filepath.Join(home, ".config", app)}
	}
	return nil
}

// platformSystemDirs returns <base>/<app> subdirectories to search for system
// config files on Linux and other Unix-like systems.
// Search order: /etc/<app>.
func platformSystemDirs(app string) []string {
	return []string{"/etc/" + app}
}
