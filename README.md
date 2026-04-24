# kongfig

Layered configuration with provenance tracking for Go.

Loads config from multiple sources (defaults, env, files, flags) in priority order,
tracks which source set each value, and renders annotated output for debugging.

## Install

```bash
go get github.com/pmarschik/kongfig@latest
# Separate modules — get only what you need:
go get github.com/pmarschik/kongfig/parsers/yaml@latest
go get github.com/pmarschik/kongfig/providers/file@latest
```

## Quick start

```go
import (
    kongfig "github.com/pmarschik/kongfig"
    tomlparser "github.com/pmarschik/kongfig/parsers/toml"   // separate module
    yamlparser "github.com/pmarschik/kongfig/parsers/yaml"   // separate module
    fileprovider "github.com/pmarschik/kongfig/providers/file" // separate module
    structsprovider "github.com/pmarschik/kongfig/providers/structs"
    "github.com/pmarschik/kongfig/style/plain"
)

type AppConfig struct {
    Host string `kongfig:"host" env:"APP_HOST"`
    Port int    `kongfig:"port" env:"APP_PORT"`
}

ctx := context.Background()
kf := kongfig.New()
kf.MustLoad(ctx, structsprovider.Defaults(AppConfig{Host: "localhost", Port: 8080}))
kf.MustLoad(ctx, structsprovider.TagEnv[AppConfig]()) // reads APP_HOST, APP_PORT
kf.MustLoad(ctx, fileprovider.New("config.yaml", yamlparser.Default))

cfg, _ := kongfig.Get[AppConfig](kf)

// Render annotated output (uses the parser registered by Load, or the first
// registered parser when multiple are loaded).
kf.Render(ctx, os.Stdout, plain.New())
// host: localhost  # defaults
// port: 9090       # config.yaml

// Explicit format per call:
kf.Render(ctx, os.Stdout, plain.New(), kongfig.WithRenderFormat("toml"))

// Pin a default format at construction time (overrides registration order):
kf = kongfig.New(
    kongfig.WithParsers(yamlparser.Default, tomlparser.Default),
    kongfig.WithDefaultFormat("yaml"),
)
// Format selection priority: WithRenderFormat > WithDefaultFormat > first registered.
```

