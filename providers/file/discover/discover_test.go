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
