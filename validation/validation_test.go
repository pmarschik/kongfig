package validation_test

import (
	"context"
	"testing"

	"github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/validation"
)

// helpers

func mustNewWith(t *testing.T, reg *validation.Registry) *validation.Validator {
	t.Helper()
	v, err := validation.NewWith(reg)
	if err != nil {
		t.Fatalf("NewWith: %v", err)
	}
	return v
}

func mustNewFrom(t *testing.T, reg *validation.Registry) *validation.Validator {
	t.Helper()
	v, err := validation.NewFrom(reg)
	if err != nil {
		t.Fatalf("NewFrom: %v", err)
	}
	return v
}

type staticProvider struct {
	data   map[string]any
	source string
}

func (p *staticProvider) Load(_ context.Context) (kongfig.ConfigData, error) {
	return kongfig.ConfigData(p.data), nil
}

func (p *staticProvider) ProviderInfo() kongfig.ProviderInfo {
	return kongfig.ProviderInfo{Name: p.source}
}

func mustLoad(t *testing.T, k *kongfig.Kongfig, p *staticProvider) {
	t.Helper()
	if err := k.Load(context.Background(), p); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

// ── Diagnostics.Err ──────────────────────────────────────────────────────────

func TestDiagnostics_Err_Nil(t *testing.T) {
	var d *validation.Diagnostics
	if err := d.Err(); err != nil {
		t.Fatalf("nil Diagnostics.Err() = %v, want nil", err)
	}
}

func TestDiagnostics_Err_WarningOnly(t *testing.T) {
	d := &validation.Diagnostics{
		Violations: []validation.Violation{
			{Message: "advisory", Severity: validation.SeverityWarning},
		},
	}
	if err := d.Err(); err != nil {
		t.Fatalf("warning-only Err() = %v, want nil", err)
	}
}

func TestDiagnostics_Err_WithError(t *testing.T) {
	d := &validation.Diagnostics{
		Violations: []validation.Violation{
			{Paths: []validation.PathSource{{Path: "db.host"}}, Message: "required", Severity: validation.SeverityError},
		},
	}
	if err := d.Err(); err == nil {
		t.Fatal("expected non-nil error")
	}
}

// ── Field validators ─────────────────────────────────────────────────────────

func TestValidate_NoViolations(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"port": 8080}, source: "test"})

	v := validation.NewWithDefaults()
	v.AddValidator("port", func(_ validation.Event) []validation.FieldViolation {
		return nil
	})

	if d := v.Validate(k); d != nil {
		t.Fatalf("expected nil diagnostics, got %+v", d)
	}
}

func TestValidate_FieldViolation(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"port": -1}, source: "test"})

	v := validation.NewWithDefaults()
	v.AddValidator("port", func(e validation.Event) []validation.FieldViolation {
		if n, ok := e.Value.(int); ok && n < 1 {
			return []validation.FieldViolation{{Message: "must be positive", Code: "port.invalid"}}
		}
		return nil
	})

	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected diagnostics, got nil")
	}
	if len(d.Violations) != 1 {
		t.Fatalf("violations = %d, want 1", len(d.Violations))
	}
	got := d.Violations[0]
	if got.Code != "port.invalid" {
		t.Errorf("code = %q, want %q", got.Code, "port.invalid")
	}
	if len(got.Paths) != 1 || got.Paths[0].Path != "port" {
		t.Errorf("paths = %v, want [port]", got.Paths)
	}
	if err := d.Err(); err == nil {
		t.Fatal("Err() = nil for SeverityError violation")
	}
}

func TestValidate_MissingKeySkipped(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"other": "x"}, source: "test"})

	v := validation.NewWithDefaults()
	called := false
	v.AddValidator("port", func(_ validation.Event) []validation.FieldViolation {
		called = true
		return nil
	})

	v.Validate(k)
	if called {
		t.Fatal("validator should not be called when key is absent")
	}
}

func TestValidate_EventFields(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"host": "localhost"}, source: "test"})

	v := validation.NewWithDefaults()
	var gotEvent validation.Event
	v.AddValidator("host", func(e validation.Event) []validation.FieldViolation {
		gotEvent = e
		return nil
	})
	v.Validate(k)

	if gotEvent.Key != "host" {
		t.Errorf("event.Key = %q, want %q", gotEvent.Key, "host")
	}
	if gotEvent.Value != "localhost" {
		t.Errorf("event.Value = %v, want localhost", gotEvent.Value)
	}
}

