# Provenance & Filtering

## What is provenance?

`Provenance` records, for each leaf dot-path in the merged config, which source last wrote it:

```
path       → source label
"host"     → "env.tag"
"port"     → "flags"
"log.level" → "file"
"timeout"  → "defaults"
```

Every `mergeInto` call updates it, which means every `Load` call. The last writer wins, and it overwrites the provenance that an earlier source recorded for that path.

`k.Provenance()` returns a snapshot copy safe for concurrent use.

## Where a path was written

Provenance names the layer. For a file-backed layer, it can also give the exact spot:

```go
if src, ok := k.SourceFor("db.port"); ok {
    if pos := src.Layer.PositionOf("db.port"); pos != nil {
        fmt.Println(pos) // /etc/app/config.yaml:3:9
    }
}
```

`PositionOf` returns `nil` for a layer with no document (env, flags, defaults). It also
returns `nil` for a parser that reports no positions. YAML reports them, and TOML does not.
See [architecture.md](architecture.md#source-positions).

## Derived values

`KindDerived = "derived"` is a source kind constant for layers that represent computed
or inherited defaults rather than explicit user input. It is a **display annotation only** —
there is no expression language or computed-value machinery.

A provider that loads a derived value or a default value must set
`Kind: kongfig.KindDerived` in its `ProviderInfo()` return value. A renderer such as
`style/charming` gives a derived value a dimmed style. The user can then tell an overridden
default from an unchanged one.

## Source labels

Source labels are the canonical short names for provenance and layer filtering:

| Label        | Meaning                                  |
| ------------ | ---------------------------------------- |
| `defaults`   | Struct or file defaults                  |
| `file`       | Config file                              |
| `xdg`        | XDG config file                          |
| `workdir`    | Workdir-local config file                |
| `env`        | Environment variables (generic)          |
| `env.tag`    | Struct tag env vars (`env:""` field tag) |
| `env.kong`   | Env vars read by kong flag resolver      |
| `env.prefix` | Prefix-stripped env vars                 |
| `flags`      | Explicit CLI arguments                   |
| `derived`    | Computed/inherited values                |

Env sub-sources share the `env` prefix. This prefix gives three features:

- Collision detection: two `env.*` providers that write the same path trigger a warning.
- Merged display: `env.*` providers collapse to `"env"` in the merged view (non-verbose mode).
- Filtering: `WithRenderFilterSource([]string{"env"})` matches `env`, `env.tag`, `env.kong`, `env.prefix`.

## Source label design

Source labels are plain strings. A typed enum was rejected, because it blocks a custom label from a third-party provider, such as `"consul"` or `"vault"`. A structured type was rejected, because the `env.` prefix already encodes the one case where a structural match matters. The `env.*` prefix is the only convention with runtime significance, and everything else is advisory.

A provider that uses `"environment"` instead of `"env"` gets no collision detection and no `env` layer grouping. This documentation is the only enforcement, and the type system is not.

## FilterSource semantics

`WithRenderFilterSource(filters)` accepts a list of filter entries. An empty list is no filter, and it shows everything.

**Exclude entries** (`no-` prefix): exclude any source matching the prefix.

- `"no-defaults"` excludes `"defaults"`
- `"no-env"` excludes `"env"`, `"env.tag"`, `"env.kong"`, `"env.prefix"`

**Include entries** (no prefix): with one or more positive entries, only a matching source passes.

- `"env"` allows only `"env"`, `"env.tag"`, `"env.kong"`, `"env.prefix"`
- `"flags"` allows only `"flags"`

The two kinds combine: `["env", "no-env.tag"]` means every env sub-source except `env.tag`.

**Prefix matching**: `"env"` matches `source == "env"` OR `strings.HasPrefix(source, "env.")`.

`render.BuildFilterSource(map[string]bool)` builds a filter list from a map of `layerName → show`.

## Layers vs merged provenance

`k.Layers()` returns the snapshot of each provider in load order. Each `Layer.Data` is the pre-merge data for that provider, and not the merged result. `--layers` shows this data.

The merged `k.data` is the last-writer-wins result across all the layers. Its provenance is in `k.Provenance()`.

The two views have different purposes:

- **Merged view**: "what is the current effective value and who last set it?"
- **Layer view**: "what did each provider contribute, independent of priority?"
