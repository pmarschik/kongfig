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
  [v0.3.0]: https://github.com/pmarschik/kongfig/releases/tag/v0.3.0
  [v0.2.0]: https://github.com/pmarschik/kongfig/releases/tag/v0.2.0
  [v0.1.0]: https://github.com/pmarschik/kongfig/releases/tag/v0.1.0
