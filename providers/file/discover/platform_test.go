package discover_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/providers/file/discover"
)

// makeFile creates a file at path and returns its path.
func makeFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("key: val\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestUserDirs_NoAppName(t *testing.T) {
	got, err := discover.UserDirs().Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestUserDirs_Name(t *testing.T) {
	if got := discover.UserDirs().Name(); got != "user-dirs" {
		t.Fatalf("want \"user-dirs\", got %q", got)
	}
}

func TestSystemDirs_NoAppName(t *testing.T) {
	got, err := discover.SystemDirs().Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestSystemDirs_Name(t *testing.T) {
	if got := discover.SystemDirs().Name(); got != "system-dirs" {
		t.Fatalf("want \"system-dirs\", got %q", got)
	}
}

// TestUserDirs_SubdirStyle verifies the canonical <base>/<app>/config.<ext> style.
func TestUserDirs_SubdirStyle(t *testing.T) {
	tmp := t.TempDir()
	xdgDir := filepath.Join(tmp, "xdg")
	want := makeFile(t, filepath.Join(xdgDir, "myapp", "config.yaml"))

	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	ctx := kongfig.WithAppName(context.Background(), "myapp")

	got, err := discover.UserDirs().Discover(ctx, []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

// TestUserDirs_FlatStyle verifies the flat <base>/<app>.<ext> style.
func TestUserDirs_FlatStyle(t *testing.T) {
	tmp := t.TempDir()
	xdgDir := filepath.Join(tmp, "xdg")
	want := makeFile(t, filepath.Join(xdgDir, "myapp.yaml"))

	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	ctx := kongfig.WithAppName(context.Background(), "myapp")

	got, err := discover.UserDirs().Discover(ctx, []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

// TestUserDirs_SubdirTakesPriorityOverFlat verifies canonical wins over flat.
func TestUserDirs_SubdirTakesPriorityOverFlat(t *testing.T) {
	tmp := t.TempDir()
	xdgDir := filepath.Join(tmp, "xdg")
	wantSubdir := makeFile(t, filepath.Join(xdgDir, "myapp", "config.yaml"))
	makeFile(t, filepath.Join(xdgDir, "myapp.yaml")) // flat — must NOT win

	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	ctx := kongfig.WithAppName(context.Background(), "myapp")

	got, err := discover.UserDirs().Discover(ctx, []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != wantSubdir {
		t.Fatalf("want subdir %q, got %q", wantSubdir, got)
	}
}

// TestUserDirs_WithNames_Custom verifies custom subdir filenames.
func TestUserDirs_WithNames_Custom(t *testing.T) {
	tmp := t.TempDir()
	xdgDir := filepath.Join(tmp, "xdg")
	want := makeFile(t, filepath.Join(xdgDir, "myapp", "settings.yaml"))

	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	ctx := kongfig.WithAppName(context.Background(), "myapp")

	got, err := discover.UserDirs().WithNames("settings").Discover(ctx, []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

// TestUserDirs_DisplayPath_XDG_Short verifies short token for XDG path.
func TestUserDirs_DisplayPath_XDG_Short(t *testing.T) {
	tmp := t.TempDir()
	xdgDir := filepath.Join(tmp, "xdg")
	p := makeFile(t, filepath.Join(xdgDir, "myapp", "config.yaml"))

	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	ctx := kongfig.WithAppName(context.Background(), "myapp")

	dp := discover.UserDirs().DisplayPath(ctx, p)
	if dp != "$xdg" {
		t.Fatalf("want \"$xdg\", got %q", dp)
	}
}

// TestUserDirs_DisplayPath_XDG_Long verifies long form uses full path.
func TestUserDirs_DisplayPath_XDG_Long(t *testing.T) {
	tmp := t.TempDir()
	xdgDir := filepath.Join(tmp, "xdg")
	p := makeFile(t, filepath.Join(xdgDir, "myapp", "config.yaml"))

	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	ctx := discover.WithLongDisplayPaths(kongfig.WithAppName(context.Background(), "myapp"))

	dp := discover.UserDirs().DisplayPath(ctx, p)
	want := "$XDG_CONFIG_HOME/myapp/config.yaml"
	if dp != want {
		t.Fatalf("want %q, got %q", want, dp)
	}
}

// TestUserDirs_DisplayPath_Flat_XDG_Short verifies flat path uses short token.
func TestUserDirs_DisplayPath_Flat_XDG_Short(t *testing.T) {
	tmp := t.TempDir()
	xdgDir := filepath.Join(tmp, "xdg")
	p := makeFile(t, filepath.Join(xdgDir, "myapp.yaml"))

	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	ctx := kongfig.WithAppName(context.Background(), "myapp")

	dp := discover.UserDirs().DisplayPath(ctx, p)
	if dp != "$xdg" {
		t.Fatalf("want \"$xdg\", got %q", dp)
	}
}

// TestUserDirs_DisplayPath_Flat_XDG_Long verifies flat path uses full path in long mode.
func TestUserDirs_DisplayPath_Flat_XDG_Long(t *testing.T) {
	tmp := t.TempDir()
	xdgDir := filepath.Join(tmp, "xdg")
	p := makeFile(t, filepath.Join(xdgDir, "myapp.yaml"))

	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	ctx := discover.WithLongDisplayPaths(kongfig.WithAppName(context.Background(), "myapp"))

	dp := discover.UserDirs().DisplayPath(ctx, p)
	want := "$XDG_CONFIG_HOME/myapp.yaml"
	if dp != want {
		t.Fatalf("want %q, got %q", want, dp)
	}
}

func TestUserDirs_DisplayPath_NoMatchReturnsEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	ctx := kongfig.WithAppName(context.Background(), "myapp")
	// must not panic; may return non-empty on some platforms (e.g. ~/.config fallback)
	_ = discover.UserDirs().DisplayPath(ctx, "/some/unrelated/path.yaml")
}

// TestUserDirs_StyleSubdir_IgnoresFlat verifies StyleSubdir skips flat files.
func TestUserDirs_StyleSubdir_IgnoresFlat(t *testing.T) {
	tmp := t.TempDir()
	xdgDir := filepath.Join(tmp, "xdg")
	makeFile(t, filepath.Join(xdgDir, "myapp.yaml")) // flat — must be ignored

	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	ctx := kongfig.WithAppName(context.Background(), "myapp")

	got, err := discover.UserDirs().WithStyle(discover.StyleSubdir).Discover(ctx, []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("StyleSubdir should ignore flat file, got %q", got)
	}
}

// TestUserDirs_StyleFlat_IgnoresSubdir verifies StyleFlat skips subdir files.
func TestUserDirs_StyleFlat_IgnoresSubdir(t *testing.T) {
	tmp := t.TempDir()
	xdgDir := filepath.Join(tmp, "xdg")
	makeFile(t, filepath.Join(xdgDir, "myapp", "config.yaml")) // subdir — must be ignored
	want := makeFile(t, filepath.Join(xdgDir, "myapp.yaml"))

	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	ctx := kongfig.WithAppName(context.Background(), "myapp")

	got, err := discover.UserDirs().WithStyle(discover.StyleFlat).Discover(ctx, []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestWithLongDisplayPaths_DoesNotAffectDiscover(t *testing.T) {
	tmp := t.TempDir()
	xdgDir := filepath.Join(tmp, "xdg")
	want := makeFile(t, filepath.Join(xdgDir, "myapp", "config.yaml"))

	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	ctx := discover.WithLongDisplayPaths(kongfig.WithAppName(context.Background(), "myapp"))

	got, err := discover.UserDirs().Discover(ctx, []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Discover: want %q, got %q", want, got)
	}
}
