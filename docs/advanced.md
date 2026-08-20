# Advanced Features

Reference for power-user features that do not warrant a dedicated page.
For CLI integration (`ScanFlag`, kong packages) see [cli.md](cli.md).

---

## Get options

### At — sub-tree decoding

```go
func At(path string) GetOption
```

Decodes the sub-tree at the dot-delimited `path` instead of the full config root.

```go
dbCfg, _ := kongfig.Get[DBConfig](kf, kongfig.At("db"))
// decodes kf["db"] into DBConfig
```

### Strict — fail on unknown keys

```go
func Strict() GetOption
```

Returns an error if any key in the config has no matching struct field.

```go
cfg, err := kongfig.Get[Config](kf, kongfig.Strict())
```

### Default decode hooks

`Get` always applies these mapstructure hooks before user-supplied `TypedDecodeHook` hooks:

- `TextUnmarshallerHookFunc` — kongfig decodes any type that implements `encoding.TextUnmarshaler` from strings, for example `net.IP`, `time.Time` and `net.IPNet`.
- `StringToTimeDurationHookFunc` — `time.Duration` fields decode from strings like `"1h30m"`.

These cover the most common stdlib types with no extra wiring.

### TypedDecodeHook — custom string-to-type conversion

```go
func TypedDecodeHook[T any](fn func(string) (T, error)) GetOption
```

Registers a conversion hook for string config values into non-scalar Go types.
Use this for types that do not implement `encoding.TextUnmarshaler`, such as `*url.URL` and `*regexp.Regexp`.

```go
cfg, err := kongfig.Get[Config](kf,
    kongfig.TypedDecodeHook(func(s string) (*url.URL, error) {
        return url.Parse(s)
    }),
)
```

**TypedDecodeHook vs AddTransform:** these operate at different pipeline stages and are not interchangeable.

`TypedDecodeHook` runs at decode time and does not touch the stored map. Use it when your struct needs a custom Go type, but the config must still store and render a plain string.

`AddTransform` runs at load time and changes what the map stores. Use it for value normalization, for example a trim or a lowercase, that must persist in the map and affect rendering. CAUTION: do not store a custom type like `net.IP` through `AddTransform`. It breaks renderers.

### WithCodec — named bidirectional codec

```go
func WithCodec[T any](name string, c Codec[T]) Option
func WithCodecRegistry(r *CodecRegistry) Option
```

Registers a named bidirectional codec (`Decode any→T`, `Encode T→string`) on the Kongfig
instance. Codecs serve two purposes:

1. **Load-time decode**: at load time, kongfig decodes the raw config value into the typed
   value. Two conditions trigger this decode: a `codec=name` struct tag annotation, or a Go
   field type that matches a registered codec. Validators see the typed value.
2. **Render-time encode**: before a render, kongfig encodes each typed value in the map to
   its canonical string. The Styler dispatches to `s.Codec(formatted)`, so themes can show
   codec-transformed values differently.

The `codec` sub-package provides ready-made values for common stdlib types:

```go
import "github.com/pmarschik/kongfig/codec"

// Easiest — register all standard codecs at once:
kf := kongfig.NewFor[Config](kongfig.WithCodecRegistry(codec.Default))

// Or selectively:
kf := kongfig.New(
    kongfig.WithCodec("ip", codec.IP),
    kongfig.WithCodec("duration", codec.Duration),
    kongfig.WithCodec("time-rfc3339", codec.TimeRFC3339),
    kongfig.WithCodec("time-date", codec.TimeDate),
    kongfig.WithCodec("url", codec.URL),
    kongfig.WithCodec("regexp", codec.Regexp),
    // custom layout:
    kongfig.WithCodec("time-kitchen", codec.TimeFormat(time.Kitchen)),
)

type Config struct {
    Addr    net.IP    `kongfig:"addr"`                       // auto-matched by type via NewFor
    Created time.Time `kongfig:"created,codec=time-rfc3339"` // explicit
    Updated time.Time `kongfig:"updated,codec=time-date"`    // different layout
}
```

`Encode` converts a stored typed value to its canonical string for rendering.
If `Encode` is nil, kongfig does no render-time encoding, and the renderers get the decoded value unchanged.
`Decode` accepts `any`. It must pass a value that is already of type `T` through unchanged, and parse a string.

Use `NewCodecRegistry` + the method form to build a shared registry across multiple instances:

