package discover_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pmarschik/kongfig/providers/file/discover"
)

func TestDiscoverFirst_FirstWins(t *testing.T) {
	tmpA := t.TempDir()
	tmpB := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpA, "config.yaml"), []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpB, "config.yaml"), []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}

	hit, err := discover.First(context.Background(), []string{".yaml"},
		discover.WithLabel("a", discover.Compose("a", staticDirs(tmpA), discover.LocateConfigBase())),
		discover.WithLabel("b", discover.Compose("b", staticDirs(tmpB), discover.LocateConfigBase())),
	)
	if err != nil {
		t.Fatal(err)
	}
	if hit.Label != "a" {
		t.Errorf("Label: want %q, got %q", "a", hit.Label)
	}
	if want := filepath.Join(tmpA, "config.yaml"); hit.Path != want {
		t.Errorf("Path: want %q, got %q", want, hit.Path)
	}
}

func TestDiscoverFirst_FallsThrough(t *testing.T) {
	tmpA := t.TempDir() // empty
	tmpB := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpB, "config.yaml"), []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}

	hit, err := discover.First(context.Background(), []string{".yaml"},
		discover.WithLabel("a", discover.Compose("a", staticDirs(tmpA), discover.LocateConfigBase())),
		discover.WithLabel("b", discover.Compose("b", staticDirs(tmpB), discover.LocateConfigBase())),
	)
	if err != nil {
		t.Fatal(err)
	}
	if hit.Label != "b" {
		t.Errorf("Label: want %q, got %q", "b", hit.Label)
	}
	if want := filepath.Join(tmpB, "config.yaml"); hit.Path != want {
		t.Errorf("Path: want %q, got %q", want, hit.Path)
	}
}

func TestDiscoverFirst_NoneFound_ReturnsEmptyHit(t *testing.T) {
	hit, err := discover.First(context.Background(), []string{".yaml"},
		discover.WithLabel("a", discover.Compose("a", staticDirs(t.TempDir()), discover.LocateConfigBase())),
		discover.WithLabel("b", discover.Compose("b", staticDirs(t.TempDir()), discover.LocateConfigBase())),
	)
	if err != nil {
		t.Fatal(err)
	}
	if hit.Path != "" || hit.Label != "" {
		t.Errorf("want zero hit, got %+v", hit)
	}
}

func TestDiscoverFirst_Empty_ReturnsZeroHit(t *testing.T) {
	hit, err := discover.First(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if hit.Path != "" || hit.Label != "" {
		t.Errorf("want zero hit on empty input, got %+v", hit)
	}
}

func TestDiscoverFirst_ErrorShortCircuits(t *testing.T) {
	hit, err := discover.First(context.Background(), []string{".yaml"},
		discover.WithLabel("err", &errorDiscoverer{}),
		discover.WithLabel("b", discover.Compose("b", staticDirs(t.TempDir()), discover.LocateConfigBase())),
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if hit.Path != "" {
		t.Errorf("expected empty hit on error, got %+v", hit)
	}
}

func TestDiscoverFirst_LabelPreservedInHit(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}

	hit, err := discover.First(context.Background(), []string{".yaml"},
		discover.WithLabel("system_config", discover.Compose("d", staticDirs(tmp), discover.LocateConfigBase())),
	)
	if err != nil {
		t.Fatal(err)
	}
	if hit.Label != "system_config" {
		t.Errorf("Label: want %q, got %q", "system_config", hit.Label)
	}
}