func TestValidate_MultipleViolations(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{
		data:   map[string]any{"host": "", "port": -1},
		source: "test",
	})

	v := validation.NewWithDefaults()
	v.AddValidator("host", func(e validation.Event) []validation.FieldViolation {
		if s, ok := e.Value.(string); ok && s == "" {
			return []validation.FieldViolation{{Message: "required"}}
		}
		return nil
	})
	v.AddValidator("port", func(e validation.Event) []validation.FieldViolation {
		if n, ok := e.Value.(int); ok && n < 1 {
			return []validation.FieldViolation{{Message: "must be positive"}}
		}
		return nil
	})

	d := v.Validate(k)
	if d == nil || len(d.Violations) != 2 {
		t.Fatalf("violations = %d, want 2", len(d.Violations))
	}
}

// ── required annotation via AddSchema ───────────────────────────────────

type schemaConfig struct {
	Host string `kongfig:"host,validate=required"`
	Port int    `kongfig:"port"`
}

func TestAddSchema_Required_Missing(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"port": 8080}, source: "test"})

	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[schemaConfig]())

	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected diagnostics for missing required field")
	}
	if len(d.Violations) != 1 {
		t.Fatalf("violations = %d, want 1", len(d.Violations))
	}
	if d.Violations[0].Code != "kongfig.required" {
		t.Errorf("code = %q, want kongfig.required", d.Violations[0].Code)
	}
}

func TestAddSchema_Required_Present(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{
		data:   map[string]any{"host": "localhost", "port": 8080},
		source: "test",
	})

	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[schemaConfig]())

	if d := v.Validate(k); d != nil {
		t.Fatalf("unexpected violations: %+v", d)
	}
}

func TestAddSchema_WithAt(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{
		data:   map[string]any{"db": map[string]any{"port": 5432}},
		source: "test",
	})

	type dbConfig struct {
		Host string `kongfig:"host,validate=required"`
		Port int    `kongfig:"port"`
	}

	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[dbConfig](kongfig.At("db")))

	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected violation for db.host missing")
	}
	if len(d.Violations) == 0 || d.Violations[0].Paths[0].Path != "db.host" {
		t.Errorf("expected path db.host, got %v", d.Violations)
	}
}

// ── Rule ──────────────────────────────────────────────────────────────────────

// dbConnRule uses full dot-path tags — no At() prefix needed.
type dbConnRule struct {
	MinConns int `kongfig:"db.min_conns"`
	MaxConns int `kongfig:"db.max_conns"`
}

func TestRule_NoViolation(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{
		data:   map[string]any{"db": map[string]any{"min_conns": 1, "max_conns": 10}},
		source: "test",
	})

	v := validation.NewWithDefaults()
	v.AddRule(validation.Rule(
		func(c dbConnRule) []validation.FieldViolation {
			if c.MaxConns < c.MinConns {
				return []validation.FieldViolation{{Message: "max >= min required"}}
			}
			return nil
		},
	))

	if d := v.Validate(k); d != nil {
		t.Fatalf("unexpected violations: %+v", d)
	}
}

func TestRule_Violation(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{
		data:   map[string]any{"db": map[string]any{"min_conns": 10, "max_conns": 1}},
		source: "test",
	})

	v := validation.NewWithDefaults()
	v.AddRule(validation.Rule(
		func(c dbConnRule) []validation.FieldViolation {
			if c.MaxConns < c.MinConns {
				return []validation.FieldViolation{{Message: "max >= min required", Code: "db.conns.order"}}
			}
			return nil
		},
	))

	d := v.Validate(k)
	if d == nil || len(d.Violations) != 1 {
		t.Fatalf("violations = %d, want 1", len(d.Violations))
	}
	got := d.Violations[0]
	if got.Code != "db.conns.order" {
		t.Errorf("code = %q, want db.conns.order", got.Code)
	}
	// paths inferred from dbConnRule's full dot-path tags
	if len(got.Paths) != 2 {
		t.Errorf("paths = %v, want 2 paths", got.Paths)
	}
}

func TestRule_PathsFromTags(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{
		data:   map[string]any{"db": map[string]any{"min_conns": 1, "max_conns": 10}},
		source: "test",
	})

	v := validation.NewWithDefaults()
	v.AddRule(validation.Rule(
		func(_ dbConnRule) []validation.FieldViolation {
			return []validation.FieldViolation{{Message: "always"}}
		},
	))

	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected violation")
	}
	for _, p := range d.Violations[0].Paths {
		if p.Path != "db.min_conns" && p.Path != "db.max_conns" {
			t.Errorf("unexpected path %q", p.Path)
		}
	}
}

// ── ValidateOnLoad / NotifyOnLoad ─────────────────────────────────────────────

func TestValidateOnLoad_FiredOnLoad(t *testing.T) {
	k := kongfig.New()
	v := validation.NewWithDefaults(validation.WithValidateOnLoad(validation.SeverityError))
	v.AddValidator("port", func(e validation.Event) []validation.FieldViolation {
		if n, ok := e.Value.(int); ok && n < 1 {
			return []validation.FieldViolation{{Message: "must be positive"}}
		}
		return nil
	})
	v.Register(k)

	err := k.Load(context.Background(), &staticProvider{
		data:   map[string]any{"port": -1},
		source: "test",
	})
	if err == nil {
		t.Fatal("Load should return error for SeverityError violation with WithValidateOnLoad(SeverityError)")
	}
}

