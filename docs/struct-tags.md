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
| `kongfig:"my-key,overflow"`       | As `inline`, and kept compact even past the terminal width        |
| `kongfig:"my-key,omitempty"`      | Key `"my-key"`, left out of output while it holds nothing         |
| `kongfig:"my-key,sortby=field"`   | Map `"my-key"`, entries ordered by `field` inside each entry      |

`parseTag(tag, fieldName)` parses the name part. An empty name becomes `strings.ToLower(fieldName)`.

### Nested structs

Kongfig treats a struct field as a namespace when its type is a struct, or a pointer to a struct. It reads the fields of that struct and adds them under the parent key:

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

Kongfig squashes an anonymous (embedded) struct into the parent namespace:

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

Kongfig always stores the values as strings. At decode time, `Get[T]` converts them to the
declared field type with mapstructure.

Single-quoted values allow commas and equals signs in defaults:

```go
Sep string `kongfig:"sep,default=','"`  // default value is ","
```

`TagDefaults[T]()` includes only the fields with a `default=` annotation, and omits the
rest. `structs.Defaults(instance)` behaves differently: it includes every non-zero field
value.

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

`NewFor[T]` derives the texts automatically, and no extra call is necessary. Internally it
uses `schema.HelpTextPaths[T]()`, which reflects on `T` and returns a `map[string]string`
of dot-path → help text:

```go
texts := schema.HelpTextPaths[Config]()
// {"host": "hostname or IP to listen on", "port": "TCP port", "labels": "arbitrary key=value labels"}
```

Register a set explicitly with the `WithHelpTexts(texts)` option. This option is
instance-level, for example when you construct with `New` instead of `NewFor`. To override
the registered set for a single render call, use `WithRenderHelpTexts(texts)`.

Use single-quoted values to allow commas and equals signs:

```go
Sep string `kongfig:"sep,help='separator; default is ','"`
```

**Prefix matching for maps and slices**: when the path of a field is `"labels"`, the help
text also fires for rendered leaf paths. Two examples are `"labels.key1"` and
`"labels[0]"`. Kongfig
emits each help text at most once per render call. The first match wins, and the later
paths under the same key stay silent.

**A namespace is described where it is written**: `help=` on a struct or map field is the
text of that field. It is not the text of its children. A format with a place for it puts
it there. The TOML renderer writes it above the `[table]` header, and once above the first
`[[block]]` of a table array.

That position also spends the once-per-render mark. The prefix match therefore cannot
re-attach the text to the first key inside the table, where it reads as documentation of
that key. A renderer with no place for such a text omits it.

---

## redacted option

`kongfig:"name,redacted"` marks a field as sensitive. `structs.RedactedPaths[T]()` reflects on `T` and returns the set of dot-paths that are redacted.

### Inheritance

Nested struct fields inherit the redaction. A parent marked `redacted` makes all its leaf descendants redacted by default. An individual leaf can override this:

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

Kongfig populates the path set at startup from the config struct type, not at runtime. To include a new field, mark it `redacted` and call `New()` again.

---

## inline option

`kongfig:"name,inline"` marks a table as a candidate for a compact one-line form, where
the output format supports it. The mark is format-agnostic. The TOML parser writes an
inline table (`work = {path = "/w", color = "blue"}`) instead of a `[buckets.work]`
section. The YAML parser writes a flow mapping (`work: {path: /w, color: blue}`) instead
of a nested block mapping. A format with no compact form ignores the mark.

The type of the field decides what the mark applies to:

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

`inline=N` caps the number of direct keys that the table can hold. A table with more keys
becomes a regular section. Plain `inline`, with no `=N`, uses the default of the renderer:
`toml.DefaultInlineMaxKeys` or `yaml.DefaultInlineMaxKeys` (3).

`schema.InlineTablePaths[T]()` returns the marked paths as `[]schema.InlineTableEntry`
(`{Path, MaxKeys, Overflow}`). `NewFor[T]` calls it automatically and publishes the result
under [`kongfig.InlineTablesKey`]. A render and a config-file write both read it, with no
extra wiring:

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

