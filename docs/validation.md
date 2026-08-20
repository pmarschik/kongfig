# Validation

The `kongfig/validation` package is an optional layer on top of the core `kongfig` package.
It provides per-key validators, cross-key validators, and schema annotation validators
with configurable per-load or post-load execution.

## Concepts

| Concept              | What it does                                                                          |
| -------------------- | ------------------------------------------------------------------------------------- |
| `Validator`          | Central registry that accumulates validator definitions and wires into `*kongfig.Kongfig` |
| `AddValidator`       | Per-key validation: fires for one dot-path                                            |
| `Rule[T]`            | Cross-key validation: decodes a typed projection struct and validates relationships   |
| `Schema[T]`          | Annotation-driven validation: reads `kongfig` struct tags to install validators       |
| `RegisterAnnotation` | Extend the annotation system with custom tag options                                  |
| `Severity`           | `Error` / `Warning` / `Info` / `Hint` — only `Error` causes `Err()` to return non-nil |
| `Diagnostics`        | Bag of violations from `Validate()`. Call `.Err()` for a plain `error`                |

---

## Quick start

```go
v := validation.NewWithDefaults()

// Per-key validator
v.AddValidator("port", func(e validation.Event) []validation.FieldViolation {
    if n, ok := e.Value.(int); ok && (n < 1 || n > 65535) {
        return []validation.FieldViolation{{Message: "port must be 1–65535", Code: "port.range"}}
    }
    return nil
})

// Cross-key rule — projection struct tags are full dot-paths from root
type DBConnRule struct {
    MinConns int `kongfig:"db.min_conns"`
    MaxConns int `kongfig:"db.max_conns"`
}
v.AddRule(validation.Rule(
    func(c DBConnRule) []validation.FieldViolation {
        if c.MaxConns < c.MinConns {
            return []validation.FieldViolation{{Message: "max_conns must be >= min_conns"}}
        }
        return nil
    },
))

// Run after all layers are loaded
d := v.Validate(k)
if err := d.Err(); err != nil {
    log.Fatal(err)
}
```

---

## Per-key validators

```go
v.AddValidator(path string, fn validation.Func)
```

- `fn` receives `Event{Key, Value}` and returns `[]FieldViolation`.
- Kongfig skips the validator silently when the merged config has no such key.
- Several validators can share the same path. All of them run.

