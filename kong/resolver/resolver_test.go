package resolver_test

import (
	"errors"
	"testing"

	"github.com/alecthomas/kong"
	kongfig "github.com/pmarschik/kongfig"
	kongresolver "github.com/pmarschik/kongfig/kong/resolver"
)

type testCLI struct {
	Host     string `name:"host"      default:"localhost" help:"Host."`
	UITheme  string `name:"ui-theme"  config:"ui.theme"   default:"auto"    help:"Theme."`
	LogLevel string `name:"log-level" default:"info"      help:"Log level."`
	Port     int    `name:"port"      default:"8080"      help:"Port."`
}

// loadKongfig seeds a Kongfig with the given flat data.
func loadKongfig(t *testing.T, data map[string]any) *kongfig.Kongfig {
	t.Helper()
	return loadKongfigWithOpts(t, data)
}

func loadKongfigWithOpts(t *testing.T, data map[string]any, opts ...kongfig.Option) *kongfig.Kongfig {
	t.Helper()
	k := kongfig.New(opts...)
	if err := k.LoadParsed(data, "file"); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestResolverOverridesDefault(t *testing.T) {
	kf := loadKongfig(t, map[string]any{"host": "filehost", "port": 9090})

	cli := &testCLI{}
	k, err := kong.New(cli, kong.Resolvers(kongresolver.New(kf)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = k.Parse([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if cli.Host != "filehost" {
		t.Errorf("Host: got %q, want %q", cli.Host, "filehost")
	}
	if cli.Port != 9090 {
		t.Errorf("Port: got %d, want 9090", cli.Port)
	}
}

func TestResolverConfigTag(t *testing.T) {
	kf := loadKongfig(t, map[string]any{
		"ui": map[string]any{"theme": "dark"},
	})

	cli := &testCLI{}
	k, err := kong.New(cli, kong.Resolvers(kongresolver.New(kf)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = k.Parse([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if cli.UITheme != "dark" {
		t.Errorf("UITheme: got %q, want %q", cli.UITheme, "dark")
	}
}

func TestResolverMissingKeyFallsBack(t *testing.T) {
	kf := loadKongfig(t, map[string]any{"host": "filehost"})

	cli := &testCLI{}
	k, err := kong.New(cli, kong.Resolvers(kongresolver.New(kf)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = k.Parse([]string{})
	if err != nil {
		t.Fatal(err)
	}
	// port not in kongfig — should fall back to kong default "8080"
	if cli.Port != 8080 {
		t.Errorf("Port: got %d, want 8080 (kong default)", cli.Port)
	}
}

func TestResolverCLIWins(t *testing.T) {
	kf := loadKongfig(t, map[string]any{"host": "filehost"})

	cli := &testCLI{}
	k, err := kong.New(cli, kong.Resolvers(kongresolver.New(kf)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = k.Parse([]string{"--host=clihost"})
	if err != nil {
		t.Fatal(err)
	}
	// CLI flag wins over resolver value
	if cli.Host != "clihost" {
		t.Errorf("Host: got %q, want %q", cli.Host, "clihost")
	}
}

// stubValidator implements kongfig.ConfigValidator for testing.
type stubValidator struct {
	called *bool
	err    error
}

func (s *stubValidator) ValidateConfig(_ *kongfig.Kongfig) error {
	if s.called != nil {
		*s.called = true
	}
	return s.err
}

func TestResolverWithValidation_Pass(t *testing.T) {
	called := false
	kf := loadKongfigWithOpts(t, map[string]any{"host": "filehost"},
		kongfig.WithValidator(&stubValidator{called: &called}))

	cli := &testCLI{}
	k, err := kong.New(cli, kong.Resolvers(kongresolver.New(kf)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.Parse([]string{}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("validation function was not called")
	}
}

func TestResolverWithValidation_Fail(t *testing.T) {
	kf := loadKongfigWithOpts(t, map[string]any{"host": "filehost"},
		kongfig.WithValidator(&stubValidator{err: errors.New("host is invalid")}))

	cli := &testCLI{}
	k, err := kong.New(cli, kong.Resolvers(kongresolver.New(kf)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = k.Parse([]string{})
	if err == nil {
		t.Error("expected parse to fail due to validation error")
	}
}

func TestResolverWithoutValidation(t *testing.T) {
	// No validator registered — Validate is a no-op.
	kf := loadKongfig(t, map[string]any{"host": "filehost"})
	cli := &testCLI{}
	k, err := kong.New(cli, kong.Resolvers(kongresolver.New(kf)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.Parse([]string{}); err != nil {
		t.Fatalf("unexpected error without validation: %v", err)
	}
}
