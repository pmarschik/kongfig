<!-- read_when: adding codec support, registering codecs, or implementing a custom Codec[T] -->

# Codecs

Codecs perform bidirectional transformation between raw config values (strings from env/flags,
native types from file parsers) and typed Go values. They serve two purposes:

1. **Decode** — convert raw config values to a Go type at `Get[T]` time (e.g. `"1h30m"` → `time.Duration`)
2. **Encode** — convert the typed value back to a canonical string at render time (styled with `Styler.Codec`)

Decode-only codecs skip the encode step — the raw value is shown verbatim by renderers.

---

## Ready-made codecs (`codec` package)

Import `github.com/pmarschik/kongfig/codec` for standard library types:

| Name                       | Type             | Example                  |
| -------------------------- | ---------------- | ------------------------ |
| `codec.IP`                 | `net.IP`         | `"192.168.1.1"`          |
| `codec.Duration`           | `time.Duration`  | `"1h30m"`                |
| `codec.URL`                | `*url.URL`       | `"https://example.com"`  |
| `codec.Regexp`             | `*regexp.Regexp` | `"^foo.*"`               |
| `codec.TimeRFC3339`        | `time.Time`      | `"2024-01-15T10:00:00Z"` |
| `codec.TimeDate`           | `time.Time`      | `"2024-01-15"`           |
| `codec.TimeFormat(layout)` | `time.Time`      | custom layout            |

### Register all at once

```go
import (
    kongfig "github.com/pmarschik/kongfig"
    "github.com/pmarschik/kongfig/codec"
)

kf := kongfig.New(kongfig.WithCodecRegistry(codec.Default))
```

### Register individually

```go
kf := kongfig.New(
    kongfig.WithCodec("ip", codec.IP),
    kongfig.WithCodec("duration", codec.Duration),
    kongfig.WithCodec("time-kitchen", codec.TimeFormat(time.Kitchen)),
)
```

---

## Auto-detection with `NewFor[T]`

`kongfig.NewFor[T]()` inspects the struct tags and Go types of `T` and automatically wires
codecs from the registry to matching paths. Two resolution strategies:

1. **Explicit** — `codec=name` struct tag: `kongfig:"addr,codec=ip"`
2. **Type-based** — first registered codec whose Go type matches the field type

```go
type Config struct {
    Addr    net.IP    `kongfig:"addr"`                       // auto-matched: net.IP → codec.IP
    Created time.Time `kongfig:"created,codec=time-rfc3339"` // explicit codec name
    Updated time.Time `kongfig:"updated,codec=time-date"`    // different layout for same type
}

kf := kongfig.NewFor[Config](kongfig.WithCodecRegistry(codec.Default))
```

When multiple codecs exist for the same Go type (e.g. two `time.Time` codecs), the first
registered wins for auto-detection. Use explicit `codec=name` tags to select a specific one.

---

## Per-path registration

Use `kf.RegisterCodec` to attach a codec to a specific path after construction:

```go
kf.RegisterCodec("addr", kongfig.Of(codec.IP))
kf.RegisterCodec("timeout", kongfig.Of(codec.Duration))
```

Or at construction time with `WithCodec`:

```go
kf := kongfig.New(kongfig.WithCodec("ip", codec.IP))
```

---

## Decode-only codecs

When you only need to normalize a value at `Get` time (no render encoding), use `DecodeOnly`:

```go
splitComma := func(v any) any {
    if s, ok := v.(string); ok {
        return strings.Split(s, ",")
    }
    return v
}
kf.RegisterCodec("tags", kongfig.DecodeOnly(splitComma))
```

The raw string is preserved in the store and shown verbatim by renderers. Only `Get[T]` sees the decoded value.

---

## Custom codecs

Implement `kongfig.Codec[T]` directly:

```go
var MyCodec = kongfig.Codec[MyType]{
    Decode: func(v any) (MyType, error) {
        s, ok := v.(string)
        if !ok {
            return MyType{}, fmt.Errorf("expected string, got %T", v)
        }
        return parseMyType(s)
    },
    Encode: func(t MyType) string {
        return t.String()
    },
}
```

Then register it:

```go
kf := kongfig.New(kongfig.WithCodec("mytype", MyCodec))
// or build a registry:
r := kongfig.NewCodecRegistry()
r.Register("mytype", kongfig.Of(MyCodec))
kf := kongfig.New(kongfig.WithCodecRegistry(r))
```

### Decode contract

- Handle both string input (from env/flags) and already-decoded input (from file parsers)
- Pass through if already the target type: `if t, ok := v.(T); ok { return t, nil }`
- Return a clear error on invalid input

### Encode contract

- Return a single-line canonical string
- `nil` Encode is valid — means no render encoding (same as `DecodeOnly`)

---

## Rendering codec values

At render time, bidirectional codecs (those with `Encode != nil`) produce a `RenderedValue`
with `Encoded: true`. Renderers style these via `Styler.Codec(formatted)` — visually distinct
from plain `String` values. Use `render.Value(s, v, formatted)` to handle this automatically.
