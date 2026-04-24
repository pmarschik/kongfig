//go:build darwin

package discover

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

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

// platformUserDisplayPath returns a symbolic display path for foundPath on macOS.
// Short mode (default): concise token — $xdg, ~/.config, ~/Library/AS.
// Long mode ([WithLongDisplayPaths]): full path — $XDG_CONFIG_HOME/<path>, ~/.config/<path>,
// ~/Library/Application Support/<path>.
func platformUserDisplayPath(ctx context.Context, _, foundPath string) string {
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
	if symPathContains(filepath.Join(home, ".config"), foundPath) {
		if long {
			p := symPath(filepath.Join(home, ".config"), "~/.config", foundPath)
			return p
		}
		return "~/.config"
	}
	if symPathContains(filepath.Join(home, "Library", "Application Support"), foundPath) {
		if long {
			p := symPath(filepath.Join(home, "Library", "Application Support"), "~/Library/Application Support", foundPath)
			return p
		}
		return "~/Library/AS"
	}
	return ""
}

// platformSystemDisplayPath returns a display path for system config paths on macOS.
// Long mode: paths returned as-is.
// Short mode: /Library/Application Support abbreviated to /Library/AS; /usr/local/etc and
// /opt/homebrew/etc abbreviated to /brew/etc.
func platformSystemDisplayPath(ctx context.Context, _, foundPath string) string {
	if displayPathIsLong(ctx) {
		return foundPath
	}
	if after, ok := strings.CutPrefix(foundPath, "/Library/Application Support/"); ok {
		return "/Library/AS/" + after
	}
	if after, ok := strings.CutPrefix(foundPath, "/usr/local/etc/"); ok {
		return "/brew/etc/" + after
	}
	if after, ok := strings.CutPrefix(foundPath, "/opt/homebrew/etc/"); ok {
		return "/brew/etc/" + after
	}
	return foundPath
}
