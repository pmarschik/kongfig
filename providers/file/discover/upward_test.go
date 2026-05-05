package discover_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/providers/file/discover"
)

// --- ComposeAll ---

func TestComposeAll_Length(t *testing.T) {
	dirs := staticDirs(t.TempDir())
	locs := []discover.FileLocator{
		discover.LocateConfigBase(),
		discover.LocateAppFlat(),
		discover.LocateAppDir(),
	}
	got := discover.ComposeAll("myname", dirs, locs...)
	if len(got) != len(locs) {
		t.Errorf("ComposeAll length: want %d, got %d", len(locs), len(got))
	}
}

func TestComposeAll_SharedName(t *testing.T) {
	dirs := staticDirs(t.TempDir())
	got := discover.ComposeAll("shared",
		dirs,
		discover.LocateConfigBase(),
		discover.LocateAppFlat(),
	)
	for i, d := range got {
		if d.Name() != "shared" {
			t.Errorf("element %d: Name() = %q, want %q", i, d.Name(), "shared")
		}
	}
}

// TestComposeAll_EachLocatorIsIndependent verifies that each element uses its
// own FileLocator: the nth element finds the nth file and not others.
func TestComposeAll_EachLocatorIsIndependent(t *testing.T) {
	tmp := t.TempDir()

	// File A: matches LocateConfigBase ("config.yaml")
	fileA := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(fileA, []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}

	// File B: matches LocateAppFlat ("myapp.yaml")
	fileB := filepath.Join(tmp, "myapp.yaml")
	if err := os.WriteFile(fileB, []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := kongfig.WithAppName(context.Background(), "myapp")
	dirs := staticDirs(tmp)

	got := discover.ComposeAll("test",
		dirs,
		discover.LocateConfigBase(), // index 0 → fileA
		discover.LocateAppFlat(),    // index 1 → fileB
	)

	if len(got) != 2 {
		t.Fatalf("want 2 discoverers, got %d", len(got))
	}

	path0, err := got[0].Discover(ctx, []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if path0 != fileA {
		t.Errorf("element 0: want %q, got %q", fileA, path0)
	}

	path1, err := got[1].Discover(ctx, []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if path1 != fileB {
		t.Errorf("element 1: want %q, got %q", fileB, path1)
	}
}

// TestComposeAll_SharedNameAmbigiousInFirstOf documents the known design quirk:
// because all elements from ComposeAll share the same name, FirstOf cannot
// distinguish the winner by name alone.
func TestComposeAll_SharedNameAmbiguousInFirstOf(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "myapp.yaml"), []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := kongfig.WithAppName(context.Background(), "myapp")
	dirs := staticDirs(tmp)

	discoverers := discover.ComposeAll("ambiguous",
		dirs,
		discover.LocateConfigBase(), // won't find anything (no config.yaml)
		discover.LocateAppFlat(),    // finds myapp.yaml
	)

	fo := discover.FirstOf(discoverers[0], discoverers[1])
	if _, err := fo.Discover(ctx, []string{".yaml"}); err != nil {
		t.Fatal(err)
	}
	// Both have name "ambiguous", so Name() == "ambiguous" regardless of winner.
	if fo.Name() != "ambiguous" {
		t.Errorf("Name() = %q, want %q (shared name)", fo.Name(), "ambiguous")
	}
}

// --- UpwardFunc ---

func TestUpwardFunc_FindsFileInCurrentDir(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(path, []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}

	d := discover.UpwardFunc(discover.LocateConfigBase()).FromDir(tmp)
	got, err := d.Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Errorf("want %q, got %q", path, got)
	}
}

func TestUpwardFunc_FindsFileOneLevelUp(t *testing.T) {
	tmp := t.TempDir()
	// Config file is at the top.
	path := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(path, []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Start directory is one level deeper.
	sub := filepath.Join(tmp, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}

	d := discover.UpwardFunc(discover.LocateConfigBase()).FromDir(sub)
	got, err := d.Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Errorf("want %q, got %q", path, got)
	}
}

func TestUpwardFunc_MaxDepthPreventsFind(t *testing.T) {
	tmp := t.TempDir()
	// Config file at root.
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Build a 3-level deep subdirectory; file is 3 parents up.
	deep := tmp
	for i := range 3 {
		deep = filepath.Join(deep, fmt.Sprintf("d%d", i))
	}
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}

	// MaxDepth(1) allows only the start dir itself; cannot reach 3 levels up.
	d := discover.UpwardFunc(discover.LocateConfigBase()).FromDir(deep).MaxDepth(1)
	got, err := d.Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty (MaxDepth too small), got %q", got)
	}
}