func TestValidateOnLoad_NotFiredWithoutOption(t *testing.T) {
	k := kongfig.New()
	v := validation.NewWithDefaults() // neither WithNotifyOnLoad nor WithValidateOnLoad
	v.AddValidator("port", func(e validation.Event) []validation.FieldViolation {
		if n, ok := e.Value.(int); ok && n < 1 {
			return []validation.FieldViolation{{Message: "must be positive"}}
		}
		return nil
	})
	v.Register(k)

	err := k.Load(context.Background(), &staticProvider{
		data:   map[string]any{"port": -1},
		source: "test",
	})
	if err != nil {
		t.Fatalf("Load should not return error without on-load option, got: %v", err)
	}
}

func TestWithValidateOnLoad_RejectsAtCutoff(t *testing.T) {
	k := kongfig.New()
	v := validation.NewWithDefaults(validation.WithValidateOnLoad(validation.SeverityError))
	v.AddValidator("port", func(e validation.Event) []validation.FieldViolation {
		if n, ok := e.Value.(int); ok && n < 1 {
			return []validation.FieldViolation{{Message: "must be positive"}}
		}
		return nil
	})
	v.Register(k)

	err := k.Load(context.Background(), &staticProvider{
		data:   map[string]any{"port": -1},
		source: "test",
	})
	if err == nil {
		t.Fatal("Load should return error when WithValidateOnLoad(SeverityError) is set")
	}
}

func TestValidateOnLoad_WarningDoesNotFail(t *testing.T) {
	k := kongfig.New()
	v := validation.NewWithDefaults(validation.WithValidateOnLoad(validation.SeverityError))
	v.AddValidator("port", func(_ validation.Event) []validation.FieldViolation {
		return []validation.FieldViolation{{Message: "advisory", Severity: validation.SeverityWarning}}
	})
	v.Register(k)

	err := k.Load(context.Background(), &staticProvider{
		data:   map[string]any{"port": 8080},
		source: "test",
	})
	if err != nil {
		t.Fatalf("warning-only violation should not fail Load with SeverityError cutoff, got: %v", err)
	}
}

func TestValidateOnLoad_MissingKeySkipped(t *testing.T) {
	k := kongfig.New()
	v := validation.NewWithDefaults(validation.WithValidateOnLoad(validation.SeverityError))
	v.AddValidator("port", func(_ validation.Event) []validation.FieldViolation {
		return []validation.FieldViolation{{Message: "always fail"}}
	})
	v.Register(k)

	// load data that does NOT contain "port"
	err := k.Load(context.Background(), &staticProvider{
		data:   map[string]any{"host": "localhost"},
		source: "test",
	})
	if err != nil {
		t.Fatalf("missing key should be skipped during per-load validation, got: %v", err)
	}
}

func TestValidateOnLoad_LayerPopulated(t *testing.T) {
	// Non-error (Warning) violations accumulate in LoadViolations and are surfaced
	// by Validate(). The load succeeds because warnings are below the SeverityError cutoff.
	k := kongfig.New()
	v := validation.NewWithDefaults(validation.WithValidateOnLoad(validation.SeverityError))
	v.AddValidator("port", func(_ validation.Event) []validation.FieldViolation {
		return []validation.FieldViolation{{Message: "advisory", Severity: validation.SeverityWarning}}
	})
	v.Register(k)

	err := k.Load(context.Background(), &staticProvider{
		data:   map[string]any{"port": 8080},
		source: "mysrc",
	})
	if err != nil {
		t.Fatalf("warning violation must not fail Load, got: %v", err)
	}

	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected diagnostics")
	}
	if len(d.LoadViolations) == 0 {
		t.Fatal("LoadViolations should be populated for committed loads with non-error violations")
	}
	if d.LoadViolations[0].Layer.Meta.Name != "mysrc" {
		t.Errorf("layer name = %q, want mysrc", d.LoadViolations[0].Layer.Meta.Name)
	}
}

func TestValidateOnLoad_RejectedLoadNotAccumulated(t *testing.T) {
	// An error-severity violation rejects the load (k.data unchanged)
	// and must NOT appear in LoadViolations — the caller already gets the error.
	k := kongfig.New()
	v := validation.NewWithDefaults(validation.WithValidateOnLoad(validation.SeverityError))
	v.AddValidator("port", func(e validation.Event) []validation.FieldViolation {
		if n, ok := e.Value.(int); ok && n < 1 {
			return []validation.FieldViolation{{Message: "bad port"}}
		}
		return nil
	})
	v.Register(k)

	err := k.Load(context.Background(), &staticProvider{
		data:   map[string]any{"port": -1},
		source: "mysrc",
	})
	if err == nil {
		t.Fatal("expected Load error for SeverityError violation")
	}

	// k.data must be unchanged (transactional reject).
	if k.Exists("port") {
		t.Fatal("rejected load must not commit data to k")
	}

	// Rejected load must not appear in LoadViolations.
	d := v.Validate(k)
	if d != nil {
		t.Fatalf("rejected load should not be accumulated in LoadViolations, got: %+v", d)
	}
}

