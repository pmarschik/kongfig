<!-- read_when: starting work on this repo, planning new features, clarifying scope -->

# Intent

## What kongfig is

A layered config library for Go CLIs. The core proposition is more than "load config from
multiple sources". Kongfig shows the user exactly where each value came from. A
`show-config`-style command renders the annotated output in the native format of each
source.

Most config libraries answer "what is the value of X?".
Kongfig also answers "which source set X, and what did every other source contribute?".

## Design principles

- **Kongfig preserves the layers, and does not only merge them.** Each `Load` call records
  a snapshot. The merged view is derived from those snapshots. The layer snapshots are
  primary, and they stay inspectable.
- **Provenance is a first-class citizen.** Every leaf value tracks its source label. A
  renderer annotates a value inline, so the user sees
  `host: prod.example.com  # env $APP_HOST`.
- **Rendering is part of the public API.** `Renderer`, `Styler`, `OutputProvider`, and
  `LayerMeta` are public interfaces. A consumer extends them, because the library does not
  own the output.
- **Format fidelity.** A YAML file renders as YAML, TOML as TOML. `Layer.Parser` carries
  the native format, and a per-layer render uses it automatically.
- **kong-aware, not kong-required.** The core module has zero kong dependency. The
  `kong/provider`, `kong/show`, `kong/resolver`, `kong/charming` modules are opt-in.

## CLI behavior: `show-config` intent

Any CLI can embed the `kong/show.Flags` struct in its `show-config` command, or in a
`--show-config` flag. These are the intended flag combinations and their behaviors:

### Merged view (default)

No `--layers` flag. Renders the single merged map.

| Flags             | Behavior                                                                                                                                                          |
| ----------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| _(none)_          | Merged config. The format comes from the registered parsers (first registered, or YAML if none). Each value gets an inline `# source` annotation.                  |
| `--format=X`      | Force a specific output format (yaml/toml/json/env/flags). Overrides the inferred default. Not meaningful in `--layers` mode (each layer uses its native format).  |
| `--no-comments`   | Suppress every comment: the help texts and the source annotations.                                                                                                |
| `--no-provenance` | Suppress the source annotations, and keep the help comments. Merged view only — rejected together with `--layers`.                                                |
| `--no-help`       | Suppress the help comments, and keep the source annotations.                                                                                                      |
| `--redacted`      | Reveal the values that are normally hidden, such as a password.                                                                                                   |

**Note:** The default format selection is planned, and not yet implemented. That selection
reads YAML or TOML from the registered parsers. The default today is always YAML.

### Per-layer view (`--layers`)

Renders each config source as a separate section. Each layer uses its native format.
`--format` has no meaning here, because the native format wins. It applies only to a layer
with no native renderer, such as `defaults` or `derived`.

| Flags                    | Behavior                                                                                                               |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------- |
| `--layers`               | One section per source group. All `env.*` sub-sources merged into a single `env` section.                              |
| `--layers -v`            | One section per sub-source. Each env provider shown separately: `# === env (env.tag) ===`, `# === env (env.kong) ===`. |
| `--layers --no-defaults` | Exclude the defaults layer.                                                                                            |
| `--layers --no-env`      | Exclude env layers.                                                                                                    |
| `--layers --no-file`     | Exclude file/xdg/workdir layers.                                                                                       |
| `--layers --no-flags`    | Exclude the flags layer.                                                                                               |

The `--no-<source>` flags are **not built into `kong/show.Flags`**. An application wires
them manually with `BuildFilterSource` (`example/common` and `example/full`). Two variants
are possible. The first is one negatable boolean flag per source (`--no-defaults`,
`--no-env`, …). The second is a single `--sources=defaults,env,file,flags` list flag.
Neither one is in `kong/show` yet, and both are tracked as a future feature.

### Source annotation detail (`-v` / `--verbose`)

`--verbose` / `-v` is a counter flag:

- **0 (default):** the layer headers collapse the `env.*` sub-sources to `env`. A per-value
  annotation shows `# env $APP_HOST`.
- **1+ (`-v`):** the layer headers show the sub-source labels in full (`env (env.tag)`,
  `env (env.kong)`). Planned: a higher level can show more annotation detail.

### `--plain` / `--no-plain`

Available on `SimpleFlags`. Turns the ANSI color output off. With `Flags`, the caller
controls the styling and passes the correct `Styler` to `Render`.

### `--no-comments` / `--no-provenance` / `--no-help`

Comments are on by default. `--no-provenance` drops the per-value `# env.tag $APP_HOST`
annotation. `--no-help` drops the help comment above each key. `--no-comments` drops both.

`--no-provenance` is a merged-view flag. With `--layers`, the section header is what
attributes the values of a layer, and the per-value annotations are already silent
for the rendered layer. Kongfig therefore rejects the combination, and does not
ignore it silently. `--no-help` and `--no-comments` stay meaningful per layer, and
kongfig accepts them there.

### `--redacted` / `--no-redacted`

`--no-redacted` (default): every output shows `<redacted>` for a path that
`kongfig.WithRedacted` or `kongfig.NewFor[T]()` registered.
`--redacted`: shows the raw values.
