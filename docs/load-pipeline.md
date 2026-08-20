# Load pipeline

## Overview

`kongfig.Load` is the high-level entry point. `kongfig.LoadParsed` is the low-level
entry point for callers that already have a `map[string]any`.

## Call flow

```
Kongfig.Load(ctx, provider, opts...)
  ├─ provider.Load(ctx)                   → map[string]any
  ├─ provider.ProviderInfo()               → source label + kind
  ├─ collision detection (env.* sources)
  ├─ parser snapshot (ParserProvider)
  ├─ data snapshot (ProviderDataSupport)
  └─ commitLayer(data, source, parser, providerMeta)
       ├─ stamp LayerMeta{ID, Name, Kind, Data}
       ├─ applyTransforms
       ├─ mergeInto(k.data, ...) → SourceMeta per path
       ├─ append Layer{Data, Parser, Meta}
       └─ fire OnLoad hooks
```

## Layer identity model

Each loaded layer carries a `LayerMeta` struct with four fields:

| Field       | Purpose                                                              | Example                        |
| ----------- | -------------------------------------------------------------------- | ------------------------------ |
| `Meta.ID`   | Unique per-Load stamp. The order reflects the load sequence          | `SourceID(3)`                  |
| `Meta.Name` | Stable provenance label (source string passed at load time)          | `"xdg.yaml"`, `"env.tag"`      |
| `Meta.Kind` | Provider category. Set by the provider, or derived via `inferKind()` | `"file"`, `"env"`              |
| `Meta.Data` | Provider-specific annotation data (file path, env var name)          | `file.SourceData{Path: "..."}` |

`Name` comes from `ProviderInfo()`, which `Provider` requires. Built-in providers set
`Kind` explicitly. Custom providers get `Kind` from `inferKind(name)`. If the provider
implements `ProviderDataSupport`, `Data` comes from that interface.

## LoadParsed options

`LoadParsed(data map[string]any, source string, opts ...LoadOption)` accepts:

- `WithParser(p)` — attaches a parser for native-format rendering in `--layers` mode
  and registers it for `--format` selection.

- `WithProviderData(d)` — attaches `ProviderData` for structured source annotations
  (overrides any `ProviderDataSupport` on the provider).

- `WithSilenceCollisions(keys...)` — suppresses env-collision warnings for env.* sources.
  Pass no keys to silence all warnings for this call.

## Merge strategy

The merge is **last-writer-wins per leaf path**. Each `Load` call overwrites the existing value at the same dot-path. The merge descends into a sub-map, so the sibling keys survive. Load order is the only control over priority, and a later load beats an earlier one. An array merge, and every other non-overwrite strategy, needs an explicit `SetMergeFunc` for the specific path.

## Layer snapshots

After each `Load`, kongfig stores a **deep clone of the post-transform data of the provider** as a `Layer` struct. Kongfig takes the snapshot before the merge into `k.data`, and never modifies it afterwards. `--layers` shows this snapshot. It is the full contribution of each source, whatever survived the merge. An on-demand reload of the providers was rejected, because a provider can have side effects, or can already be closed.

## Watch reload path

On watch reload, `reloadEntry` replays the **full ordered pipeline** rather than
appending a new layer:

1. Kongfig updates the snapshot of the reloaded provider with the new data.
2. Kongfig replays all the pipeline entries in registration order:
   - A **provider entry** merges from its stored (post-transform) snapshot.
   - A **derive entry** re-runs its function against the accumulated state at
     that position. It sees only the layers registered _before_ it.
3. OnLoad hooks fire against the proposed state. A hook error rejects the reload
   and leaves the earlier state unchanged.
4. If all the hooks pass, kongfig commits `k.data`, the provenance, and the
   pipeline snapshot atomically.

Every derived value therefore matches the current provider data.
See [Watch — Derives and pipeline replay](watch.md#derives-and-pipeline-replay)
for an example.

## Source label conventions

- Env providers: `env.<tag>` prefix, for example `env.tag`, `env.prefix`, `env.kong`
- File providers: `<discoverer>.<format>`, for example `xdg.yaml` or `workdir.toml`, Kind=`"file"`
- Flag providers: `flags`
- Defaults: `defaults`
- Derived/computed values: `derived`