func TestValidateOnLoad_StaleKeyNotReFired(t *testing.T) {
	// A bad value loaded by an earlier layer must not cause a later, unrelated
	// load to fail — on-load validators only fire for paths in the current layer.
	k := kongfig.New()
	// Load invalid port BEFORE the validator is registered (no hook yet).
	mustLoad(t, k, &staticProvider{data: map[string]any{"port": -1}, source: "defaults"})

	v := validation.NewWithDefaults(validation.WithValidateOnLoad(validation.SeverityError))
	v.AddValidator("port", func(e validation.Event) []validation.FieldViolation {
		if n, ok := e.Value.(int); ok && n < 1 {
			return []validation.FieldViolation{{Message: "bad port"}}
		}
		return nil
	})
	v.Register(k)

	// Load only "host" — "port" is not in this layer.
	err := k.Load(context.Background(), &staticProvider{
		data:   map[string]any{"host": "localhost"},
		source: "env",
	})
	if err != nil {
		t.Fatalf("load of unrelated key must not fail due to stale bad port, got: %v", err)
	}
}

func TestNotifyOnLoad_AccumulatesWithoutRejecting(t *testing.T) {
	// WithNotifyOnLoad accumulates all violations — including SeverityError — without
	// ever rejecting a Load. Errors surface only via the final Validate() call.
	k := kongfig.New()
	v := validation.NewWithDefaults(validation.WithNotifyOnLoad())
	v.AddValidator("port", func(e validation.Event) []validation.FieldViolation {
		if n, ok := e.Value.(int); ok && n < 1 {
			return []validation.FieldViolation{{Message: "bad port", Severity: validation.SeverityError}}
		}
		return nil
	})
	v.Register(k)

	err := k.Load(context.Background(), &staticProvider{
		data:   map[string]any{"port": -1},
		source: "test",
	})
	if err != nil {
		t.Fatalf("WithNotifyOnLoad must not reject Load even for SeverityError, got: %v", err)
	}

	d := v.Validate(k)
	if d == nil || len(d.LoadViolations) == 0 {
		t.Fatal("expected error violation in LoadViolations")
	}
	if d.LoadViolations[0].Violations[0].Severity != validation.SeverityError {
		t.Errorf("severity = %v, want SeverityError", d.LoadViolations[0].Violations[0].Severity)
	}
}

// ── Custom annotation ─────────────────────────────────────────────────────────

// nonemptyHandler is a reusable AnnotationFieldFunc for tests.
var nonemptyHandler = validation.AnnotationFieldFunc(func(e validation.AnnotationEvent) []validation.FieldViolation {
	if !e.Exists {
		return nil
	}
	if s, ok := e.Value.(string); ok && s == "" {
		return []validation.FieldViolation{{Message: e.Path + " must not be empty", Code: "nonempty", Severity: validation.SeverityError}}
	}
	return nil
})

func TestRegisterAnnotation_Custom(t *testing.T) {
	type cfg struct {
		Mode string `kongfig:"mode,validate=nonempty"`
	}

	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"mode": ""}, source: "test"})

	reg := validation.NewRegistryFromDefaults()
	reg.Register("nonempty", nonemptyHandler)
	v := mustNewWith(t, reg)
	v.AddSchema(validation.Schema[cfg]())

	d := v.Validate(k)
	if d == nil || len(d.Violations) == 0 {
		t.Fatal("expected nonempty violation")
	}
	if d.Violations[0].Code != "nonempty" {
		t.Errorf("code = %q, want nonempty", d.Violations[0].Code)
	}
}

func TestAddSchemaBeforeAnnotation(t *testing.T) {
	// AddSchema before registry.Register: annotation must still be applied
	// at Validate() time because the registry is a live reference (lazy resolution).
	type cfg struct {
		Mode string `kongfig:"mode,validate=nonempty"`
	}

	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"mode": ""}, source: "test"})

	reg := validation.NewEmptyRegistry()
	v := mustNewWith(t, reg)
	// Schema registered FIRST — annotation handler not yet in registry.
	v.AddSchema(validation.Schema[cfg]())
	// Handler registered AFTER schema but before Validate.
	reg.Register("nonempty", nonemptyHandler)

	d := v.Validate(k)
	if d == nil || len(d.Violations) == 0 {
		t.Fatal("expected nonempty violation even though schema was registered before annotation")
	}
	if d.Violations[0].Code != "nonempty" {
		t.Errorf("code = %q, want nonempty", d.Violations[0].Code)
	}
}

