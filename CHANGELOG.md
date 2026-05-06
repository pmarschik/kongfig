## [v0.5.0] - 2026-05-06

### 🚀 Features

- _(kong/show)_ Suppress redundant per-line source annotations when rendering --layers

### 💼 Other

- _(release)_ Run go work sync + go mod tidy after pinning versions in release:prepare
- _(release)_ Add GONOSUMDB guard so go work sync/mod tidy work pre-tag

## [v0.4.0] - 2026-05-06

### 🚀 Features

- _(core)_ Add key ordering support
- _(core)_ Add Derive method for cross-field computed values
- _(providers/file)_ Composable file discoverer primitives
- _(providers/file)_ Add LocateNames FileLocator and FirstOf Discoverer combinator
- _(core)_ Add MigrationResult return type; MigrationWarnResult built-in; MigrationWarnings accumulation
- _(providers/file)_ Add LabeledDiscoverer and First for priority-chain discovery
- _(core)_ Add DeriveInput/DeriveOutput structs; pass Provenance to DeriveFn
- _(core)_ Add DeriveLoad for computing and loading provider-derived layers
- _(core)_ Add AddWarning for app-level diagnostic notices
- _(core)_ Right-align source annotations to terminal right edge
- _(core)_ Read TTY size from COLUMNS/ROWS env vars as fallback
- _(parsers/toml)_ Render []ConfigData as [[table-array]] when complex
- _(core)_ Replay pipeline on watch reload so derives re-run against preceding layers

### 🐛 Bug Fixes

- _(style/charming)_ Make derived annotation less bold
- _(parsers/yaml)_ Style block slice-of-maps keys; fix annotation indent
- _(parsers/toml)_ Style keys in multiline array inline tables via tomlValueStyled
- _(core)_ Store delta not snapshot for derived layer in --layers mode
- _(core)_ Suppress derived provenance for unchanged keys in merged view
- _(core)_ Protect merge-func paths from pruneUnchanged stripping
- _(core)_ Address review findings — docs, tests, nolint, delta test
- _(core)_ Prune unchanged replace-func results from Derive delta
- _(core)_ Preserve key order across watch provider reloads

### 💼 Other

- Disable revive unexported-return; remove 20 nolints and rewrite 6 errcheck test assertions

### 🚜 Refactor

- _(providers/file)_ Rewrite LoadConfigPaths in terms of DeriveLoad
- _(core)_ Use disposable provenance in Derive instead of pruning
- _(providers/file)_ Unify gitRootDiscoverer/jujutsuRootDiscoverer into vcsRootDiscoverer
- _(core)_ Make derive a full layer entry; drop redundant k.layers slice
- _(providers/file)_ Extract compositeDiscoverer into compose.go
- _(core)_ Replace MigrationResult{Err,Warning} with {Severity,Message}
- _(providers/file)_ Make LabeledDiscoverer implement innerDiscoverer
- _(core)_ Eliminate nolint overrides; extract helpers; add render opts structs
- _(parsers/json)_ Encapsulate s/p/align in jsonRenderOpts struct
- _(providers/file)_ Replace nolint:nilerr with nilerr-native // ignored comment
- _(kong/show)_ Replace nolint:gosec with explicit bounds check for fd cast

### 📚 Documentation

- Document pipeline replay, TOML table-array, and context options reference

### 🧪 Testing

- _(core)_ Add test for unchanged Derive values retaining original provenance
- _(providers/file)_ Add ComposeAll and UpwardFunc tests

## [v0.3.0] - 2026-04-29

### 🚀 Features

- _(parsers)_ Enhance yaml and toml parsing
- _(parsers)_ Handle typed slice/map/struct via reflection in yaml and toml renderers
- _(core)_ Inherit render options in RenderLayers; add WithRenderBlockCollections
- _(providers/file)_ Short display paths by default; WithLongDisplayPaths for all discoverers

### 💼 Other

- _(release)_ Add changelog footer links; strip heading from release notes
- _(kong/show)_ Add kong/show as dedicated workspace module with dependencies
- Go mod tidy — update transitive dep versions (lipgloss v2.0.3, x/sys v0.43.0)

### 📚 Documentation

- _(core)_ Add ProviderFieldNamesSupport and ParserProvider to provider checklist

### 🧪 Testing

- _(parsers)_ Add typed slice rendering tests for yaml and toml

## [v0.2.0] - 2026-04-29

### 🚀 Features

- _(schema)_ Add HelpTextPaths[T] struct-tag reflector
- _(core)_ Help text once-default with prefix matching; add AlignAnnotationsCtx above-line fallback
- _(style)_ Muted colors for provenance annotations; apply syntax style to annotation parens
- _(providers/file)_ Add ExplicitBase discoverer; fix Explicit extension matching
- _(core)_ Support []string fields for config-path; LoadConfigPaths handles slices

### 🐛 Bug Fixes

- _(kong/show)_ Apply govet and perfsprint lint fixes

### 💼 Other

- _(release)_ Add release:push task; trim push section from release:tag
- _(release)_ Advance main bookmark to tagged commit before push
- _(release)_ Fix changelog version prefix

### ⚙️ Miscellaneous Tasks

- _(kong/charming)_ Update kong-charming to v0.2.0

## [v0.1.0] - 2026-04-28

### 🚀 Features

- Kongfig initial public release
  [v0.5.0]: https://github.com/pmarschik/kongfig/releases/tag/v0.5.0
  [v0.4.0]: https://github.com/pmarschik/kongfig/releases/tag/v0.4.0
  [v0.3.0]: https://github.com/pmarschik/kongfig/releases/tag/v0.3.0
  [v0.2.0]: https://github.com/pmarschik/kongfig/releases/tag/v0.2.0
  [v0.1.0]: https://github.com/pmarschik/kongfig/releases/tag/v0.1.0
