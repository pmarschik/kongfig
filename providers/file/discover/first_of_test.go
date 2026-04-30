package discover_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/providers/file/discover"
)

// --- LocateNames ---

func TestLocateNames_FindsFile(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "yard.yaml"), []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}
	loc := discover.LocateNames("yard")
	got := loc(context.Background(), tmp, []string{".yaml"})
	if got != filepath.Join(tmp, "yard.yaml") {
		t.Errorf("want %q, got %q", filepath.Join(tmp, "yard.yaml"), got)
	}
}

func TestLocateNames_TriesNamesInOrder(t *testing.T) {
	tmp := t.TempDir()
	// Only the second name exists.
	if err := os.WriteFile(filepath.Join(tmp, ".yard-local.yaml"), []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}
	loc := discover.LocateNames(".yard", ".yard-local")
	got := loc(context.Background(), tmp, []string{".yaml"})
	if got != filepath.Join(tmp, ".yard-local.yaml") {
		t.Errorf("want .yard-local.yaml, got %q", got)
	}
}

func TestLocateNames_TriesExtensionsPerName(t *testing.T) {
	tmp := t.TempDir()
	// Only .toml extension exists for the name.
	if err := os.WriteFile(filepath.Join(tmp, "settings.toml"), []byte("key = \"v\""), 0o600); err != nil {
		t.Fatal(err)
	}
	loc := discover.LocateNames("settings")
	got := loc(context.Background(), tmp, []string{".yaml", ".toml"})
	if got != filepath.Join(tmp, "settings.toml") {
		t.Errorf("want settings.toml, got %q", got)
	}
}

func TestLocateNames_NotFound_ReturnsEmpty(t *testing.T) {
	tmp := t.TempDir()
	loc := discover.LocateNames("nonexistent")
	got := loc(context.Background(), tmp, []string{".yaml"})
	if got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

func TestLocateNames_IgnoresContext(t *testing.T) {
	tmp := t.TempDir()
	// AppName is set, but LocateNames should NOT use it — only the explicit names.
	if err := os.WriteFile(filepath.Join(tmp, "myapp.yaml"), []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := kongfig.WithAppName(context.Background(), "myapp")
	loc := discover.LocateNames("notmyapp") // explicit name, not derived from AppName
	got := loc(ctx, tmp, []string{".yaml"})
	if got != "" {
		t.Errorf("want empty (LocateNames ignores AppName), got %q", got)
	}
}

// --- FirstOf ---

func TestFirstOf_ReturnsFirstResult(t *testing.T) {
	tmpA := t.TempDir()
	tmpB := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpA, "config.yaml"), []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpB, "config.yaml"), []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}

	dA := discover.Compose("a", staticDirs(tmpA), discover.LocateConfigBase())
	dB := discover.Compose("b", staticDirs(tmpB), discover.LocateConfigBase())
	d := discover.FirstOf(dA, dB)

	got, err := d.Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(tmpA, "config.yaml"); got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestFirstOf_FallsThrough(t *testing.T) {
	tmpA := t.TempDir() // empty — no config file
	tmpB := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpB, "config.yaml"), []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}

	dA := discover.Compose("a", staticDirs(tmpA), discover.LocateConfigBase())
	dB := discover.Compose("b", staticDirs(tmpB), discover.LocateConfigBase())
	d := discover.FirstOf(dA, dB)

	got, err := d.Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(tmpB, "config.yaml"); got != want {
		t.Errorf("want %q (from b), got %q", want, got)
	}
}

func TestFirstOf_NoneFound_ReturnsEmpty(t *testing.T) {
	tmpA := t.TempDir()
	dA := discover.Compose("a", staticDirs(tmpA), discover.LocateConfigBase())
	d := discover.FirstOf(dA)

	got, err := d.Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

func TestFirstOf_Name_ReturnsWinnerName(t *testing.T) {
	tmpA := t.TempDir() // empty
	tmpB := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpB, "config.yaml"), []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}

	dA := discover.Compose("alpha", staticDirs(tmpA), discover.LocateConfigBase())
	dB := discover.Compose("beta", staticDirs(tmpB), discover.LocateConfigBase())
	d := discover.FirstOf(dA, dB)

	if _, err := d.Discover(context.Background(), []string{".yaml"}); err != nil {
		t.Fatal(err)
	}
	if got := d.Name(); got != "beta" {
		t.Errorf("Name() after beta wins: want %q, got %q", "beta", got)
	}
}

func TestFirstOf_Name_BeforeDiscover_ReturnsFirstName(t *testing.T) {
	dA := discover.Compose("alpha", staticDirs(t.TempDir()), discover.LocateConfigBase())
	dB := discover.Compose("beta", staticDirs(t.TempDir()), discover.LocateConfigBase())
	d := discover.FirstOf(dA, dB)

	// Name() before any Discover call should return the first sub-discoverer's name.
	if got := d.Name(); got != "alpha" {
		t.Errorf("Name() before Discover: want %q, got %q", "alpha", got)
	}
}

func TestFirstOf_Name_NoneFound_ReturnsFirstName(t *testing.T) {
	dA := discover.Compose("alpha", staticDirs(t.TempDir()), discover.LocateConfigBase())
	dB := discover.Compose("beta", staticDirs(t.TempDir()), discover.LocateConfigBase())
	d := discover.FirstOf(dA, dB)

	if _, err := d.Discover(context.Background(), []string{".yaml"}); err != nil {
		t.Fatal(err)
	}
	// No winner — should still return first name so empty provider has meaningful label.
	if got := d.Name(); got != "alpha" {
		t.Errorf("Name() when none found: want %q, got %q", "alpha", got)
	}
}

func TestFirstOf_DisplayPath_DelegatesToWinner(t *testing.T) {
	tmp := t.TempDir()
	resolved, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(resolved, "config.yaml")
	if err := os.WriteFile(path, []byte("k: v"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(resolved)

	// Workdir has a meaningful DisplayPath.
	dEmpty := discover.Compose("empty", staticDirs(t.TempDir()), discover.LocateConfigBase())
	dWorkdir := discover.Workdir()
	d := discover.FirstOf(dEmpty, dWorkdir)

	if _, err := d.Discover(context.Background(), []string{".yaml"}); err != nil {
		t.Fatal(err)
	}
	got := d.DisplayPath(context.Background(), path)
	if got != "$workdir" {
		t.Errorf("DisplayPath: want $workdir, got %q", got)
	}
}

func TestFirstOf_Empty_ReturnsFirstOfName(t *testing.T) {
	d := discover.FirstOf()
	if got := d.Name(); got != "first-of" {
		t.Errorf("empty FirstOf().Name(): want %q, got %q", "first-of", got)
	}
	got, err := d.Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("empty FirstOf().Discover(): want empty, got %q", got)
	}
}

// staticDirs returns a DirProvider that always yields dir as the only entry.
func staticDirs(dir string) discover.DirProvider {
	return func(_ context.Context) ([]discover.DirEntry, error) {
		return []discover.DirEntry{{Path: dir, Short: "$test", Long: "$test"}}, nil
	}
}

// errorDiscoverer is a fake that always returns an error from Discover.
type errorDiscoverer struct{}

func (*errorDiscoverer) Name() string { return "error" }
func (*errorDiscoverer) Discover(_ context.Context, _ []string) (string, error) {
	return "", errors.New("discover error")
}