func TestUnknownAnnotation(t *testing.T) {
	// An annotation tag never registered must produce a SeverityError at Validate()
	// time, not silently disappear at AddSchema time.
	type cfg struct {
		Mode string `kongfig:"mode,validate=unknowntag"`
	}

	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"mode": "x"}, source: "test"})

	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[cfg]())

	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected diagnostics for unknown annotation")
	}
	found := false
	for _, viol := range d.Violations {
		if viol.Code == "kongfig.unknown_annotation" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected kongfig.unknown_annotation violation, got: %+v", d.Violations)
	}
	if err := d.Err(); err == nil {
		t.Fatal("unknown annotation must be SeverityError")
	}
}

func TestRegisterIdempotent(t *testing.T) {
	// Calling Register(k) twice must not install duplicate OnLoad hooks.
	// The validator function should be called exactly once per Load.
	k := kongfig.New()
	v := validation.NewWithDefaults(validation.WithValidateOnLoad(validation.SeverityError))
	callCount := 0
	v.AddValidator("port", func(_ validation.Event) []validation.FieldViolation {
		callCount++
		return nil
	})

	v.Register(k)
	v.Register(k) // second call must be a no-op

	mustLoad(t, k, &staticProvider{data: map[string]any{"port": 8080}, source: "test"})

	if callCount != 1 {
		t.Errorf("validator called %d times after double Register, want 1", callCount)
	}
}

// ── NewWith / NewFrom ─────────────────────────────────────────────────────────

func TestNewWith_LiveReference(t *testing.T) {
	// NewWith(reg) holds a live reference: annotations added to reg after NewWith
	// are visible on the next Validate call.
	type cfg struct {
		Mode string `kongfig:"mode,validate=livecheck"`
	}
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"mode": ""}, source: "test"})

	reg := validation.NewEmptyRegistry()
	v := mustNewWith(t, reg)
	v.AddSchema(validation.Schema[cfg]())

	// Before registering the handler: unknown_annotation error.
	d := v.Validate(k)
	if d == nil || d.Violations[0].Code != "kongfig.unknown_annotation" {
		t.Fatalf("expected unknown_annotation before handler registration, got %+v", d)
	}

	// Register handler on the live registry — validator sees it immediately.
	reg.Register("livecheck", validation.AnnotationFieldFunc(func(e validation.AnnotationEvent) []validation.FieldViolation {
		if e.Exists {
			if s, ok := e.Value.(string); ok && s == "" {
				return []validation.FieldViolation{{Message: "must not be empty", Code: "livecheck"}}
			}
		}
		return nil
	}))

	d = v.Validate(k)
	if d == nil || len(d.Violations) == 0 {
		t.Fatal("expected violation after registering handler on live registry")
	}
	if d.Violations[0].Code != "livecheck" {
		t.Errorf("code = %q, want livecheck", d.Violations[0].Code)
	}
}

func TestNewWith_Nil_ReturnsError(t *testing.T) {
	_, err := validation.NewWith(nil)
	if err == nil {
		t.Fatal("NewWith(nil) must return an error")
	}
}

func TestNewFrom_IsSnapshot(t *testing.T) {
	// NewFrom(reg) copies handlers at construction time.
	// Changes to reg after NewFrom are NOT visible to the validator.
	type cfg struct {
		Mode string `kongfig:"mode,validate=snapcheck"`
	}
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"mode": ""}, source: "test"})

	reg := validation.NewEmptyRegistry()
	v := mustNewFrom(t, reg)
	v.AddSchema(validation.Schema[cfg]())

	// Add handler to reg AFTER snapshot — must not affect v.
	reg.Register("snapcheck", validation.AnnotationFieldFunc(func(_ validation.AnnotationEvent) []validation.FieldViolation {
		return []validation.FieldViolation{{Message: "should not fire", Code: "snapcheck"}}
	}))

	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected diagnostics (unknown_annotation from snapshot)")
	}
	for _, viol := range d.Violations {
		if viol.Code == "snapcheck" {
			t.Fatal("NewFrom snapshot must not see handlers added to reg after construction")
		}
	}
	if d.Violations[0].Code != "kongfig.unknown_annotation" {
		t.Errorf("expected unknown_annotation, got %q", d.Violations[0].Code)
	}
}

func TestNewFrom_Nil_ReturnsError(t *testing.T) {
	_, err := validation.NewFrom(nil)
	if err == nil {
		t.Fatal("NewFrom(nil) must return an error")
	}
}

// ── Registry ─────────────────────────────────────────────────────────────────

