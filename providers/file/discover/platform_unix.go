//go:build !windows && !darwin

package discover

import (
	"os"
	"path/filepath"
)

// platformUserBaseDirs returns user-level config base directories on Linux/Unix,
// without the appname component.
// Search order: $XDG_CONFIG_HOME, ~/.config.
func platformUserBaseDirs() []DirEntry {
	var entries []DirEntry
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		entries = append(entries, DirEntry{xdg, "$xdg", "$XDG_CONFIG_HOME"})
	}
	if home, err := os.UserHomeDir(); err == nil {
		entries = append(entries, DirEntry{filepath.Join(home, ".config"), "~/.config", "~/.config"})
	}
	return entries
}

// platformSystemBaseDirs returns system-level config base directories on Linux/Unix,
// without the appname component.
func platformSystemBaseDirs() []DirEntry {
	return []DirEntry{{"/etc", "/etc", "/etc"}}
}

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