y := yaml.New(
    yaml.WithInlineMaps("buckets.*"),
    yaml.WithInlineMaxKeys(4),
)
```

See [render-pipeline.md](render-pipeline.md#toml-layout) for how the key limit interacts
with terminal width.

---

## overflow option

`kongfig:"name,overflow"` keeps the compact form even when it does not fit the terminal.
The alternative is the roomier shape of the format. TOML gets a `[section]`, or
`[[section]]` blocks for an array of tables. YAML gets a block mapping. `overflow` implies
`inline`, so it stands alone:

```go
type Config struct {
    Rules []Rule `kongfig:"rules,overflow"`   // a line per rule, however narrow the terminal
}
```

Use it where the shape carries the meaning. A list of two-key rules reads as a list, even
when the lines run past the edge of the window. A section per rule buries that meaning. A
config-file write never consults the terminal width, so the mark changes only rendered
output.

`NewFor[T]` publishes the marked paths under [`kongfig.InlineOverflowKey`] alongside the
inline marks. The parser route is `toml.WithInlineOverflow("rules")` or
`yaml.WithInlineOverflow("rules")`.

---

## omitempty option

`kongfig:"name,omitempty"` leaves the key out of the output while it holds nothing. These
values count as nothing: an empty string, a zero number, `false`, an empty list, an empty
map, or an empty table. A missing value also counts as nothing. An unmarked key still shows its zero value,
because a zero is also config.

The option is also read from the `toml:` and `yaml:` tags, so a struct that already
carries encoder tags needs no second mark:

```go
type Config struct {
    Tags  []string `kongfig:"tags,omitempty"`
    Alias string   `kongfig:"alias" toml:"alias,omitempty"`
}
```

Kongfig never treats a redacted value as empty. The placeholder represents a value that
is set, and a removal of the key says the opposite.

The mark applies to rendered output and to TOML config files written through
`toml.Parser.MarshalCtx`. `NewFor[T]` publishes the marked paths under
[`kongfig.OmitEmptyKey`]. A renderer that cannot express absence ignores the mark. In TOML
the mark also reaches inside inline tables and table arrays, where the reader cannot
remove the key by hand later. If a table or an object has nothing to show, kongfig omits
it together with its key, and does not write it empty.

## sortby option

`kongfig:"name,sortby=field"` orders the entries of a map by a value inside them rather
than alphabetically. Prefix the value name with `-` for descending order, which is the
usual choice for a priority value:

```go
type Rule struct {
    Path     string `kongfig:"path"`
    Priority int    `kongfig:"priority"`
}

type Config struct {
    Rules map[string]Rule `kongfig:"rules,sortby=-priority"`
}
```

Renders and writes the highest priority first, whatever the keys are called:

```toml
[rules.deny-admin]
path = "/admin"
priority = 10

[rules.allow-all]
path = "/"
priority = 1
```

The spec can be dotted (`sortby=meta.priority`) to reach a value one level down. A map
inside a map value type takes the same mark. Kongfig matches it through a `*` segment, so
a mark on `Rules` inside `Profile` applies at `profiles.*.rules`.

The mark belongs on a map. The children of a struct are distinct fields in declaration
order. A value inside them says nothing about that order. Kongfig ignores the mark on
anything but a map.

Entries whose value is missing, or of a kind the others are not, read last in both
directions. Kongfig cannot place them among the rest. A sort of them as a zero claims a
position that they do not have. Entries that compare equal keep the order from before the
mark, so a tie uses the document order instead of the map iteration order.

`NewFor[T]` publishes the marked paths under [`kongfig.KeySortByKey`], and
`render.OrderedKeys` applies the sort. The sort therefore reaches rendered output and
written config files alike. It sits below an explicit `WithRenderKeyOrder` and a
`WithRenderKeySort` comparator, and above the order that the documents and the struct
fields report — see [docs/render-pipeline.md](render-pipeline.md#key-order).

---

## env tag (structs provider)

`structs.TagEnv[T]()` reads the `env:""` tag. It loads the env var values under their kongfig path:

```go
type Config struct {
    Host string `kongfig:"host" env:"APP_HOST"`
}
```

`structs.TagEnv[Config]()` reads `os.Environ()`, finds `APP_HOST`, and returns `{"host": value}` with source label `"env.tag"`.

The provider includes only the env vars that are set (it uses `os.LookupEnv`).

---

## config tag (kong provider)

`kong/provider` reads the `config:""` tag to map kong flags to kongfig dot-paths:

```go
type CLI struct {
    APIKey string `kong:"--api-key" config:"api-key" env:"APP_API_KEY"`
}
```

Without `config:"api-key"`, `kong/provider` converts the flag name `"api-key"` to `"api.key"`, because hyphens become dots. The result is a nested key instead of a flat one. The `config:""` tag forces the exact dot-path.

`config:"-"` excludes a flag from kongfig entirely, for example a flag that only controls CLI behavior.