func TestNewEmptyRegistry_NoBuiltins(t *testing.T) {
	// NewEmpty() has no handlers — even "required" is absent.
	type cfg struct {
		Host string `kongfig:"host,validate=required"`
	}
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{}, source: "test"})

	v := validation.NewEmpty()
	v.AddSchema(validation.Schema[cfg]())

	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected diagnostics")
	}
	// required is unknown in the empty registry → unknown_annotation error, not kongfig.required
	found := false
	for _, viol := range d.Violations {
		if viol.Code == "kongfig.unknown_annotation" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected kongfig.unknown_annotation for empty registry, got: %+v", d.Violations)
	}
}

func TestNewRegistryFromDefaults_HasRequired(t *testing.T) {
	// NewRegistryFromDefaults includes the built-in "required" handler.
	type cfg struct {
		Host string `kongfig:"host,validate=required"`
	}
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{}, source: "test"})

	reg := validation.NewRegistryFromDefaults()
	v := mustNewWith(t, reg)
	v.AddSchema(validation.Schema[cfg]())

	d := v.Validate(k)
	if d == nil || len(d.Violations) == 0 {
		t.Fatal("expected required violation")
	}
	if d.Violations[0].Code != "kongfig.required" {
		t.Errorf("code = %q, want kongfig.required", d.Violations[0].Code)
	}
}

func TestNewFromDefaults_HasRequired(t *testing.T) {
	// NewFromDefaults() snapshots DefaultRegistry — built-ins are present.
	type cfg struct {
		Host string `kongfig:"host,validate=required"`
	}
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{}, source: "test"})

	v := validation.NewFromDefaults()
	v.AddSchema(validation.Schema[cfg]())

	d := v.Validate(k)
	if d == nil || len(d.Violations) == 0 {
		t.Fatal("expected required violation")
	}
	if d.Violations[0].Code != "kongfig.required" {
		t.Errorf("code = %q, want kongfig.required", d.Violations[0].Code)
	}
}

func TestNewFromDefaults_IsSnapshot(t *testing.T) {
	// NewFromDefaults() takes a snapshot: annotations registered on DefaultRegistry
	// after construction are NOT visible to the validator.
	type cfg struct {
		Tag string `kongfig:"tag,validate=fromsnapcheck"`
	}
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"tag": ""}, source: "test"})

	v := validation.NewFromDefaults()
	v.AddSchema(validation.Schema[cfg]())

	const snapTag = "fromsnapcheck"
	validation.RegisterAnnotation(snapTag, validation.AnnotationFieldFunc(func(_ validation.AnnotationEvent) []validation.FieldViolation {
		return []validation.FieldViolation{{Message: "should not fire", Code: snapTag}}
	}))
	t.Cleanup(func() {
		validation.RegisterAnnotation(snapTag, validation.AnnotationFieldFunc(func(_ validation.AnnotationEvent) []validation.FieldViolation {
			return nil
		}))
	})

	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected diagnostics (unknown_annotation from snapshot)")
	}
	for _, viol := range d.Violations {
		if viol.Code == snapTag {
			t.Fatal("NewFromDefaults snapshot must not see annotations added to DefaultRegistry after construction")
		}
	}
}

func TestDefaultRegistry_LiveReference(t *testing.T) {
	// Validators created with New() hold a live reference to DefaultRegistry.
	// An annotation registered on DefaultRegistry after New() is visible at Validate() time.
	type cfg struct {
		Tag string `kongfig:"tag,validate=testlive"`
	}
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"tag": ""}, source: "test"})

	// Register annotation on DefaultRegistry BEFORE New() (clean baseline).
	// Use a unique name to avoid polluting DefaultRegistry for other tests.
	const testTag = "testlive_unique_" // must not already be registered
	v := validation.NewWithDefaults()  // holds live reference to DefaultRegistry
	v.AddSchema(validation.Schema[cfg]())

	type cfgLive struct {
		Tag string `kongfig:"tag,validate=testlive_unique_"`
	}
	vLive := validation.NewWithDefaults()
	vLive.AddSchema(validation.Schema[cfgLive]())

	// At this point the annotation is unregistered — should produce unknown_annotation.
	d := vLive.Validate(k)
	if d == nil {
		t.Fatal("expected unknown_annotation before registration")
	}

	// Register on DefaultRegistry after creating the validator.
	validation.RegisterAnnotation(testTag, validation.AnnotationFieldFunc(func(e validation.AnnotationEvent) []validation.FieldViolation {
		if e.Exists {
			if s, ok := e.Value.(string); ok && s == "" {
				return []validation.FieldViolation{{Message: "must not be empty", Code: testTag}}
			}
		}
		return nil
	}))
	t.Cleanup(func() {
		// Re-register a no-op to avoid polluting other tests relying on DefaultRegistry.
		validation.RegisterAnnotation(testTag, validation.AnnotationFieldFunc(func(_ validation.AnnotationEvent) []validation.FieldViolation {
			return nil
		}))
	})

	// Now the annotation resolves — validator created before registration sees it.
	d = vLive.Validate(k)
	if d == nil || len(d.Violations) == 0 {
		t.Fatal("expected violation after annotation registered on DefaultRegistry")
	}
	if d.Violations[0].Code != testTag {
		t.Errorf("code = %q, want %q", d.Violations[0].Code, testTag)
	}
	_ = v // suppress unused warning
}

