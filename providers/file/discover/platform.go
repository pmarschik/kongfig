package discover

import (
	"context"
	"path/filepath"

	kongfig "github.com/pmarschik/kongfig"
)

// FileStyle is a bitflag controlling which filename variants are searched by
// [UserDirs] and [SystemDirs]. Combine with |; zero value searches both.
type FileStyle int

const (
	// StyleSubdir searches <base>/<app>/<name>.<ext>.
	StyleSubdir FileStyle = 1 << iota
	// StyleFlat searches <base>/<app>.<ext>.
	StyleFlat
)

// userDirsDiscoverer searches OS-appropriate user config directories.
type userDirsDiscoverer struct {
	names []string  // subdirectory filenames to try; default ["config"]
	style FileStyle // which filename variants to search; default StyleBoth
}

// UserDirs returns a Discoverer that searches user-level config directories
// appropriate for the current operating system.
//
// For each OS-specific base directory, two filename variants are tried in order
// (controlled by [FileStyle] via [userDirsDiscoverer.WithStyle]):
//  1. <base>/<app>/<name>.<ext>  — canonical subdirectory style; <name> defaults to "config"
//  2. <base>/<app>.<ext>         — flat style (config file named after the app)
//
// Standard bases per OS (tried in order):
//
//	Linux/Unix:  $XDG_CONFIG_HOME, ~/.config
//	macOS:       $XDG_CONFIG_HOME, ~/.config, ~/Library/Application Support
//	Windows:     %APPDATA%
//
// The app name is read from ctx via [kongfig.AppName]. Returns no results if no
// app name is set in ctx.
func UserDirs() *userDirsDiscoverer { return &userDirsDiscoverer{} } //nolint:revive // returning concrete type allows callers to chain methods

// WithNames sets the subdirectory filenames searched in [StyleSubdir] and [StyleBoth] modes.
// Each name is tried as <base>/<app>/<name>.<ext>. Defaults to ["config"] when not set.
func (u *userDirsDiscoverer) WithNames(names ...string) *userDirsDiscoverer {
	u.names = names
	return u
}

// WithStyle restricts which filename variants are searched.
// Use [StyleSubdir], [StyleFlat], or [StyleSubdir]|[StyleFlat] (both, same as default).
func (u *userDirsDiscoverer) WithStyle(s FileStyle) *userDirsDiscoverer {
	u.style = s
	return u
}

func (*userDirsDiscoverer) Name() string { return "user-dirs" }

func (u *userDirsDiscoverer) Discover(ctx context.Context, exts []string) (string, error) {
	app := kongfig.AppName(ctx)
	if app == "" {
		return "", nil
	}
	names := u.names
	if len(names) == 0 {
		names = []string{"config"}
	}
	useSubdir := u.style == 0 || u.style&StyleSubdir != 0
	useFlat := u.style == 0 || u.style&StyleFlat != 0
	for _, dir := range platformUserDirs(app) {
		if useSubdir {
			for _, name := range names {
				if p := findFile(dir, name, exts); p != "" {
					return p, nil
				}
			}
		}
		if useFlat {
			if p := findFile(filepath.Dir(dir), app, exts); p != "" {
				return p, nil
			}
		}
	}
	return "", nil
}

// DisplayPath formats the found path with a human-friendly symbolic prefix.
// Short mode (default): returns a concise token such as $xdg, ~/Library/AS, %APPDATA%.
// Long mode ([WithLongDisplayPaths]): emits full path including app subdir and filename.
func (*userDirsDiscoverer) DisplayPath(ctx context.Context, foundPath string) string {
	app := kongfig.AppName(ctx)
	return platformUserDisplayPath(ctx, app, foundPath)
}

// systemDirsDiscoverer searches OS-appropriate system config directories.
type systemDirsDiscoverer struct {
	names []string  // subdirectory filenames to try; default ["config"]
	style FileStyle // which filename variants to search; default StyleBoth
}

// SystemDirs returns a Discoverer that searches system-level config directories
// appropriate for the current operating system.
//
// For each OS-specific base directory, two filename variants are tried in order
// (controlled by [FileStyle] via [systemDirsDiscoverer.WithStyle]):
//  1. <base>/<app>/<name>.<ext>  — canonical subdirectory style; <name> defaults to "config"
//  2. <base>/<app>.<ext>         — flat style
//
// Standard bases per OS (tried in order):
//
//	Linux/Unix:  /etc
//	macOS:       /etc, /usr/local/etc, /opt/homebrew/etc, /Library/Application Support
//	Windows:     %ProgramData%
//
// The app name is read from ctx via [kongfig.AppName]. Returns no results if no
// app name is set in ctx.
func SystemDirs() *systemDirsDiscoverer { return &systemDirsDiscoverer{} } //nolint:revive // returning concrete type allows callers to chain methods

// WithNames sets the subdirectory filenames searched in [StyleSubdir] and [StyleBoth] modes.
// Each name is tried as <base>/<app>/<name>.<ext>. Defaults to ["config"] when not set.
func (s *systemDirsDiscoverer) WithNames(names ...string) *systemDirsDiscoverer {
	s.names = names
	return s
}

// WithStyle restricts which filename variants are searched.
// Use [StyleSubdir], [StyleFlat], or [StyleSubdir]|[StyleFlat] (both, same as default).
func (s *systemDirsDiscoverer) WithStyle(style FileStyle) *systemDirsDiscoverer {
	s.style = style
	return s
}

func (*systemDirsDiscoverer) Name() string { return "system-dirs" }

func (s *systemDirsDiscoverer) Discover(ctx context.Context, exts []string) (string, error) {
	app := kongfig.AppName(ctx)
	if app == "" {
		return "", nil
	}
	names := s.names
	if len(names) == 0 {
		names = []string{"config"}
	}
	useSubdir := s.style == 0 || s.style&StyleSubdir != 0
	useFlat := s.style == 0 || s.style&StyleFlat != 0
	for _, dir := range platformSystemDirs(app) {
		if useSubdir {
			for _, name := range names {
				if p := findFile(dir, name, exts); p != "" {
					return p, nil
				}
			}
		}
		if useFlat {
			if p := findFile(filepath.Dir(dir), app, exts); p != "" {
				return p, nil
			}
		}
	}
	return "", nil
}

// DisplayPath returns the found path, with a platform-appropriate symbolic prefix.
// Short mode (default): concise token (e.g. /etc, %ProgramData%). Long mode ([WithLongDisplayPaths]):
// full path including app subdir and filename.
func (*systemDirsDiscoverer) DisplayPath(ctx context.Context, foundPath string) string {
	app := kongfig.AppName(ctx)
	return platformSystemDisplayPath(ctx, app, foundPath)
}
