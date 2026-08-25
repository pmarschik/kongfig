//go:build !windows

package discover_test

import (
	"context"
	"path/filepath"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/providers/file/discover"
)

// XDG_CONFIG_HOME does not add a directory to the search, it names the one
// directory user config lives in. $HOME/.config is what the spec falls back to
// when the variable is unset, so searching it anyway turns an empty
// XDG_CONFIG_HOME into "keep looking" rather than "there is no user config".
//
// A consumer's own tests are where that bites: they point XDG_CONFIG_HOME at a
// t.TempDir() to get a clean slate, and discovery hands back the developer's
// real config instead. yard's setup tests went further and offered to overwrite
// it.
func TestUserDirs_XDGConfigHomeReplacesTheHomeFallback(t *testing.T) {
	home := t.TempDir()
	makeFile(t, filepath.Join(home, ".config", "myapp", "config.yaml"))
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ctx := kongfig.WithAppName(context.Background(), "myapp")
	got, err := discover.UserDirs().Discover(ctx, []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("discovery reached outside XDG_CONFIG_HOME and found %q", got)
	}
}

// UserBaseDirs and XDGDirs hand the same directory list to anyone composing
// their own discoverer, and DisplayPath turns it into the symbolic prefix it
// prints. All three follow the search, so an entry that discovery no longer
// reaches is one this list must not offer either.
func TestBaseDirProviders_XDGConfigHomeReplacesTheHomeFallback(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", xdg)

	for name, provider := range map[string]discover.DirProvider{
		"UserBaseDirs": discover.UserBaseDirs(),
		"XDGDirs":      discover.XDGDirs(),
	} {
		t.Run(name, func(t *testing.T) {
			entries, err := provider(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Path != xdg {
				t.Fatalf("want only %q, got %v", xdg, entries)
			}
		})
	}
}

// The fallback itself stays: an unset XDG_CONFIG_HOME is the case the spec
// defines $HOME/.config for, and that is how most machines are set up.
func TestUserDirs_HomeFallbackAppliesWhenXDGIsUnset(t *testing.T) {
	home := t.TempDir()
	want := makeFile(t, filepath.Join(home, ".config", "myapp", "config.yaml"))
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	ctx := kongfig.WithAppName(context.Background(), "myapp")
	got, err := discover.UserDirs().Discover(ctx, []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}
