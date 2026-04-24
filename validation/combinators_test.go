package validation_test

// combinators_test.go covers the built-in expression combinators (each, keys, any, all),
// edge cases (invalid regex, non-collection each, WithValidateOnLoad severity cutoff),
// and Rule[T] with pointer fields.

import (
	"context"
	"testing"

	"github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/validation"
)

// ── each combinator ───────────────────────────────────────────────────────────

// eachIntSlice uses each(min(1)) on an []any field backed by int values.
type eachIntSlice struct {
	Nums any `kongfig:"nums,validate=each(min(1))"`
}

func TestEach_AllElementsPass(t *testing.T) {
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"nums": []any{2, 3, 10}},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[eachIntSlice]())
	assertClean(t, v.Validate(k))
}

func TestEach_OneElementFails(t *testing.T) {
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"nums": []any{5, 0, 3}}, // 0 < min(1)
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[eachIntSlice]())
	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected violation for element below min(1)")
	}
	found := false
	for _, viol := range d.Violations {
		if viol.Code == "kongfig.min" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected kongfig.min violation; got: %+v", d.Violations)
	}
}

func TestEach_EmptySlice(t *testing.T) {
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"nums": []any{}},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[eachIntSlice]())
	// Empty slice: each has nothing to iterate — no violations.
	assertClean(t, v.Validate(k))
}

// TestEach_NonCollectionIsNoOp applies each(required) to a plain string field.
// The combinator should silently do nothing (it only iterates []any / ConfigData).
func TestEach_NonCollectionIsNoOp(t *testing.T) {
	type cfg struct {
		Host string `kongfig:"host,validate=each(required)"`
	}
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"host": "localhost"},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[cfg]())
	// A string is not a slice/map; each is a graceful no-op.
	assertClean(t, v.Validate(k))
}

// ── keys combinator ───────────────────────────────────────────────────────────

// keysHostname uses keys(hostname) on a map[string]any field so that map keys
// must be valid RFC 1123 hostnames.
type keysHostname struct {
	Backends any `kongfig:"backends,validate=keys(hostname)"`
}

func TestKeys_AllValidHostnames(t *testing.T) {
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data: map[string]any{"backends": map[string]any{
			"db.internal":  "primary",
			"db2.internal": "replica",
		}},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[keysHostname]())
	assertClean(t, v.Validate(k))
}

func TestKeys_OneInvalidHostname(t *testing.T) {
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data: map[string]any{"backends": map[string]any{
			"valid.host":  "ok",
			"not a host!": "bad", // spaces and ! are not valid hostname chars
		}},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[keysHostname]())
	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected hostname violation for invalid map key")
	}
	found := false
	for _, viol := range d.Violations {
		if viol.Code == "kongfig.hostname" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected kongfig.hostname violation; got: %+v", d.Violations)
	}
}

func TestKeys_EmptyMap(t *testing.T) {
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"backends": map[string]any{}},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[keysHostname]())
	// Empty map: keys has nothing to iterate — no violations.
	assertClean(t, v.Validate(k))
}

// ── any combinator ────────────────────────────────────────────────────────────

func TestAny_FailureMessage(t *testing.T) {
	// "not-either" is neither a valid hostname nor a valid IP address.
	type cfg struct {
		Addr string `kongfig:"addr,validate=any(hostname ip)"`
	}
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"addr": "not-either!@#"},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[cfg]())
	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected violation when all any() branches fail")
	}
	found := false
	for _, viol := range d.Violations {
		if viol.Code == "kongfig.any" {
			found = true
			if viol.Message == "" {
				t.Errorf("any violation message must not be empty")
			}
		}
	}
	if !found {
		t.Fatalf("expected kongfig.any violation; got: %+v", d.Violations)
	}
}

func TestAny_FirstBranchPasses(t *testing.T) {
	type cfg struct {
		Addr string `kongfig:"addr,validate=any(hostname ip)"`
	}
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"addr": "my.host.name"},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[cfg]())
	assertClean(t, v.Validate(k))
}

func TestAny_SecondBranchPasses(t *testing.T) {
	type cfg struct {
		Addr string `kongfig:"addr,validate=any(hostname ip)"`
	}
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"addr": "192.168.1.1"},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[cfg]())
	assertClean(t, v.Validate(k))
}

// ── all combinator (nested argument passing) ──────────────────────────────────

func TestAll_ValueInRange(t *testing.T) {
	type cfg struct {
		Level int `kongfig:"level,validate=all(min(10) max(100))"`
	}
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"level": 50},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[cfg]())
	assertClean(t, v.Validate(k))
}

func TestAll_ValueBelowMin(t *testing.T) {
	type cfg struct {
		Level int `kongfig:"level,validate=all(min(10) max(100))"`
	}
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"level": 5},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[cfg]())
	assertViolation(t, v.Validate(k), "kongfig.min")
}

func TestAll_ValueAboveMax(t *testing.T) {
	type cfg struct {
		Level int `kongfig:"level,validate=all(min(10) max(100))"`
	}
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"level": 200},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[cfg]())
	assertViolation(t, v.Validate(k), "kongfig.max")
}

// ── invalid regex in pattern ──────────────────────────────────────────────────