func TestNewEmpty_IsolatesFromDefault(t *testing.T) {
	// NewEmpty() has no handlers — does not fall back to DefaultRegistry.
	type cfg struct {
		Host string `kongfig:"host,validate=required"`
	}
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{}, source: "test"})

	v := validation.NewEmpty()
	v.AddSchema(validation.Schema[cfg]())

	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected diagnostics")
	}
	for _, viol := range d.Violations {
		if viol.Code == "kongfig.required" {
			t.Fatal("NewEmpty must not fall back to DefaultRegistry")
		}
	}
}

// ── AnnotationFieldFunc ───────────────────────────────────────────────────────

func TestAnnotationFieldFunc_ExistsFalse(t *testing.T) {
	// When the field is absent, Exists=false is passed to the handler.
	type cfg struct {
		Host string `kongfig:"host,validate=checkexists"`
	}
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{}, source: "test"}) // host absent

	var gotEvent validation.AnnotationEvent
	reg := validation.NewEmptyRegistry()
	reg.Register("checkexists", validation.AnnotationFieldFunc(func(e validation.AnnotationEvent) []validation.FieldViolation {
		gotEvent = e
		return nil
	}))
	v := mustNewWith(t, reg)
	v.AddSchema(validation.Schema[cfg]())
	v.Validate(k)

	if gotEvent.Exists {
		t.Error("expected Exists=false for absent key")
	}
	if gotEvent.Path != "host" {
		t.Errorf("Path = %q, want host", gotEvent.Path)
	}
}

func TestAnnotationFieldFunc_PathsAutoSet(t *testing.T) {
	// Violations from AnnotationFieldFunc have Paths set to the field path automatically.
	type cfg struct {
		Port int `kongfig:"port,validate=checkport"`
	}
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"port": -1}, source: "test"})

	reg := validation.NewEmptyRegistry()
	reg.Register("checkport", validation.AnnotationFieldFunc(func(e validation.AnnotationEvent) []validation.FieldViolation {
		if n, ok := e.Value.(int); ok && n < 0 {
			return []validation.FieldViolation{{Message: "negative", Code: "port.neg"}}
		}
		return nil
	}))
	v := mustNewWith(t, reg)
	v.AddSchema(validation.Schema[cfg]())

	d := v.Validate(k)
	if d == nil || len(d.Violations) == 0 {
		t.Fatal("expected violation")
	}
	got := d.Violations[0]
	if len(got.Paths) != 1 || got.Paths[0].Path != "port" {
		t.Errorf("Paths = %v, want [port]", got.Paths)
	}
}

// ── Param helpers ─────────────────────────────────────────────────────────────

func TestParseParamInt(t *testing.T) {
	cases := []struct {
		in    string
		want  int64
		valid bool
	}{
		{"42", 42, true},
		{"-7", -7, true},
		{"0", 0, true},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, c := range cases {
		n, ok := validation.ParseParamInt(c.in)
		if ok != c.valid || (ok && n != c.want) {
			t.Errorf("ParseParamInt(%q) = (%d, %v), want (%d, %v)", c.in, n, ok, c.want, c.valid)
		}
	}
}

func TestParseParamBool(t *testing.T) {
	cases := []struct {
		in    string
		want  bool
		valid bool
	}{
		{"true", true, true},
		{"TRUE", true, true},
		{"1", true, true},
		{"yes", true, true},
		{"false", false, true},
		{"0", false, true},
		{"no", false, true},
		{"", false, false},
		{"maybe", false, false},
	}
	for _, c := range cases {
		b, ok := validation.ParseParamBool(c.in)
		if ok != c.valid || (ok && b != c.want) {
			t.Errorf("ParseParamBool(%q) = (%v, %v), want (%v, %v)", c.in, b, ok, c.want, c.valid)
		}
	}
}

func TestParseParamList(t *testing.T) {
	if got := validation.ParseParamList(""); got != nil {
		t.Errorf("empty param: got %v, want nil", got)
	}
	got := validation.ParseParamList("a|b|c")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("ParseParamList(\"a|b|c\") = %v, want [a b c]", got)
	}
	single := validation.ParseParamList("only")
	if len(single) != 1 || single[0] != "only" {
		t.Errorf("ParseParamList(\"only\") = %v, want [only]", single)
	}
}