To fire all validators on every `Load()`, pass `WithValidateOnLoad` at construction time
(see [ValidateOnLoad — global switch](#validateonload--global-switch) below).

---

## Cross-key rules

```go
r := validation.Rule(fn func(T) []validation.FieldViolation) RuleValidator
v.AddRule(r)
```

`Rule[T]` decodes the Kongfig into `T` via `kongfig.GetByPaths[T]` and calls `fn`.
Each field in `T` must use its `kongfig` tag as a **full dot-delimited path from the
config root**. `Rule[T]` accepts no `At()` prefix, and needs none:

```go
type DBConnRule struct {
    MinConns int `kongfig:"db.min_conns"` // absolute path from root
    MaxConns int `kongfig:"db.max_conns"`
}
```

Kongfig infers the violation paths from the leaf tags of `T`.

`RuleValidator` is opaque — construct it only with `Rule`.

### Composite rule helpers

The `validation` package ships helpers for common multi-field constraints. Pass field
pointers inside the decoded struct `T`. Kongfig derives each path from the `kongfig` tag of
the field:

| Helper                              | Constraint                                        |
| ----------------------------------- | ------------------------------------------------- |
| `ExactlyOneOf(&c, &c.A, &c.B)`      | Exactly one field must be non-zero                |
| `AtLeastOneOf(&c, &c.A, &c.B)`      | One or more fields must be non-zero               |
| `MutuallyExclusive(&c, &c.A, &c.B)` | At most one field can be non-zero                 |
| `AllOrNone(&c, &c.A, &c.B)`         | Either all are non-zero, or none are              |
| `RequiredWith(&c, &c.F, &c.Trig)`   | `F` required when any trigger field is non-zero   |
| `RequiredWithout(&c, &c.F, &c.FB)`  | `F` required when none of the fallback fields are |

```go
type TLSConfig struct {
    CertFile string `kongfig:"tls.cert-file"`
    KeyFile  string `kongfig:"tls.key-file"`
}

v.AddRule(validation.Rule(func(c TLSConfig) []validation.FieldViolation {
    return validation.AllOrNone(&c, &c.CertFile, &c.KeyFile)
}))
```

If a pointer does not match a field in `*T`, every helper panics (programming error).

---

## Registry

A `Registry` holds the annotation handlers. Choose the right constructor:

| Constructor                 | Semantics                                                                     |
| --------------------------- | ----------------------------------------------------------------------------- |
| `DefaultRegistry()`         | The package-level shared registry. `NewWithDefaults()` holds a live reference |
| `NewRegistryFromDefaults()` | Independent copy seeded from current `DefaultRegistry()` handlers             |
| `NewEmptyRegistry()`        | Blank — no handlers, not even `"required"`                                    |

### Validator constructors

| Constructor         | Registry                      | Returns               | nil arg |
| ------------------- | ----------------------------- | --------------------- | ------- |
| `NewWithDefaults()` | live → `DefaultRegistry`      | `*Validator`          | n/a     |
| `NewFromDefaults()` | snapshot of `DefaultRegistry` | `*Validator`          | n/a     |
| `NewWith(reg)`      | live → `reg`                  | `(*Validator, error)` | error   |
| `NewFrom(reg)`      | snapshot copy of `reg`        | `(*Validator, error)` | error   |
| `NewEmpty()`        | none                          | `*Validator`          | n/a     |

**Live** means that an annotation added to the registry after construction is visible on
the next `Validate()` call. **Snapshot** means that the constructor copies the registry. A
later change to the source registry is then invisible.

```go
// Simple apps: register globally once, all NewWithDefaults()-based validators see it.
validation.RegisterAnnotation("nonempty", myHandler)

// Isolated registry — changes to DefaultRegistry after construction are not visible.
reg := validation.NewRegistryFromDefaults()
reg.Register("nonempty", myHandler)
v, err := validation.NewWith(reg)

// Live reference to a custom registry — adds after NewWith are visible.
reg := validation.NewEmptyRegistry()
v, err := validation.NewWith(reg)
reg.Register("nonempty", myHandler) // still visible to v
```

---

## Schema annotation validators

`Schema[T]` reflects on the `kongfig` struct tags of `T` and extracts the annotation
options. An annotation option is anything after the first comma that is not a structural
option like `squash` or `redacted`. `AddSchema` stores the schema for lazy resolution at
`Validate()` time. The call order of `AddSchema` and `Registry.Register` does not matter.

```go
type Config struct {
    Host string `kongfig:"host,required"`
    Port int    `kongfig:"port"`
}

v := validation.NewWithDefaults()             // uses DefaultRegistry live; "required" pre-wired
v.AddSchema(validation.Schema[Config]()) // stored for lazy resolution
```

`SchemaValidator` is opaque — construct it only with `Schema`.

### At() prefix

```go
v.AddSchema(validation.Schema[DBConfig](kongfig.At("db")))
```

This option prepends `"db."` to every extracted path.

### Built-in annotations

| Annotation | Effect                                                      |
| ---------- | ----------------------------------------------------------- |
| `required` | Missing-key check for the field. `Code: "kongfig.required"` |

### Custom annotations

Use `AnnotationFieldFunc` for the common field-scoped case. Kongfig then resolves the
field value and sets `Paths` on the violations:

```go
reg := validation.NewRegistryFromDefaults()
reg.Register("nonempty", validation.AnnotationFieldFunc(
    func(e validation.AnnotationEvent) []validation.FieldViolation {
        if !e.Exists {
            return nil // absent key; "required" handles that separately
        }
        if s, ok := e.Value.(string); ok && s == "" {
            return []validation.FieldViolation{{
                Message:  e.Path + " must not be empty",
                Code:     "nonempty",
                Severity: validation.SeverityError,
            }}
        }
        return nil
    },
))
v, err := validation.NewWith(reg)
if err != nil {
    log.Fatal(err)
}
v.AddSchema(validation.Schema[Config]())
```

`AnnotationEvent` fields:

| Field    | Type       | Meaning                                                        |
| -------- | ---------- | -------------------------------------------------------------- |
| `Path`   | `string`   | Full dot-delimited config key                                  |
| `Args`   | `[]string` | Expression arguments. A zero-arg atom such as `required` gives nil |
| `Value`  | `any`      | Current value at `Path`. Valid only when `Exists` is true      |
| `Exists` | `bool`     | Whether `Path` is present in the merged config                 |

`Args` reflects the validate= expression: `required` → `nil`, `min(1)` → `["1"]`,
`oneof(a b c)` → `["a", "b", "c"]`.

For cross-field constraints use `Rule` instead of annotation handlers.

### Annotation param helpers

Use these inside `AnnotationFieldFunc` to parse `e.Args`:

```go
n, ok  := validation.ParseParamInt(e.Args[0])  // parse first arg as integer
b, ok  := validation.ParseParamBool(e.Args[0]) // parse first arg as boolean
items  := validation.ParseParamList(e.Args[0]) // split pipe-separated list
```

An unknown annotation tag surfaces as `SeverityError{Code: "kongfig.unknown_annotation"}`
at the `Validate()` call. `AddSchema` does not drop it silently.

---

## OnLoad transactional semantics

`OnLoad` hooks run on the **proposed** merged state before kongfig commits it. If any hook returns a non-nil error, kongfig rejects the load. `k.data`, `k.prov`, and `k.layers` then stay unchanged. Inside a hook, `k.Raw()` returns the pre-load state. Use `e.ProposedData` to read the post-merge view.

A per-load field validator fires only for the keys in the current layer data (`e.Layer.Data`), and not for every key in the merged config. A stale bad value from an earlier layer therefore cannot make a later unrelated load fail. A `SeverityError` violation rejects the load entirely. A lower-severity violation accumulates in `Diagnostics.LoadViolations`, and `Validate()` surfaces it.

## ValidateOnLoad — global switch

`WithValidateOnLoad(at Severity)` makes **all** field validators fire on every `Load()`. A
violation at severity `at` or higher makes `Load()` return an error immediately. A
lower-severity violation accumulates in `Diagnostics.LoadViolations`.

```go
v := validation.NewWithDefaults(validation.WithValidateOnLoad(validation.SeverityError))
```

This option fits a strict environment, where an invalid load must fail fast.

---

## Wiring into Kongfig

```go
v.Register(k)
```

`Register` installs an `OnLoad` hook on `k`. The hook fires only when
`WithValidateOnLoad` was set at construction time. A second `Register` call on the same `k`
is a no-op, because kongfig installs the hook exactly once. Call `Register` after you add
all the validators, and before any `Load` call.

---

## Validate — post-load check

```go
d := v.Validate(k)
```

`Validate` runs all the validators (per-key, cross-key, required checks) against the
current merged state of `k`. It also drains the per-load violations that accumulated since
the last `Validate` call, and attaches them as `Diagnostics.LoadViolations`.

It returns `nil` when no violation of any severity exists.

---

## Diagnostics

```go
type Diagnostics struct {
    Violations     []Violation      // from Validate()
    LoadViolations []LayerViolation // accumulated from per-load hook firings
}

func (d *Diagnostics) Err() error
```

- `Err()` returns a non-nil error only when at least one `SeverityError` violation exists.
- `Err()` is nil-safe: `(*Diagnostics)(nil).Err()` returns `nil`.
- `LoadViolations` links each violation set to the `kongfig.Layer` that triggered it.

### Source positions

Every `Violation` carries `Paths []PathSource`, and each `PathSource` pairs the config path
with the layer that last wrote it. `PathSource.Position()` returns the place of that path in
its source document. It returns `nil` when the position is unavailable. Three causes exist:
no provenance, a source with no document (env, flags, defaults), or a parser that does not
report positions ([architecture.md](architecture.md#source-positions)).

```go
for _, v := range diags.Violations {
    for _, ps := range v.Paths {
        msg := ps.Path + ": " + v.Message
        if pos := ps.Position(); pos != nil {
            msg += " (" + pos.String() + ")" // db.host: required (/etc/app/config.yaml:42:8)
        }
        fmt.Println(msg)
    }
}
```

A position points at the **value**, because that is where a rejected value sits.
`Diagnostics.Err()` does not include the positions, and its message format is unchanged.
Format your own message when you want the file, the line and the column.

### Severity

| Level             | `Err()` non-nil | Meaning                          |
| ----------------- | --------------- | -------------------------------- |
| `SeverityError`   | yes             | Config is unusable               |
| `SeverityWarning` | no              | You must correct this, but the application can continue |
| `SeverityInfo`    | no              | Informational only               |
| `SeverityHint`    | no              | Optional improvement suggestion  |

---

## Execution order

```
k.Load()  (each call)
  └─ OnLoad hook (if Register was called and WithValidateOnLoad was set)
       └─ per-key field validators (all registered paths)
            → violations at or above cutoff severity cause Load() to fail
            → lower-severity violations accumulate in Validator

v.Validate(k)
  ├─ per-key field validators (all, against merged state)
  ├─ schema annotation validators (lazy: resolved against registry handlers;
  │    unknown tags → SeverityError)
  ├─ cross-key rules (Rule[T] via GetByPaths[T])
  └─ drain accumulated LoadViolations
       → returns *Diagnostics (nil if no violations)
```