func TestUpwardFunc_FromDir_OverridesStart(t *testing.T) {
	// Two sibling directories; config is in dirA, start from dirB via FromDir.
	dirA := t.TempDir()
	dirB := t.TempDir()
	path := filepath.Join(dirA, "config.yaml")
	if err := os.WriteFile(path, []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Without FromDir pointing at dirA, UpwardFunc starting from dirB would not find it.
	// With FromDir(dirA) it finds it immediately.
	d := discover.UpwardFunc(discover.LocateConfigBase()).FromDir(dirA)
	got, err := d.Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Errorf("want %q, got %q", path, got)
	}

	// Sanity: starting from dirB does NOT find dirA's file.
	dB := discover.UpwardFunc(discover.LocateConfigBase()).FromDir(dirB).MaxDepth(1)
	gotB, err := dB.Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if gotB == path {
		t.Errorf("should not find dirA file when starting from dirB (sibling), got %q", gotB)
	}
}

func TestUpwardFunc_DisplayPath_Short(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sub", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}

	d := discover.UpwardFunc(discover.LocateConfigBase()).FromDir(tmp)
	if got := d.DisplayPath(context.Background(), path); got != "$upward" {
		t.Errorf("short mode: want %q, got %q", "$upward", got)
	}
}

func TestUpwardFunc_DisplayPath_Long(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "subdir")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "config.yaml")
	if err := os.WriteFile(path, []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}

	d := discover.UpwardFunc(discover.LocateConfigBase()).FromDir(tmp)
	ctx := discover.WithLongDisplayPaths(context.Background())
	want := "./subdir/config.yaml"
	if got := d.DisplayPath(ctx, path); got != want {
		t.Errorf("long mode: want %q, got %q", want, got)
	}
}

func TestUpwardFunc_DisplayPath_Long_CurrentDir(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(path, []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}

	d := discover.UpwardFunc(discover.LocateConfigBase()).FromDir(tmp)
	ctx := discover.WithLongDisplayPaths(context.Background())
	want := "./config.yaml"
	if got := d.DisplayPath(ctx, path); got != want {
		t.Errorf("long mode (same dir): want %q, got %q", want, got)
	}
}

func TestUpwardFunc_StopsAtFilesystemRoot(t *testing.T) {
	// Start from filesystem root — should not loop and should return "" cleanly.
	root := filepath.VolumeName("/") + "/"
	d := discover.UpwardFunc(discover.LocateConfigBase()).FromDir(root)
	got, err := d.Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	// We don't assert got == "" because a config.yaml might theoretically exist at
	// the filesystem root; we just assert it terminates without panic.
	_ = got
}

// --- Convenience wrappers smoke tests ---

func TestUpwardConfigBase_NonNilAndName(t *testing.T) {
	d := discover.UpwardConfigBase()
	if d == nil {
		t.Fatal("UpwardConfigBase() returned nil")
	}
	if got := d.Name(); got != "upward" {
		t.Errorf("Name() = %q, want %q", got, "upward")
	}
}

func TestUpwardAppFlat_NonNilAndName(t *testing.T) {
	d := discover.UpwardAppFlat()
	if d == nil {
		t.Fatal("UpwardAppFlat() returned nil")
	}
	if got := d.Name(); got != "upward" {
		t.Errorf("Name() = %q, want %q", got, "upward")
	}
}

func TestUpwardAppDir_NonNilAndName(t *testing.T) {
	d := discover.UpwardAppDir()
	if d == nil {
		t.Fatal("UpwardAppDir() returned nil")
	}
	if got := d.Name(); got != "upward" {
		t.Errorf("Name() = %q, want %q", got, "upward")
	}
}

func TestUpwardApp_NonNilAndName(t *testing.T) {
	d := discover.UpwardApp()
	if d == nil {
		t.Fatal("UpwardApp() returned nil")
	}
	if got := d.Name(); got != "upward" {
		t.Errorf("Name() = %q, want %q", got, "upward")
	}
}
