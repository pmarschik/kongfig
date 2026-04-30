//go:build darwin

package discover

import (
	"os"
	"path/filepath"
)

// platformUserBaseDirs returns user-level config base directories on macOS,
// without the appname component.
// Search order: $XDG_CONFIG_HOME, ~/.config, ~/Library/Application Support.
func platformUserBaseDirs() []DirEntry {
	var entries []DirEntry
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		entries = append(entries, DirEntry{xdg, "$xdg", "$XDG_CONFIG_HOME"})
	}
	if home, err := os.UserHomeDir(); err == nil {
		entries = append(entries,
			DirEntry{filepath.Join(home, ".config"), "~/.config", "~/.config"},
			DirEntry{filepath.Join(home, "Library", "Application Support"), "~/Library/AS", "~/Library/Application Support"},
		)
	}
	return entries
}

// platformSystemBaseDirs returns system-level config base directories on macOS,
// without the appname component.
func platformSystemBaseDirs() []DirEntry {
	return []DirEntry{
		{"/etc", "/etc", "/etc"},
		{"/usr/local/etc", "/brew/etc", "/usr/local/etc"},
		{"/opt/homebrew/etc", "/brew/etc", "/opt/homebrew/etc"},
		{"/Library/Application Support", "/Library/AS", "/Library/Application Support"},
	}
}

// platformUserDirs returns <base>/<app> subdirectories to search for user config
// files on macOS.
// Search order: $XDG_CONFIG_HOME/<app>, ~/.config/<app>, ~/Library/Application Support/<app>.
// XDG dirs are checked first for portability; Application Support is the native macOS location.
func platformUserDirs(app string) []string {
	var dirs []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		dirs = append(dirs, filepath.Join(xdg, app))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config", app), filepath.Join(home, "Library", "Application Support", app))
	}
	return dirs
}

// platformSystemDirs returns <base>/<app> subdirectories to search for system
// config files on macOS.
// Search order:
//  1. /etc/<app>
//  2. /usr/local/etc/<app>  (Homebrew on Intel)
//  3. /opt/homebrew/etc/<app>  (Homebrew on Apple Silicon)
//  4. /Library/Application Support/<app>  (system-wide macOS convention)
func platformSystemDirs(app string) []string {
	return []string{
		"/etc/" + app,
		"/usr/local/etc/" + app,
		"/opt/homebrew/etc/" + app,
		"/Library/Application Support/" + app,
	}
}