Use `structs.TagDefaults[T]()` to drive defaults from `default=` struct tag
annotations instead of a populated instance — see
[struct-tags.md](docs/struct-tags.md#default-option).

See [`example/minimal`](example/minimal/) for the full minimal example.

### Get options

`Get[T]` accepts option arguments:

```go
// Decode the sub-tree at "db" instead of the config root
dbCfg, _ := kongfig.Get[DBConfig](kf, kongfig.At("db"))

// Fail if any struct field has no matching key
cfg, err := kongfig.Get[Config](kf, kongfig.Strict())
```

### Custom types: codec= struct tag (preferred)

For custom Go types, register a named codec and annotate the field with `codec=`:

```go
import "github.com/pmarschik/kongfig/codec"

// Register all standard codecs at once:
kf := kongfig.NewFor[Config](kongfig.WithCodecRegistry(codec.Default))

type Config struct {
    Addr    net.IP        `kongfig:"addr"`                       // auto-matched by type via NewFor
    Created time.Time     `kongfig:"created,codec=time-rfc3339"` // explicit layout
    Timeout time.Duration `kongfig:"timeout,codec=duration"`
}
```

The `codec` sub-package provides ready-made codecs for `net.IP`, `time.Duration`,
`time.Time`, `*url.URL`, `*regexp.Regexp`, and more. Custom codecs implement `Codec[T]`
(Decode + Encode). See [docs/advanced.md](docs/advanced.md#withcodec--named-bidirectional-codec)
for the full API including `RegisterCodec`, `AddCodec`, and building shared registries.

`net.IP` and `time.Duration` also decode from strings automatically via `TextUnmarshaler`
and duration hooks — no codec needed for those common cases.

> **Escape hatch:** `TypedDecodeHook` is available for one-off conversions where a codec
> is too much. See [docs/advanced.md](docs/advanced.md#typeddecodeHook--custom-string-to-type-conversion).

### Cross-key projection (dotted tags)

`Get[T]` supports dotted `kongfig` tags to project values from different sub-trees into
a flat struct — useful for cross-key validation rules:

```go
type DBRule struct {
    MinConns int    `kongfig:"db.min_conns"`
    MaxConns int    `kongfig:"db.max_conns"`
    Timeout  int    `kongfig:"server.timeout"`
}

rule, _ := kongfig.Get[DBRule](kf)
// rule.MinConns == kf data at "db.min_conns"
// rule.Timeout  == kf data at "server.timeout"
```

> **Note:** `GetByPaths[T]` is deprecated. Use `Get[T]` with dotted tags instead.

## File provider discovery

`providers/file` ships a `discover` sub-package with composable discoverers that locate
config files on disk. Pass them to `file.Discover` or `file.DiscoverAll`:

```go
import (
    fileprovider "github.com/pmarschik/kongfig/providers/file"
    "github.com/pmarschik/kongfig/providers/file/discover"
)

// For system-wide config (/etc/appname, /Library/Application Support/appname, etc.),
// prefer SystemDirs over a hard-coded Explicit path — it handles platform differences.
p, err := fileprovider.Discover(ctx, discover.SystemDirs(), yamlparser.Default)

// All paths: every discoverer that finds a file is loaded.
providers := fileprovider.DiscoverAll(ctx,
    discover.SystemDirs(),        // system-wide config (/etc/app, platform-specific)
    discover.UserDirs(),          // OS user config dirs (~/.config/app, platform-specific)
    // Note: UserDirs already covers XDG user dirs (~/.config/appname) on Linux/macOS.
    // Use XDG() only when you need to honour $XDG_CONFIG_HOME explicitly.
    discover.XDG(),               // $XDG_CONFIG_HOME / ~/.config (XDG spec override)
    discover.Workdir(),           // current working directory
    discover.GitRoot(),           // walk up to nearest .git root
    discover.JujutsuRoot(),       // walk up to nearest .jj root
    discover.ExecDir(),           // directory of the running binary
    discover.Explicit("/etc/app/config.yaml"), // hard-coded fallback path
    yamlparser.Default,
)
```

### UserDirs and SystemDirs

`UserDirs()` and `SystemDirs()` search platform-appropriate directories
(`~/.config`, `~/Library/Application Support`, `%APPDATA%`, `/etc`, etc.) and are
controlled by two method chains:

```go
discover.UserDirs().
    WithNames("config", "settings"). // filenames to try (default: "config")
    WithStyle(discover.StyleSubdir)  // StyleSubdir, StyleFlat, or both (default)
// Searches: <base>/<app>/config.<ext> and <base>/<app>/settings.<ext>
```

The app name is read from `ctx` via `kongfig.AppName`. `SystemDirs()` follows the same
API for system-wide config directories.

### VCS-aware discoverers

`providers/file/discover/vcs` is a separate module with go-git and jj-backed discoverers:

```go
import "github.com/pmarschik/kongfig/providers/file/discover/vcs"

discover.DiscoverAll(ctx,
    vcs.GitRoot(),                     // uses go-git; no git binary required
    vcs.JujutsuRoot(),                 // runs jj root
    vcs.GitRoot(vcs.WithStartDir("/path/to/start")), // override start dir
    yamlparser.Default,
)
```

### Deprecating legacy paths

Wrap an old discoverer with `discover.Deprecated` to emit a `LegacyFileEvent` when
the legacy location is still in use:

```go
file.Discover(ctx,
    discover.Deprecated(discover.XDG(), "~/.config/app/config.yaml"),
    yamlparser.Default,
)
// Fires LegacyFileEvent on first find; handler can warn user to migrate.
```

Wire a handler via `kf.AddRename` + `MigrationPolicy` or `kongfig.MigrationWarn`.
See [docs/migration.md](docs/migration.md) for the full migration API.

## Key migration

Rename config keys across versions without breaking existing configs. Register a rename
before loading; kongfig silently rewrites any old key it finds in an incoming layer:

```go
kf.AddRename("db.host", "database.host")      // move key
kf.AddRename("log-level", "log.level",         // custom policy: fail on first use
    kongfig.MigrationPolicy{
        OnFirst:  kongfig.MigrationFail,
        OnRepeat: kongfig.MigrationWarn,
    },
)
```

See [docs/migration.md](docs/migration.md) for event types (`RenameEvent`,
`ConflictEvent`, `LegacyFileEvent`) and custom handlers.

## Live reload

Providers that implement `WatchProvider` can push config updates at runtime. Register
them with `AddWatcher`, subscribe to changes with `OnChange`, then start the watch loop:

```go
fp := fileprovider.New("config.yaml", yamlparser.Default)
kf.MustLoad(ctx, fp)
kf.AddWatcher(fp)                      // fp implements WatchProvider via fsnotify
kf.OnChange(func() {
    cfg, _ := kongfig.Get[AppConfig](kf)
    log.Println("reloaded:", cfg)
})
go kf.Watch(ctx)                       // blocks until ctx is canceled
```

See [docs/watch.md](docs/watch.md) for the full API and implementing custom watchers.

## Merge strategies

By default, a later `Load` overwrites earlier values at the same key. For slice fields,
register a merge strategy with `SetMergeFunc`:

```go
import "github.com/pmarschik/kongfig/mergefuncs"

kf.SetMergeFunc("plugins", mergefuncs.AppendSlice) // accumulate across loads
kf.SetMergeFunc("tags",    mergefuncs.UnionSet)     // deduplicated union
```

## Modules

Packages with external dependencies are separate Go modules so you only pull in what
you use. Packages without external deps ship in the core module.

**Core** (`go get github.com/pmarschik/kongfig`):

| Package             | Description                                                   |
| ------------------- | ------------------------------------------------------------- |
| `providers/structs` | Defaults and env vars from struct field tags                  |
| `providers/env`     | Env var prefix scanning                                       |
| `parsers/json`      | JSON/JSONC encoding/decoding                                  |
| `mergefuncs`        | Custom merge strategies (AppendSlice, ReplaceSlice, UnionSet) |
| `style/plain`       | Plain-text (no color) styler                                  |

**Separate modules** (each `go get`-able independently):

| Module                      | `go get` path                                              |
| --------------------------- | ---------------------------------------------------------- |
| YAML parser                 | `github.com/pmarschik/kongfig/parsers/yaml`                |
| TOML parser                 | `github.com/pmarschik/kongfig/parsers/toml`                |
| File provider               | `github.com/pmarschik/kongfig/providers/file`              |
| File discover/vcs           | `github.com/pmarschik/kongfig/providers/file/discover/vcs` |
| Charming (lipmark) styler   | `github.com/pmarschik/kongfig/style/charming`              |
| kong resolver               | `github.com/pmarschik/kongfig/kong/resolver`               |
| kong provider (env + flags) | `github.com/pmarschik/kongfig/kong/provider`               |
| kong show flags             | `github.com/pmarschik/kongfig/kong/show`                   |
| kong charming integration   | `github.com/pmarschik/kongfig/kong/charming`               |

## Redaction

Mark sensitive values with `kongfig:",redacted"` struct tags and they are hidden
in rendered output (shown as `<redacted>`):

```go
type Config struct {
    DB struct {
        Host     string `kongfig:"host"`
        Password string `kongfig:"password,redacted"`
    } `kongfig:"db"`
}

// NewFor auto-derives the redacted path set from struct tags:
kf := kongfig.NewFor[Config]()
```

## Validation

The `kongfig/validation` sub-package (core module, no extra dependencies) provides
per-key validators, cross-key rules (typed projection structs), and schema annotation
validators (`required`, `required` with custom annotations). Run `v.Validate(kf)` after
loading; optionally fire validators on each `Load()` via `WithValidateOnLoad`. Composite
helpers (`ExactlyOneOf`, `AllOrNone`, `RequiredWith`, …) cover common multi-field
constraints.

See [docs/validation.md](docs/validation.md) for the full reference.

## CLI integration

Four `kong/` subpackages integrate with [kong](https://github.com/alecthomas/kong),
and `ScanFlag` handles pre-parse bootstrapping for any CLI framework:

| Package         | Description                                                |
| --------------- | ---------------------------------------------------------- |
| `kong/resolver` | Use kongfig as a kong flag resolver (config-file defaults) |
| `kong/provider` | Load kong-parsed env vars and flags as a kongfig layer     |
| `kong/show`     | Add `--format`, `--layers`, `--redacted` flags to your CLI |
| `kong/charming` | Charming-styled help + resolver wired in one call          |

```go
type CLI struct {
    Host   string         `name:"host" env:"APP_HOST" default:"localhost"`
    Render kongshow.Flags `embed:"" config:"-"`
}

kf := kongfig.New()
k, _ := kong.New(&cli, kong.Resolvers(kongresolver.New(kf)))
kctx, _ := k.Parse(os.Args[1:])
kf.MustLoad(ctx, kongprovider.Env(k))
kf.MustLoad(ctx, kongprovider.Args(kctx))
```

See [docs/cli.md](docs/cli.md) for `ScanFlag`, `kong/show` flags,
`kong/charming` wiring, and a full integration example.
Also: [`example/features/`](example/features/) and [`example/full/`](example/full/).

## Advanced

For features beyond the above:

- [docs/cli.md](docs/cli.md) — CLI bootstrap (`ScanFlag`), kong integration
- [docs/advanced.md](docs/advanced.md) — `MustLoadAll`, `LoadParsed`, `OnLoad` hooks,
  `WithCodec`/`RegisterCodec`, `TypedDecodeHook` (escape hatch), `AddTransform`, `MetaKey`/path metadata
- [docs/styling.md](docs/styling.md) — charming styler, custom `Styler` implementations, renderer conventions
- [docs/struct-tags.md](docs/struct-tags.md) — full struct tag reference (`default=`,
  `redacted`, `env`, `config`, split separators)
- [docs/validation.md](docs/validation.md) — composite rule helpers, custom annotations,
  registry constructors, `ValidateOnLoad`
- [docs/provenance.md](docs/provenance.md) — source labels, `FilterSource`, provenance API
- [docs/architecture.md](docs/architecture.md) — load/render pipeline, internal design

## Development

```bash
mise run setup    # install tools
mise run check    # format + lint + test
mise run fmt      # format only
mise run test     # test only
go build ./...    # build
```

See [AGENTS.md](AGENTS.md) for contributor conventions.
