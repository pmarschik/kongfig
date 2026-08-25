//go:build darwin

package discover

import (
	"os"
	"path/filepath"
)

// platformUserBaseDirs returns user-level config base directories on macOS,
// without the appname component.
// Search order: $XDG_CONFIG_HOME when that variable is set, otherwise ~/.config
// and ~/Library/Application Support.
func platformUserBaseDirs() []DirEntry {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return []DirEntry{{xdg, "$xdg", "$XDG_CONFIG_HOME"}}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return []DirEntry{
			{filepath.Join(home, ".config"), "~/.config", "~/.config"},
			{filepath.Join(home, "Library", "Application Support"), "~/Library/AS", "~/Library/Application Support"},
		}
	}
	return nil
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
// Search order: $XDG_CONFIG_HOME/<app> when that variable is set, otherwise
// ~/.config/<app> and ~/Library/Application Support/<app>.
// XDG_CONFIG_HOME names the single directory user config lives in, so it replaces
// the home directory search rather than being tried ahead of it. The two home
// locations are the fallback the XDG spec defines for an unset variable, plus the
// native macOS one.
func platformUserDirs(app string) []string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return []string{filepath.Join(xdg, app)}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return []string{filepath.Join(home, ".config", app), filepath.Join(home, "Library", "Application Support", app)}
	}
	return nil
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
