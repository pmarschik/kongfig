# Struct Tags

## kongfig tag

The `kongfig:""` struct tag controls how `structs.Defaults`, `structs.TagEnv`, `structs.RedactedPaths`, and `Get` map struct fields to config dot-paths.

### Syntax

```
kongfig:"name[,option[,option...]]"
```

| Tag value                         | Meaning                                                           |
| --------------------------------- | ----------------------------------------------------------------- |
| `kongfig:""`                      | Use lowercased field name as the key                              |
| `kongfig:"my-key"`                | Use `"my-key"` as the key                                         |
| `kongfig:"-"`                     | Skip this field entirely                                          |
| `kongfig:"my-key,redacted"`       | Key `"my-key"`, value hidden in output                            |
| `kongfig:"my-key,redacted=false"` | Key `"my-key"`, explicitly not redacted (overrides parent)        |
| `kongfig:"my-key,inline"`         | Key `"my-key"`, table written on one line where the format allows |
| `kongfig:"my-key,inline=5"`       | As above, but up to 5 keys instead of the renderer default        |

The name part is parsed by `parseTag(tag, fieldName)`: empty name falls back to `strings.ToLower(fieldName)`.

### Nested structs

Struct fields whose type is a struct (or pointer to struct) are treated as namespaces — they are recursed into and their fields are added under the parent key:

```go
type Config struct {
    DB DBConfig `kongfig:"db"`
}
type DBConfig struct {
    Host string `kongfig:"host"`
    Port int    `kongfig:"port"`
}
```

This maps to dot-paths `"db.host"` and `"db.port"`.

Anonymous (embedded) structs are squashed into the parent namespace:

```go
type Config struct {
    Base         // embedded: Base.Host → "host"
    DB DBConfig  `kongfig:"db"`
}
```

### Zero-value omission

`structs.Defaults` skips fields whose value is the Go zero value (0, `""`, false, nil). This means only fields with actual default values appear in the defaults layer.

---

## default= option

`kongfig:"name,default=value"` annotates a field with a default value read by
`structs.TagDefaults[T]()`. This is the struct-tag–driven alternative to passing a
populated instance to `structs.Defaults()`.

```go
type Config struct {
    Host     string `kongfig:"host,default=localhost"`
    Port     string `kongfig:"port,default=8080"`
    LogLevel string `kongfig:"log-level"` // no default= → omitted
}

// Yields {"host": "localhost", "port": "8080"} with source label "defaults".
p := structsprovider.TagDefaults[Config]()
```

Values are always stored as strings; `Get[T]` (via mapstructure) converts them to the
declared field type at decode time.

Single-quoted values allow commas and equals signs in defaults:

```go
Sep string `kongfig:"sep,default=','"`  // default value is ","
```

`TagDefaults[T]()` only includes fields with a `default=` annotation. Fields without it
are omitted, unlike `structs.Defaults(instance)` which includes all non-zero field values.

---

## help= option

`kongfig:"name,help='description'"` annotates a field with a human-readable description,
rendered as a comment above the key.

```go
type Config struct {
    Host     string `kongfig:"host,default=localhost,help='hostname or IP to listen on'"`
    Port     int    `kongfig:"port,default=8080,help='TCP port'"`
    Labels   map[string]string `kongfig:"labels,help='arbitrary key=value labels'"`
}

kf := kongfig.NewFor[Config]()
kf.RenderWith(ctx, w, renderer) // help comments included
```

`NewFor[T]` derives the texts automatically — no extra call needed. Under the hood it uses
`schema.HelpTextPaths[T]()`, which reflects on `T` and returns a `map[string]string` of
dot-path → help text:

```go
texts := schema.HelpTextPaths[Config]()
// {"host": "hostname or IP to listen on", "port": "TCP port", "labels": "arbitrary key=value labels"}
```

Register a set explicitly with the `WithHelpTexts(texts)` option (instance-level, e.g. when
constructing with `New` rather than `NewFor`), or override the registered set for a single
render call with `WithRenderHelpTexts(texts)`.

Use single-quoted values to allow commas and equals signs:

```go
Sep string `kongfig:"sep,help='separator; default is ','"`
```

**Prefix matching for maps and slices**: when a field's path is `"labels"`, the help text
also fires for rendered leaf paths like `"labels.key1"` and `"labels[0]"`. Each help text
is emitted at most once per render call (first match wins; subsequent paths covered by the
same key are silent).

