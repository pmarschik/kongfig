# CLI integration

## Bootstrapping with ScanFlag

```go
func ScanFlag(args []string, long string, shorts ...string) string
```

Scans `args` for a named flag before the full CLI parser runs. Handles
`--long=value`, `--long value`, and `-s value` forms. Returns `""` if not found.

**Use case:** extract a `--config` path so you can load the right file before kong
parses (and before the resolver is wired up).

```go
configPath := kongfig.ScanFlag(os.Args[1:], "config", "c")
if configPath == "" {
    configPath = "config.yaml"
}
kf.MustLoad(ctx, fileprovider.New(configPath, yamlparser.Default))
```

---

## kong integration

Four packages wire kongfig into a [kong](https://github.com/alecthomas/kong) CLI.
Each is a separate Go module — get only what you need.

| Package         | Role                                                                            | `go get` path                                |
| --------------- | ------------------------------------------------------------------------------- | -------------------------------------------- |
| `kong/resolver` | Seeds kong flag defaults from the merged kongfig state                          | `github.com/pmarschik/kongfig/kong/resolver` |
| `kong/provider` | Loads kong-resolved env vars and explicit CLI flags as kongfig layers           | `github.com/pmarschik/kongfig/kong/provider` |
| `kong/show`     | Adds `--format`, `--sources`, `--layers`, `--redacted` flags for config display | `github.com/pmarschik/kongfig/kong/show`     |
| `kong/charming` | Wires charming-styled help renderer and a kongfig resolver in one call          | `github.com/pmarschik/kongfig/kong/charming` |

---

### kong/resolver — config as flag defaults

`resolver.New(kf)` returns a `kong.Resolver` that seeds kong flag defaults from
the merged kongfig state. Flag defaults shown by `--help` reflect config file
values, and explicit CLI flags still win.

Key resolution: uses the flag's `config:""` struct tag as the dot-path; falls
back to the flag name with hyphens replaced by dots.

If a `ConfigValidator` is registered on `kf` via `kongfig.WithValidator`, it runs
automatically after kong parses — no extra wiring needed.

```go
k, _ := kong.New(&cli,
    kong.Resolvers(kongresolver.New(kf)),
)
```

---

### kong/provider — flags and env as config layers

Three providers extract data from a parsed kong application into kongfig layers,
in priority order (load lowest → highest priority last):

| Function               | Source label | What it loads                                               |
| ---------------------- | ------------ | ----------------------------------------------------------- |
| `provider.Defaults(k)` | `defaults`   | Default values from kong struct tags                        |
| `provider.Env(k)`      | `env.kong`   | Env vars kong resolved for each flag                        |
| `provider.Args(kctx)`  | `flags`      | Flags explicitly set on the CLI (skips resolver-set values) |

Env and Args providers also implement `ProviderFieldNamesSupport`, registering the
actual env var name (e.g. `APP_HOST`) or flag name (e.g. `--host`) for each path
so renderers can annotate values.

A convenience wrapper loads env + flags in one call:

```go
kongprovider.MustLoadAll(ctx, k, kctx, kf)
// equivalent to:
kf.MustLoad(ctx, kongprovider.Env(k))
kf.MustLoad(ctx, kongprovider.Args(kctx))
```

**Config-path flags** — tag a string flag with `kongfig:",config-path"` and call
`kongprovider.LoadConfigPaths` after parsing to load files referenced by those flags:

```go
type CLI struct {
    Config string `name:"config" short:"c" config:"-" kongfig:",config-path" optional:"" type:"path"`
}

kctx, _ := k.Parse(os.Args[1:])
kongprovider.MustLoadAll(ctx, k, kctx, kf)
kongprovider.MustLoadConfigPaths(ctx, k, kf)
```

---

### kong/show — config display flags

Embed `show.Flags` in a CLI struct to add config-display flags. Call
`Flags.Render(ctx, w, kf, styler)` to write annotated config output.

**`show.Flags`** embeds:

| Flag               | Type             | Description                                                                            |
| ------------------ | ---------------- | -------------------------------------------------------------------------------------- |
| `--format`         | enum             | Output format: `` (auto), `yaml`, `env`, `flags`, or any registered parser format      |
| `--sources`        | `[]string`       | Filter sources: `env,file` (allowlist), `-defaults` (exclude), `+env,-workdir` (mixed) |
| `--layers`         | bool             | Render each config layer separately instead of the merged view                         |
| `--verbose` / `-v` | counter          | Show detailed sub-source labels in `--layers` output (repeat for more)                 |
| `--redacted`       | bool (negatable) | Reveal values hidden by `kongfig:",redacted"` (default: hidden)                        |

For apps that don't need format selection, **`show.SimpleFlags`** provides a
simpler set: `--plain`, `--layers`, `--redacted`, and per-source negatable flags
(`--no-defaults`, `--no-env`, `--no-file`, `--no-flags`).

Inject the `--format` enum at `kong.New` time so format options match registered
parsers:

```go
type CLI struct {
    Render kongshow.Flags `embed:"" config:"-"`
}

k, _ := kong.New(&cli,
    kongshow.FlagsVarsFromKongfig(kf),  // derives --format enum from kf.Parsers()
    // or use kongshow.FlagsVars() for a fixed enum
)
```

---

### kong/charming — styled output

`charming.Options(kf, reg, themeName)` returns a `[]kong.Option` slice that wires:

- kong-charming's styled help renderer (lipmark theme)
- a kongfig resolver seeded from `kf` (same as `kongresolver.New(kf)`)

```go
reg := theme.NewWithOptions(theme.WithDefaults())
reg.RegisterStruct("auto", kongcharming.LayerStyleDefs{
    Flags:    theme.StyleDef{Foreground: "#9ece6a"},
    Env:      theme.StyleDef{Foreground: "#7dcfff"},
    File:     theme.StyleDef{Foreground: "#bb9af7"},
    Defaults: theme.StyleDef{Foreground: "#565f89"},
})

opts = append(opts, kongcharming.Options(kf, reg, "auto")...)
k, _ := kong.New(&cli, opts...)
```

When you need the styler for rendering outside of kong (e.g. in `Flags.Render`):

```go
s := kongcharming.Styler(reg, "auto")
cli.Render.Render(ctx, os.Stdout, kf, s)
```

---

### Full wiring example

```go
import (
    "context"
    "os"

    "github.com/alecthomas/kong"
    kongfig "github.com/pmarschik/kongfig"
    yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
    fileprovider "github.com/pmarschik/kongfig/providers/file"
    structsprovider "github.com/pmarschik/kongfig/providers/structs"
    kongcharming "github.com/pmarschik/kongfig/kong/charming"
    kongprovider "github.com/pmarschik/kongfig/kong/provider"
    kongshow "github.com/pmarschik/kongfig/kong/show"
    "github.com/pmarschik/lipmark/theme"
)

type CLI struct {
    Host   string         `name:"host" env:"APP_HOST" default:"localhost"`
    Config string         `name:"config" short:"c" config:"-" kongfig:",config-path" optional:"" type:"path"`
    Show   kongshow.Flags `embed:"" prefix:"show-" config:"-"`
}

func main() {
    ctx := context.Background()

    kf := kongfig.NewFor[AppConfig]()
    kf.RegisterParsers(yamlparser.Default)

    // 1. Load layers below CLI priority (defaults, discovered files).
    kf.MustLoad(ctx, structsprovider.TagDefaults[AppConfig]())
    providers := fileprovider.DiscoverAll(ctx, discover.UserDirs(), yamlparser.Default)
    kongfig.MustLoadAll(ctx, kf, providers)

    // 2. Scan for --config before kong.New so the resolver sees the right file.
    if p := kongfig.ScanFlag(os.Args[1:], "config", "c"); p != "" {
        kf.MustLoad(ctx, fileprovider.New(p, yamlparser.Default))
    }

    // 3. Wire charming (resolver + styled help) and kong/show flag vars.
    reg := theme.NewWithOptions(theme.WithDefaults())
    reg.RegisterStruct("auto", kongcharming.LayerStyleDefs{
        Flags:    theme.StyleDef{Foreground: "#9ece6a"},
        Env:      theme.StyleDef{Foreground: "#7dcfff"},
        File:     theme.StyleDef{Foreground: "#bb9af7"},
        Defaults: theme.StyleDef{Foreground: "#565f89"},
    })
    var cli CLI
    opts := append(
        kongcharming.Options(kf, reg, "auto"),
        kongshow.FlagsVarsFromKongfig(kf),
    )
    k, _ := kong.New(&cli, opts...)
    kctx, _ := k.Parse(os.Args[1:])

    // 4. Load kong-sourced layers (env, flags), then config-path flags.
    kongprovider.MustLoadAll(ctx, k, kctx, kf)
    kongprovider.MustLoadConfigPaths(ctx, k, kf)

    // 5. Render config display if requested.
    s := kongcharming.Styler(reg, "auto")
    if err := cli.Show.Render(ctx, os.Stdout, kf, s); err != nil {
        k.Fatalf("%v", err)
    }
}
```