// ── Err propagation ───────────────────────────────────────────────────────────

func TestDiagnostics_Err_Message(t *testing.T) {
	d := &validation.Diagnostics{
		Violations: []validation.Violation{
			{Paths: []validation.PathSource{{Path: "db.host"}}, Message: "required", Code: "kongfig.required", Severity: validation.SeverityError},
		},
	}
	err := d.Err()
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if msg := err.Error(); msg == "" {
		t.Fatal("error message should not be empty")
	}
}

func TestDiagnostics_Err_IsPlainError(t *testing.T) {
	// Err() returns a plain error; callers retain the *Diagnostics for structured access.
	d := &validation.Diagnostics{
		Violations: []validation.Violation{
			{Message: "bad", Severity: validation.SeverityError},
		},
	}
	err := d.Err()
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	// The error message contains the violation message.
	if msg := err.Error(); msg == "" {
		t.Fatal("error message must not be empty")
	}
}

// ── ForEach ───────────────────────────────────────────────────────────────────

type dbElem struct {
	Host string `kongfig:"host,validate=required"`
	Port int    `kongfig:"port"`
}

func TestForEach_Slice_AllPresent(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{
		source: "test",
		data: map[string]any{
			"dbs": []any{
				map[string]any{"host": "a", "port": 5432},
				map[string]any{"host": "b", "port": 5433},
			},
		},
	})

	v := validation.NewWithDefaults()
	v.AddSchema(validation.ForEach[dbElem]("dbs"))

	if d := v.Validate(k); d != nil {
		t.Fatalf("unexpected violations: %+v", d.Violations)
	}
}

func TestForEach_Slice_MissingRequired(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{
		source: "test",
		data: map[string]any{
			"dbs": []any{
				map[string]any{"host": "a"},
				map[string]any{"port": 5433}, // host missing
			},
		},
	})

	v := validation.NewWithDefaults()
	v.AddSchema(validation.ForEach[dbElem]("dbs"))

	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected violation for missing host in dbs.1")
	}
	var found bool
	for _, viol := range d.Violations {
		for _, ps := range viol.Paths {
			if ps.Path == "dbs[1].host" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected violation at dbs[1].host, got %+v", d.Violations)
	}
}

func TestForEach_Map_AllPresent(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{
		source: "test",
		data: map[string]any{
			"conns": map[string]any{
				"primary": map[string]any{"host": "db1", "port": 5432},
				"replica": map[string]any{"host": "db2", "port": 5433},
			},
		},
	})

	v := validation.NewWithDefaults()
	v.AddSchema(validation.ForEach[dbElem]("conns"))

	if d := v.Validate(k); d != nil {
		t.Fatalf("unexpected violations: %+v", d.Violations)
	}
}

func TestForEach_Map_MissingRequired(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{
		source: "test",
		data: map[string]any{
			"conns": map[string]any{
				"primary": map[string]any{"host": "db1"},
				"replica": map[string]any{"port": 5433}, // host missing
			},
		},
	})

	v := validation.NewWithDefaults()
	v.AddSchema(validation.ForEach[dbElem]("conns"))

	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected violation for missing host in conns.replica")
	}
	var found bool
	for _, viol := range d.Violations {
		for _, ps := range viol.Paths {
			if ps.Path == "conns[replica].host" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected violation at conns[replica].host, got %+v", d.Violations)
	}
}

func TestForEach_EmptyCollection(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{
		source: "test",
		data:   map[string]any{"dbs": []any{}},
	})

	v := validation.NewWithDefaults()
	v.AddSchema(validation.ForEach[dbElem]("dbs"))

	if d := v.Validate(k); d != nil {
		t.Fatalf("unexpected violations on empty slice: %+v", d.Violations)
	}
}

func TestForEach_MissingPrefix(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{source: "test", data: map[string]any{}})

	v := validation.NewWithDefaults()
	v.AddSchema(validation.ForEach[dbElem]("dbs"))

	// prefix not in config — no violations (treat as optional collection)
	if d := v.Validate(k); d != nil {
		t.Fatalf("unexpected violations when prefix absent: %+v", d.Violations)
	}
}

// ── getNestedValue with []any ─────────────────────────────────────────────────

func TestGetNestedValue_SliceIndex(t *testing.T) {
	// Indirectly tested via ForEach; also verify path dbs.0.host works in AddValidator.
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{
		source: "test",
		data: map[string]any{
			"dbs": []any{
				map[string]any{"host": "first"},
			},
		},
	})

	v := validation.NewWithDefaults()
	var captured any
	v.AddValidator("dbs[0].host", func(e validation.Event) []validation.FieldViolation {
		captured = e.Value
		return nil
	})
	v.Validate(k)

	if captured != "first" {
		t.Errorf("dbs.0.host value = %v, want \"first\"", captured)
	}
}
