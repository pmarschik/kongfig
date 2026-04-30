//go:build windows

package discover

import (
	"os"
	"path/filepath"
)

// platformUserBaseDirs returns user-level config base directories on Windows,
// without the appname component.
func platformUserBaseDirs() []DirEntry {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return []DirEntry{{appData, "%APPDATA%", "%APPDATA%"}}
	}
	return nil
}

// platformSystemBaseDirs returns system-level config base directories on Windows,
// without the appname component.
func platformSystemBaseDirs() []DirEntry {
	if programData := os.Getenv("ProgramData"); programData != "" {
		return []DirEntry{{programData, "%ProgramData%", "%ProgramData%"}}
	}
	return nil
}

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