```go
reg := kongfig.NewCodecRegistry()
reg.Register("ip", kongfig.Of(codec.IP)).
    Register("my-type", kongfig.Of(myCodec))

kf1 := kongfig.NewFor[Config1](kongfig.WithCodecRegistry(reg))
kf2 := kongfig.NewFor[Config2](kongfig.WithCodecRegistry(reg))
```

### RegisterCodec / AddCodec — instance-level registration

```go
func (k *Kongfig) RegisterCodec(path string, e CodecEntry)
func (k *Kongfig) AddCodec(path string, fn func(any) any)
```

Both register a per-path codec on an existing `*Kongfig` instance. Construction time is not the only opportunity.

**`RegisterCodec`** registers a full bidirectional codec for a path. Use `Of[T]` to wrap:

```go
kf.RegisterCodec("addr",    kongfig.Of(codec.IP))
kf.RegisterCodec("timeout", kongfig.Of(codec.Duration))
```

The Encode direction runs at render time and triggers `s.Codec(formatted)` styling.

**`AddCodec`** is decode-only. Use it for value normalization that needs no render-time
encoding, where the render shows the normalized form directly:

```go
kf.AddCodec("mode", func(v any) any {
    if s, ok := v.(string); ok { return strings.ToLower(s) }
    return v
})
```

---

## Batch loading: MustLoadAll

```go
func MustLoadAll[P Provider](ctx context.Context, k *Kongfig, providers []P, opts ...LoadOption)
```

Calls `k.MustLoad` for each provider in order. Use it for a slice of discovered file providers.

```go
providers := fileprovider.DiscoverAll(ctx, discover.UserDirs(), discover.Workdir(), yamlparser.Default)
kongfig.MustLoadAll(ctx, kf, providers)
```

---

## LoadParsed — pre-parsed data

```go
func (k *Kongfig) LoadParsed(data ConfigData, source string, opts ...LoadOption) error
```

Merges a pre-parsed `ConfigData` map directly, with no provider load.
Kongfig applies the transforms, and the `OnLoad` hooks fire as normal.
Use this method for test fixtures, for custom readers, or when you already hold a `map[string]any`.

```go
kf.LoadParsed(kongfig.ConfigData{"port": 9090}, "test-override")
```

Optional: attach a parser for `--layers` rendering with `WithParser`, or provider metadata with `WithProviderData`.

---

## Custom merge strategies

By default, later `Load` calls overwrite earlier values at the same key.
Use `SetMergeFunc` to change the merge behavior for specific paths:

```go
import "github.com/pmarschik/kongfig/mergefuncs"

kf.SetMergeFunc("plugins", mergefuncs.AppendSlice)  // accumulate across loads
kf.SetMergeFunc("tags",    mergefuncs.UnionSet)     // deduplicated union
kf.SetMergeFunc("servers", mergefuncs.ReplaceSlice) // explicit replace (the default)
```

Available strategies in `mergefuncs`:

| Strategy       | Behavior                                                   |
| -------------- | ---------------------------------------------------------- |
| `AppendSlice`  | Appends incoming items to the existing slice               |
| `ReplaceSlice` | Replaces the destination slice entirely (last-writer-wins) |
| `UnionSet`     | Merges two slices, deduplicating by string representation  |

---

## OnLoad hook

```go
func (k *Kongfig) OnLoad(fn func(LoadEvent) LoadResult)
```

Fires after each `Load` call, before the data is committed.
Return `LoadResult{Err: err}` to reject the load. Kongfig then keeps `k.data` unchanged.

- `LoadEvent.Delta` — keys that changed in this load
- `LoadEvent.ProposedData` — full merged state after this load (read the proposed state here, not `k.All()`)
- `LoadEvent.Layer` — the layer of this load

The `validation` package uses `OnLoad` internally when `WithValidateOnLoad` is set.

---

## TagDefaults

```go
// in package structs (github.com/pmarschik/kongfig/providers/structs)
func TagDefaults[T any]() Provider
```

Returns a `Provider` that reads the `default=` annotations from the `kongfig` struct tags of `T`. A populated instance is not necessary.

```go
type Config struct {
    Host string `kongfig:"host,default=localhost"`
    Port int    `kongfig:"port,default=8080"`
    Name string `kongfig:"name"` // omitted: no default=
}
kf.MustLoad(ctx, structs.TagDefaults[Config]())
```

See [struct-tags.md](struct-tags.md) for the full `kongfig` tag reference.
