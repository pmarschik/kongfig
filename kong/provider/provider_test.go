package provider_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	kongfig "github.com/pmarschik/kongfig"
	kongprovider "github.com/pmarschik/kongfig/kong/provider"
)

// kvParser is a minimal key: value parser that also implements ParserNamer,
// so it can be registered with Kongfig for path-based format selection.
type kvParser struct{}

func (kvParser) Format() string       { return "kv" }
func (kvParser) Extensions() []string { return []string{".kv"} }

func (kvParser) Unmarshal(b []byte) (kongfig.ConfigData, error) {
	out := make(kongfig.ConfigData)
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, _ := strings.Cut(line, ": ")
		if k != "" {
			out[k] = v
		}
	}
	return out, nil
}

func (kvParser) Marshal(data kongfig.ConfigData) ([]byte, error) {
	var sb strings.Builder
	for k, v := range data {
		fmt.Fprintf(&sb, "%s: %v\n", k, v)
	}
	return []byte(sb.String()), nil
}

type testCLI struct {
	Host     string `name:"host"      default:"localhost"  help:"Host."`
	LogLevel string `name:"log-level" env:"TEST_LOG_LEVEL" default:"info"    help:"Log level."`
	UITheme  string `name:"ui-theme"  env:"TEST_UI_THEME"  config:"ui.theme" default:"auto"    help:"Theme."`
	Port     int    `name:"port"      default:"8080"       help:"Port."`
}

func mustParse(t *testing.T, cli any, args []string) (*kong.Kong, *kong.Context) {
	t.Helper()
	k, err := kong.New(cli)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := k.Parse(args)
	if err != nil {
		t.Fatal(err)
	}
	return k, ctx
}

func TestDefaults(t *testing.T) {
	cli := &testCLI{}
	k, _ := mustParse(t, cli, []string{})

	p := kongprovider.Defaults(k)
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data["host"] != "localhost" {
		t.Errorf("host: got %v", data["host"])
	}
	if data["port"] != int64(8080) {
		t.Errorf("port: got %v (%T)", data["port"], data["port"])
	}
	// config:"ui.theme" tag — should produce nested map
	ui, ok := data["ui"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("ui not a map: %T", data["ui"])
	}
	if ui["theme"] != "auto" {
		t.Errorf("ui.theme: got %v", ui["theme"])
	}
}

func TestEnv(t *testing.T) {
	t.Setenv("TEST_LOG_LEVEL", "debug")
	t.Setenv("TEST_UI_THEME", "dark")

	cli := &testCLI{}
	k, _ := mustParse(t, cli, []string{})

	p := kongprovider.Env(k)
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// log-level: hyphens become dots → "log.level" path → nested map[log]map[level]
	log, ok := data["log"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("log not a map: %T (data=%v)", data["log"], data)
	}
	if log["level"] != "debug" {
		t.Errorf("log.level: got %v", log["level"])
	}
	// ui-theme uses config:"ui.theme" tag — should be nested
	ui, ok := data["ui"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("ui not a map: %T", data["ui"])
	}
	if ui["theme"] != "dark" {
		t.Errorf("ui.theme: got %v", ui["theme"])
	}
}

func TestArgsOnlyExplicit(t *testing.T) {
	cli := &testCLI{}
	_, ctx := mustParse(t, cli, []string{"--host=myhost"})

	p := kongprovider.Args(ctx)
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// host was explicitly set
	if data["host"] != "myhost" {
		t.Errorf("host: got %v", data["host"])
	}
	// port was NOT set — should be absent
	if _, ok := data["port"]; ok {
		t.Error("port should not appear in Args (not explicitly set)")
	}
}

func TestArgsConfigTag(t *testing.T) {
	cli := &testCLI{}
	_, ctx := mustParse(t, cli, []string{"--ui-theme=dark"})

	p := kongprovider.Args(ctx)
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// config:"ui.theme" tag means it should be nested
	ui, ok := data["ui"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("ui not a map: %T", data["ui"])
	}
	if ui["theme"] != "dark" {
		t.Errorf("ui.theme: got %v", ui["theme"])
	}
}

// configPathCLI has a --config flag tagged with kongfig:",config-path".
type configPathCLI struct {
	Config string `name:"config" optional:""         type:"path"  help:"Config file." kongfig:",config-path"`
	Host   string `name:"host"   default:"localhost" help:"Host."`
}

func TestLoadConfigPaths_LoadsFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "myconfig.kv")
	if err := os.WriteFile(cfgFile, []byte("host: filehost\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cli := &configPathCLI{}
	// Parse with --config= so cli.Config is set; LoadConfigPaths reads flag.Target.
	k, kctx := mustParse(t, cli, []string{"--config=" + cfgFile})

	kf := kongfig.New(kongfig.WithParsers(kvParser{}))
	kongprovider.MustLoadAll(context.Background(), k, kctx, kf)

	if err := kongprovider.LoadConfigPaths(context.Background(), k, kf); err != nil {
		t.Fatal(err)
	}

	all := kf.All()
	if all["host"] != "filehost" {
		t.Errorf("host: got %v, want filehost", all["host"])
	}
}

func TestLoadConfigPaths_EmptyFlagSkipped(t *testing.T) {
	// When the config flag is empty, LoadConfigPaths should not error.
	cli := &configPathCLI{}
	k, _ := mustParse(t, cli, []string{}) // no --config flag

	kf := kongfig.New(kongfig.WithParsers(kvParser{}))
	if err := kongprovider.LoadConfigPaths(context.Background(), k, kf); err != nil {
		t.Fatalf("unexpected error for empty config path: %v", err)
	}
}

func TestAppNameOption_SetsKongName(t *testing.T) {
	ctx := kongfig.WithAppName(context.Background(), "myapp")

	// AppNameOption returns a kong.Option that sets the app name.
	opt := kongprovider.AppNameOption(ctx)

	type simpleCLI struct {
		Host string `name:"host" help:"Host."`
	}
	cli := &simpleCLI{}
	k, err := kong.New(cli, opt)
	if err != nil {
		t.Fatal(err)
	}
	// Kong uses the name as the app's Model.Name.
	if k.Model.Name != "myapp" {
		t.Errorf("kong model name: got %q, want myapp", k.Model.Name)
	}
}

func TestAppNameOption_EmptyContext_NoError(t *testing.T) {
	// No app name set — AppNameOption should produce kong.Name("") which is valid.
	opt := kongprovider.AppNameOption(context.Background())

	type simpleCLI struct {
		Host string `name:"host" help:"Host."`
	}
	cli := &simpleCLI{}
	_, err := kong.New(cli, opt)
	if err != nil {
		t.Fatalf("unexpected error with empty app name: %v", err)
	}
}
