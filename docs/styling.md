# Styling

Kongfig uses a `Styler` interface to apply optional terminal styling to rendered config
output. Two implementations ship in the core library; a third lives in a separate module.

---

## Built-in stylers

### style/plain — no styling (default)

`plain.New()` returns tokens unchanged. It is part of the core module and has no external
dependencies. Use it when writing to a file, pipe, or any non-terminal destination.

```go
import "github.com/pmarschik/kongfig/style/plain"

kf.Render(ctx, os.Stdout, plain.New())
```

### style/charming — lipgloss terminal colors

`charming` provides a lipgloss-backed `Styler` driven by a lipmark `theme.Registry`.
It is a separate module — get only what you need:

```bash
go get github.com/pmarschik/kongfig/style/charming
```

#### Quick start

```go
import (
    "github.com/pmarschik/kongfig/style/charming"
    "github.com/pmarschik/lipmark/theme"
)

reg := theme.NewWithOptions(theme.WithDefaults())
s := charming.New(reg, "auto")   // resolves styles from the "auto" theme set
kf.Render(ctx, os.Stdout, s)
```

#### Customizing colors

Register `ConfigStyleDefs` and/or `LayerStyleDefs` before resolving the theme set:

```go
reg.RegisterStruct("auto", charming.ConfigStyleDefs{
    Key:        theme.StyleDef{Foreground: "#7aa2f7", Bold: true},
    Value:      theme.StyleDef{Foreground: "#c0caf5"},
    Number:     theme.StyleDef{Foreground: "#ff9e64"},
    Derived:    theme.StyleDef{Foreground: "#e0af68"},
    Redacted:   theme.StyleDef{Foreground: "#f7768e", Bold: true},
    CodecValue: theme.StyleDef{Foreground: "#bb9af7", Italic: true},
})

reg.RegisterStruct("auto", charming.LayerStyleDefs{
    Flags:    theme.StyleDef{Foreground: "#9ece6a"},
    Env:      theme.StyleDef{Foreground: "#7dcfff"},
    File:     theme.StyleDef{Foreground: "#bb9af7"},
    Defaults: theme.StyleDef{Foreground: "#565f89"},
})
```

Style name constants are exported from the `charming` package (e.g. `charming.ConfigKey`,
`charming.LayerFlags`) in case you need to register individual styles by name.

#### kong/charming integration

When using `kong/charming`, you get a `Styler` pre-wired to the same registry so that
`Flags.Render` and the kong help output share consistent colors:

```go
import kongcharming "github.com/pmarschik/kongfig/kong/charming"

opts := kongcharming.Options(kf, reg, "auto")
k, _ := kong.New(&cli, opts...)

// Retrieve the styler for manual Render calls:
s := kongcharming.Styler(reg, "auto")
cli.Show.Render(ctx, os.Stdout, kf, s)
```

See [cli.md](cli.md) for the full `kong/charming` wiring example.

---

## Implementing a custom Styler

`Styler` is a plain Go interface. Embed `kongfig.BaseStyler` to inherit pass-through
defaults, then override only the methods you need:

```go
import kongfig "github.com/pmarschik/kongfig"

type BoldKeyStyler struct{ kongfig.BaseStyler }

func (BoldKeyStyler) Key(s string) string { return "\033[1m" + s + "\033[0m" }
```

### Styler method reference

| Method               | Called for                                                        |
| -------------------- | ----------------------------------------------------------------- |
| `Key(s)`             | Config key tokens                                                 |
| `String(s)`          | String leaf values                                                |
| `Number(s)`          | Integer and float leaf values                                     |
| `Bool(s)`            | Boolean leaf values (`true` / `false`)                            |
| `Null(s)`            | Null / nil leaf values                                            |
| `BraceOpen(s)`       | Opening bracket (`{`, `[`)                                        |
| `BraceClose(s)`      | Closing bracket (`}`, `]`)                                        |
| `Comment(s)`         | Comment prefix tokens                                             |
| `Annotation(src, s)` | Source annotation text (legacy; prefer `SourceKind`/`SourceData`) |
| `SourceKind(s)`      | Kind token in a structured source annotation (e.g. `file`, `env`) |
| `SourceData(s)`      | Path or data segment of a structured source annotation            |
| `SourceKey(s)`       | Specific key reference (e.g. `$APP_HOST`, `--log-level`)          |
| `Redacted(s)`        | Redacted value placeholder (`<redacted>`)                         |
| `Codec(s)`           | Values encoded by a registered `Codec`                            |

### Renderer conventions

Renderers (parsers implementing `Renderer`) must follow two rules when using a `Styler`:

1. **Never call `s.Value(formatted)` on a leaf.** Always use:
   ```go
   kongfig.RenderValue(s, v, formattedString)
   ```
   This dispatches to `s.Redacted`, `s.Codec`, or the appropriate type method centrally.

2. **Never format source annotations inline.** Always use:
   ```go
   line += "  " + s.Comment("# ") + kongfig.RenderSourceAnnotation(src, path, s, opts)
   ```
   This delegates to `LayerMeta.RenderAnnotation` for structured sources (with
   `SourceKind`/`SourceData`/`SourceKey`) and falls back to `FormatSourceAnnotation` +
   `s.Annotation` for legacy string-only sources.
