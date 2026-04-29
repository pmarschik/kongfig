package discover_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pmarschik/kongfig/providers/file/discover"
)

func TestGitRoot_MaxDepthLimit(t *testing.T) {
	tmp := t.TempDir()

	// Place a .git directory at the root.
	if err := os.Mkdir(filepath.Join(tmp, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Create a config file at the git root.
	configFile := filepath.Join(tmp, "config.txt")
	if err := os.WriteFile(configFile, []byte("key: val\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Build a directory chain 3 levels deep inside the git root.
	deepDir := tmp
	for i := range 3 {
		deepDir = filepath.Join(deepDir, fmt.Sprintf("sub%d", i))
	}
	if err := os.MkdirAll(deepDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// maxDepth=5 is enough to find the root from 3 levels deep.
	d := discover.GitRoot(5).FromDir(deepDir)
	got, err := d.Discover(context.Background(), []string{".txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got != configFile {
		t.Errorf("expected %q, got %q", configFile, got)
	}
}

func TestGitRoot_MaxDepthStopsSearch(t *testing.T) {
	tmp := t.TempDir()

	// Place a .git directory at the root.
	if err := os.Mkdir(filepath.Join(tmp, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Create a config file at the git root.
	if err := os.WriteFile(filepath.Join(tmp, "config.txt"), []byte("key: val\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Build a directory chain 5 levels deep.
	deepDir := tmp
	for i := range 5 {
		deepDir = filepath.Join(deepDir, fmt.Sprintf("sub%d", i))
	}
	if err := os.MkdirAll(deepDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// maxDepth=2 means we only walk 2 parent levels — not enough to reach the git root
	// (which is 5 levels up). Should return empty path.
	d := discover.GitRoot(2).FromDir(deepDir)
	got, err := d.Discover(context.Background(), []string{".txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty path (depth limit), got %q", got)
	}
}

func TestGitRoot_NoGitRoot_ReturnsEmpty(t *testing.T) {
	tmp := t.TempDir()
	// No .git directory — should return empty.
	d := discover.GitRoot(5).FromDir(tmp)
	got, err := d.Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExplicit_ExistingFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(path, []byte("key: val\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := discover.Explicit(path).Discover(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Errorf("expected %q, got %q", path, got)
	}
}

func TestExplicit_ExtensionMatch_ReturnsPath(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(path, []byte("[section]\nkey = \"val\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := discover.Explicit(path).Discover(context.Background(), []string{".toml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Errorf("expected %q, got %q", path, got)
	}
}

func TestExplicit_ExtensionMismatch_ReturnsEmpty(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(path, []byte("[section]\nkey = \"val\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// YAML parser's extensions — should not match a .toml file.
	got, err := discover.Explicit(path).Discover(context.Background(), []string{".yaml", ".yml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty (extension mismatch), got %q", got)
	}
}

func TestExplicitBase_FindsMatchingExtension(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(path, []byte("[section]\nkey = \"val\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := discover.ExplicitBase(filepath.Join(tmp, "config")).Discover(context.Background(), []string{".toml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Errorf("expected %q, got %q", path, got)
	}
}

func TestExplicitBase_SkipsNonMatchingExtension(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte("[section]\nkey = \"val\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := discover.ExplicitBase(filepath.Join(tmp, "config")).Discover(context.Background(), []string{".yaml", ".yml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty (no yaml file present), got %q", got)
	}
}

func TestExplicitBase_NoExts_ReturnsEmpty(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte("[section]\nkey = \"val\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := discover.ExplicitBase(filepath.Join(tmp, "config")).Discover(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty when no exts provided, got %q", got)
	}
}

func TestExplicit_NonExistentFile_ReturnsEmpty(t *testing.T) {
	path := "/nonexistent/path/config.yaml"

	got, err := discover.Explicit(path).Discover(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	// Explicit returns ("", nil) when the file doesn't exist — not an error.
	if got != "" {
		t.Errorf("expected empty path for non-existent file, got %q", got)
	}
}

func TestExplicit_Directory_ReturnsEmpty(t *testing.T) {
	tmp := t.TempDir()
	// Pass a directory path — Explicit should not return it (not a file).
	got, err := discover.Explicit(tmp).Discover(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty for directory path, got %q", got)
	}
}

func TestXDG_DisplayPath_Short(t *testing.T) {
	tmp := t.TempDir()
	xdgDir := filepath.Join(tmp, "xdg")
	if err := os.MkdirAll(xdgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(xdgDir, "app", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("k: v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	if got := discover.XDG().DisplayPath(context.Background(), path); got != "$xdg" {
		t.Errorf("want $xdg, got %q", got)
	}
}

func TestXDG_DisplayPath_Long(t *testing.T) {
	tmp := t.TempDir()
	xdgDir := filepath.Join(tmp, "xdg")
	path := filepath.Join(xdgDir, "app", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("k: v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	ctx := discover.WithLongDisplayPaths(context.Background())
	want := "$XDG_CONFIG_HOME/app/config.yaml"
	if got := discover.XDG().DisplayPath(ctx, path); got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestWorkdir_DisplayPath_Short(t *testing.T) {
	tmp := t.TempDir()
	// EvalSymlinks so that os.Getwd() and the path agree after chdir.
	resolved, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(resolved, "config.yaml")
	if err := os.WriteFile(path, []byte("k: v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(resolved)
	if got := discover.Workdir().DisplayPath(context.Background(), path); got != "$workdir" {
		t.Errorf("want $workdir, got %q", got)
	}
}

func TestWorkdir_DisplayPath_Long(t *testing.T) {
	tmp := t.TempDir()
	// EvalSymlinks so that os.Getwd() and the path agree after chdir.
	resolved, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(resolved, "config.yaml")
	if err := os.WriteFile(path, []byte("k: v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(resolved)
	ctx := discover.WithLongDisplayPaths(context.Background())
	if got := discover.Workdir().DisplayPath(ctx, path); got != "./config.yaml" {
		t.Errorf("want ./config.yaml, got %q", got)
	}
}

func TestGitRoot_DisplayPath_Short(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(path, []byte("k: v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := discover.GitRoot(5).FromDir(tmp)
	if got := d.DisplayPath(context.Background(), path); got != "$git-root" {
		t.Errorf("want $git-root, got %q", got)
	}
}

func TestGitRoot_DisplayPath_Long(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(path, []byte("k: v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := discover.GitRoot(5).FromDir(tmp)
	ctx := discover.WithLongDisplayPaths(context.Background())
	if got := d.DisplayPath(ctx, path); got != "(git root)/config.yaml" {
		t.Errorf("want (git root)/config.yaml, got %q", got)
	}
}

func TestJujutsuRoot_DisplayPath_Short(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, ".jj"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(path, []byte("k: v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := discover.JujutsuRoot(5).FromDir(tmp)
	if got := d.DisplayPath(context.Background(), path); got != "$jj-root" {
		t.Errorf("want $jj-root, got %q", got)
	}
}

func TestJujutsuRoot_DisplayPath_Long(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, ".jj"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(path, []byte("k: v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := discover.JujutsuRoot(5).FromDir(tmp)
	ctx := discover.WithLongDisplayPaths(context.Background())
	if got := d.DisplayPath(ctx, path); got != "(jj root)/config.yaml" {
		t.Errorf("want (jj root)/config.yaml, got %q", got)
	}
}