---

## redacted option

`kongfig:"name,redacted"` marks a field as sensitive. `structs.RedactedPaths[T]()` reflects on `T` and returns the set of dot-paths that are redacted.

### Inheritance

Redaction is inherited by nested struct fields. A parent marked `redacted` makes all its leaf descendants redacted by default. Individual leaves can opt out:

```go
type Secrets struct {
    Token    string `kongfig:"token"`           // redacted (inherited)
    PublicID string `kongfig:"public-id,redacted=false"` // not redacted
}

type Config struct {
    API Secrets `kongfig:"api,redacted"` // marks entire Secrets subtree
}
```

`RedactedPaths[Config]()` returns `map[string]bool{"api.token": true}`.

### How redaction flows to output

```
kongfig.RedactedPaths[Config]()
    → map[string]bool{"api.token": true, ...}
    → kongfig.New(WithRedacted{Paths: redactedPaths})
    → stored in k.RenderConfig().RedactedPaths
    → ApplyRenderConfig(opts, k.RenderConfig()) copies it to opts.RedactedPaths
    → ApplyRedaction(data, opts, "") replaces leaf values with RedactedValue{Display}
    → renderers call RenderValue(s, v, formatted) → s.Redacted(display)
```

The path set is populated at startup from the config struct type, not at runtime. Adding a field with `redacted` and calling `New()` again picks it up.

---

## inline option

`kongfig:"name,inline"` marks a table as a candidate for a compact one-line form where
the output format supports it. Today only the TOML parser acts on it, emitting a TOML
inline table (`work = {path = "/w", color = "blue"}`) instead of a `[buckets.work]`
section.

What gets marked depends on the field's type:

```go
type Bucket struct {
    Path  string `kongfig:"path"`
    Color string `kongfig:"color"`
}

type Config struct {
    Buckets map[string]Bucket `kongfig:"buckets,inline"`   // marks "buckets.*" — each entry
    TLS     TLSConfig         `kongfig:"tls,inline=5"`     // marks "tls" — the table itself
}
```

- A **map** field marks its entries, not the map: the registered path is `"buckets.*"`,
  where `*` matches exactly one path segment.
- A **struct** field marks its own path (`"tls"`).

`inline=N` caps the number of direct keys the table may hold; a table with more keys falls
back to a regular section. Plain `inline` (no `=N`) defers to the renderer's default,
`toml.DefaultInlineMaxKeys` (3).

`schema.InlineTablePaths[T]()` returns the marked paths as `[]schema.InlineTableEntry`
(`{Path, MaxKeys}`). `NewFor[T]` calls it automatically and publishes the result under
[`kongfig.InlineTablesKey`], so both rendering and writing a config file pick it up with
no extra wiring:

```go
kf := kongfig.NewFor[Config]()          // "buckets.*" → 0, "tls" → 5
kf.RenderWith(ctx, w, tomlParser.Bind(styler))
```

Paths can also be marked directly on the parser, without struct tags:

```go
p := toml.New(
    toml.WithInlineTables("buckets.*"),
    toml.WithInlineMaxKeys(4),
)
```

See [render-pipeline.md](render-pipeline.md#toml-layout) for how the key limit interacts
with terminal width.

---

## env tag (structs provider)

The `env:""` tag is read by `structs.TagEnv[T]()` to load env var values keyed by their kongfig path:

```go
type Config struct {
    Host string `kongfig:"host" env:"APP_HOST"`
}
```

`structs.TagEnv[Config]()` reads `os.Environ()`, finds `APP_HOST`, and returns `{"host": value}` with source label `"env.tag"`.

Only env vars that are currently set are included (uses `os.LookupEnv`).

---

## config tag (kong provider)

The `config:""` tag is read by `kong/provider` to map kong flags to kongfig dot-paths:

```go
type CLI struct {
    APIKey string `kong:"--api-key" config:"api-key" env:"APP_API_KEY"`
}
```

Without `config:"api-key"`, `kong/provider` would convert the flag name `"api-key"` → `"api.key"` (hyphens become dots), creating a nested key rather than a flat one. The `config:""` tag forces the exact dot-path.

`config:"-"` excludes a flag from kongfig entirely (e.g. flags that only control CLI behavior).