// TestPattern_InvalidRegex verifies that an invalid regex in pattern() returns
// a kongfig.pattern violation instead of panicking.
// builtins.go:270-271: if err != nil || !matched → returns violation.
func TestPattern_InvalidRegex(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=pattern('[invalid')"`
	}
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"f": "anything"},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[cfg]())
	// Must not panic; must return a violation.
	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected violation for invalid regex pattern")
	}
	assertViolation(t, d, "kongfig.pattern")
}

// ── WithValidateOnLoad severity cutoff ────────────────────────────────────────

// TestWithValidateOnLoad_WarningCutoff_WarningsBlock verifies that
// WithValidateOnLoad(SeverityWarning) rejects loads that produce warning violations.
func TestWithValidateOnLoad_WarningCutoff_WarningsBlock(t *testing.T) {
	k := kongfig.New()
	v := validation.NewWithDefaults(validation.WithValidateOnLoad(validation.SeverityWarning))
	v.AddValidator("tag", func(_ validation.Event) []validation.FieldViolation {
		return []validation.FieldViolation{{Message: "advisory", Severity: validation.SeverityWarning}}
	})
	v.Register(k)

	err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"tag": "v1"},
	})
	if err == nil {
		t.Fatal("Load should return error when warning severity meets SeverityWarning cutoff")
	}
}

// TestWithValidateOnLoad_ErrorCutoff_WarningsPass verifies that
// WithValidateOnLoad(SeverityError) does not block loads that only have warnings.
func TestWithValidateOnLoad_ErrorCutoff_WarningsPass(t *testing.T) {
	k := kongfig.New()
	v := validation.NewWithDefaults(validation.WithValidateOnLoad(validation.SeverityError))
	v.AddValidator("tag", func(_ validation.Event) []validation.FieldViolation {
		return []validation.FieldViolation{{Message: "advisory", Severity: validation.SeverityWarning}}
	})
	v.Register(k)

	err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"tag": "v1"},
	})
	if err != nil {
		t.Fatalf("SeverityWarning should not block load with SeverityError cutoff; got: %v", err)
	}
}

// TestWithValidateOnLoad_WarningCutoff_ErrorsBlock verifies that
// WithValidateOnLoad(SeverityWarning) also rejects error-severity violations
// (errors are more severe than warnings, so they are >= the cutoff).
func TestWithValidateOnLoad_WarningCutoff_ErrorsBlock(t *testing.T) {
	k := kongfig.New()
	v := validation.NewWithDefaults(validation.WithValidateOnLoad(validation.SeverityWarning))
	v.AddValidator("port", func(e validation.Event) []validation.FieldViolation {
		if n, ok := e.Value.(int); ok && n < 1 {
			return []validation.FieldViolation{{Message: "bad port", Severity: validation.SeverityError}}
		}
		return nil
	})
	v.Register(k)

	err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"port": -1},
	})
	if err == nil {
		t.Fatal("Load should return error when error-severity violation meets SeverityWarning cutoff")
	}
}

// ── Rule[T] with pointer fields ───────────────────────────────────────────────

type ptrFieldCfg struct {
	Host *string `kongfig:"db.host"`
}

// TestRule_PointerField_NilPointer verifies that Rule[T] gracefully handles
// nil pointer fields — kongfig.Get decodes a missing key as a nil pointer,
// which should not cause a panic in the rule function.
func TestRule_PointerField_NilPointer(t *testing.T) {
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{}, // db.host absent
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddRule(validation.Rule(func(c ptrFieldCfg) []validation.FieldViolation {
		if c.Host == nil {
			return []validation.FieldViolation{{Message: "db.host required", Code: "test.ptr.nil"}}
		}
		return nil
	}))

	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected violation for nil pointer field")
	}
	assertViolation(t, d, "test.ptr.nil")
}

// TestRule_PointerField_NonNilPointer verifies that Rule[T] correctly handles
// a non-nil *string field — the path attribution must include db.host.
func TestRule_PointerField_NonNilPointer(t *testing.T) {
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"db": map[string]any{"host": "localhost"}},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddRule(validation.Rule(func(c ptrFieldCfg) []validation.FieldViolation {
		if c.Host == nil {
			return []validation.FieldViolation{{Message: "db.host required", Code: "test.ptr.nil"}}
		}
		return nil
	}))

	d := v.Validate(k)
	if d != nil {
		t.Fatalf("expected no violations for non-nil pointer field; got: %+v", d.Violations)
	}
}

// TestRule_PointerField_PathsFromTags verifies that extractLeafPaths correctly
// extracts "db.host" from a *string-typed field so that violations carry path info.
func TestRule_PointerField_PathsFromTags(t *testing.T) {
	k := kongfig.New()
	if err := k.Load(context.Background(), &staticProvider{
		source: "test",
		data:   map[string]any{"db": map[string]any{"host": "localhost"}},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddRule(validation.Rule(func(_ ptrFieldCfg) []validation.FieldViolation {
		return []validation.FieldViolation{{Message: "always", Code: "test.ptr.always"}}
	}))

	d := v.Validate(k)
	if d == nil {
		t.Fatal("expected violation")
	}
	found := false
	for _, viol := range d.Violations {
		for _, ps := range viol.Paths {
			if ps.Path == "db.host" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected violation path db.host from *string field tag; got: %+v", d.Violations)
	}
}
