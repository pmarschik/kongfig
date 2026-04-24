//go:build windows

package discover

import (
	"context"
	"os"
	"path/filepath"
)

// platformUserDirs returns <base>/<app> subdirectories to search for user config
// files on Windows.
// Search order: %APPDATA%\<app>.
func platformUserDirs(app string) []string {
	var dirs []string
	if appData := os.Getenv("APPDATA"); appData != "" {
		dirs = append(dirs, filepath.Join(appData, app))
	}
	return dirs
}

// platformSystemDirs returns <base>/<app> subdirectories to search for system
// config files on Windows.
// Search order: %ProgramData%\<app>.
func platformSystemDirs(app string) []string {
	var dirs []string
	if programData := os.Getenv("ProgramData"); programData != "" {
		dirs = append(dirs, filepath.Join(programData, app))
	}
	return dirs
}

// platformUserDisplayPath returns a symbolic display path for foundPath.
// Short mode (default): $appdata (token only).
// Long mode ([WithLongDisplayPaths]): %APPDATA%\<path>.
func platformUserDisplayPath(ctx context.Context, _ string, foundPath string) string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return ""
	}
	if !symPathContains(appData, foundPath) {
		return ""
	}
	if displayPathIsLong(ctx) {
		if p := symPath(appData, `%APPDATA%`, foundPath); p != "" {
			return p
		}
	}
	return `%APPDATA%`
}

// platformSystemDisplayPath returns a symbolic display path for foundPath.
// Short mode (default): $progdata (token only).
// Long mode ([WithLongDisplayPaths]): %ProgramData%\<path>.
func platformSystemDisplayPath(ctx context.Context, _, foundPath string) string {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		return foundPath
	}
	if !symPathContains(programData, foundPath) {
		return foundPath
	}
	if displayPathIsLong(ctx) {
		if p := symPath(programData, `%ProgramData%`, foundPath); p != "" {
			return p
		}
	}
	return `%ProgramData%`
}
