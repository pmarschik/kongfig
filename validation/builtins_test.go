package validation_test

import (
	"os"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/validation"
)

// validateWith loads value at key "f", registers Schema[T], validates, returns diagnostics.
func validateWith[T any](t *testing.T, value any) *validation.Diagnostics {
	t.Helper()
	k := kongfig.New()
	data := map[string]any{}
	if value != nil {
		data["f"] = value
	}
	if err := k.Load(t.Context(), &staticProvider{data: data, source: "test"}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[T]())
	return v.Validate(k)
}

func assertViolation(t *testing.T, d *validation.Diagnostics, wantCode string) {
	t.Helper()
	if d == nil {
		t.Fatalf("expected violation %q, got nil diagnostics", wantCode)
	}
	for _, viol := range d.Violations {
		if viol.Code == wantCode {
			return
		}
	}
	t.Fatalf("expected violation %q, got: %+v", wantCode, d.Violations)
}

func assertClean(t *testing.T, d *validation.Diagnostics) {
	t.Helper()
	if d != nil {
		t.Fatalf("expected no violations, got: %+v", d.Violations)
	}
}

// ── required ──────────────────────────────────────────────────────────────────

func TestBuiltin_Required_Missing(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=required"`
	}
	assertViolation(t, validateWith[cfg](t, nil), "kongfig.required")
}

func TestBuiltin_Required_Present(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=required"`
	}
	assertClean(t, validateWith[cfg](t, "hello"))
}

// ── notempty ──────────────────────────────────────────────────────────────────

func TestBuiltin_NotEmpty_Missing(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=notempty"`
	}
	assertViolation(t, validateWith[cfg](t, nil), "kongfig.notempty")
}

func TestBuiltin_NotEmpty_Empty(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=notempty"`
	}
	assertViolation(t, validateWith[cfg](t, ""), "kongfig.notempty")
}

func TestBuiltin_NotEmpty_Present(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=notempty"`
	}
	assertClean(t, validateWith[cfg](t, "hello"))
}

// ── min / max ─────────────────────────────────────────────────────────────────

func TestBuiltin_Min_NumericBelow(t *testing.T) {
	type cfg struct {
		F int `kongfig:"f,validate=min(10)"`
	}
	assertViolation(t, validateWith[cfg](t, 5), "kongfig.min")
}

func TestBuiltin_Min_NumericOK(t *testing.T) {
	type cfg struct {
		F int `kongfig:"f,validate=min(10)"`
	}
	assertClean(t, validateWith[cfg](t, 10))
}

func TestBuiltin_Min_StringLenBelow(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=min(5)"`
	}
	assertViolation(t, validateWith[cfg](t, "ab"), "kongfig.min")
}

func TestBuiltin_Min_StringLenOK(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=min(3)"`
	}
	assertClean(t, validateWith[cfg](t, "abc"))
}

func TestBuiltin_Max_NumericAbove(t *testing.T) {
	type cfg struct {
		F int `kongfig:"f,validate=max(100)"`
	}
	assertViolation(t, validateWith[cfg](t, 101), "kongfig.max")
}

func TestBuiltin_Max_NumericOK(t *testing.T) {
	type cfg struct {
		F int `kongfig:"f,validate=max(100)"`
	}
	assertClean(t, validateWith[cfg](t, 100))
}

func TestBuiltin_Max_StringLenAbove(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=max(3)"`
	}
	assertViolation(t, validateWith[cfg](t, "toolong"), "kongfig.max")
}

// ── len ───────────────────────────────────────────────────────────────────────

func TestBuiltin_Len_WrongLength(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=len(5)"`
	}
	assertViolation(t, validateWith[cfg](t, "ab"), "kongfig.len")
}

func TestBuiltin_Len_ExactLength(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=len(5)"`
	}
	assertClean(t, validateWith[cfg](t, "hello"))
}

// ── oneof ─────────────────────────────────────────────────────────────────────

func TestBuiltin_OneOf_Invalid(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=oneof(debug info warn error)"`
	}
	assertViolation(t, validateWith[cfg](t, "trace"), "kongfig.oneof")
}

func TestBuiltin_OneOf_Valid(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=oneof(debug info warn error)"`
	}
	assertClean(t, validateWith[cfg](t, "info"))
}

// ── pattern ───────────────────────────────────────────────────────────────────

func TestBuiltin_Pattern_NoMatch(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=pattern('[a-z]+')"`
	}
	assertViolation(t, validateWith[cfg](t, "UPPER"), "kongfig.pattern")
}

func TestBuiltin_Pattern_Match(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=pattern('[a-z]+')"`
	}
	assertClean(t, validateWith[cfg](t, "lower"))
}

// ── email ─────────────────────────────────────────────────────────────────────

func TestBuiltin_Email_Invalid(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=email"`
	}
	assertViolation(t, validateWith[cfg](t, "notanemail"), "kongfig.email")
}

func TestBuiltin_Email_Valid(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=email"`
	}
	assertClean(t, validateWith[cfg](t, "user@example.com"))
}

// ── url ───────────────────────────────────────────────────────────────────────

func TestBuiltin_URL_Invalid(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=url"`
	}
	assertViolation(t, validateWith[cfg](t, "not-a-url"), "kongfig.url")
}

func TestBuiltin_URL_Valid(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=url"`
	}
	assertClean(t, validateWith[cfg](t, "https://example.com/path"))
}

// ── hostname ──────────────────────────────────────────────────────────────────

func TestBuiltin_Hostname_Invalid(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=hostname"`
	}
	assertViolation(t, validateWith[cfg](t, "not a hostname!"), "kongfig.hostname")
}

func TestBuiltin_Hostname_Valid(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=hostname"`
	}
	assertClean(t, validateWith[cfg](t, "db.internal.example.com"))
}

// ── ip / ipv4 / ipv6 ─────────────────────────────────────────────────────────

func TestBuiltin_IP_Invalid(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=ip"`
	}
	assertViolation(t, validateWith[cfg](t, "999.999.999.999"), "kongfig.ip")
}

func TestBuiltin_IP_IPv4(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=ip"`
	}
	assertClean(t, validateWith[cfg](t, "192.168.1.1"))
}

func TestBuiltin_IP_IPv6(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=ip"`
	}
	assertClean(t, validateWith[cfg](t, "::1"))
}

func TestBuiltin_IPv4_RejectsIPv6(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=ipv4"`
	}
	assertViolation(t, validateWith[cfg](t, "::1"), "kongfig.ipv4")
}

func TestBuiltin_IPv6_RejectsIPv4(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=ipv6"`
	}
	assertViolation(t, validateWith[cfg](t, "192.168.1.1"), "kongfig.ipv6")
}

// ── port ──────────────────────────────────────────────────────────────────────

func TestBuiltin_Port_Zero(t *testing.T) {
	type cfg struct {
		F int `kongfig:"f,validate=port"`
	}
	assertViolation(t, validateWith[cfg](t, 0), "kongfig.port")
}

func TestBuiltin_Port_TooHigh(t *testing.T) {
	type cfg struct {
		F int `kongfig:"f,validate=port"`
	}
	assertViolation(t, validateWith[cfg](t, 70000), "kongfig.port")
}

func TestBuiltin_Port_Valid(t *testing.T) {
	type cfg struct {
		F int `kongfig:"f,validate=port"`
	}
	assertClean(t, validateWith[cfg](t, 8080))
}

// ── file / dir / exists ───────────────────────────────────────────────────────

func TestBuiltin_File_Missing(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=file"`
	}
	assertViolation(t, validateWith[cfg](t, "/no/such/file.txt"), "kongfig.file")
}

func TestBuiltin_File_Valid(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=file"`
	}
	f, err := os.CreateTemp(t.TempDir(), "*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	assertClean(t, validateWith[cfg](t, f.Name()))
}

func TestBuiltin_File_RejectsDir(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=file"`
	}
	assertViolation(t, validateWith[cfg](t, t.TempDir()), "kongfig.file")
}

func TestBuiltin_Dir_Missing(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=dir"`
	}
	assertViolation(t, validateWith[cfg](t, "/no/such/dir"), "kongfig.dir")
}

func TestBuiltin_Dir_Valid(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=dir"`
	}
	assertClean(t, validateWith[cfg](t, t.TempDir()))
}

func TestBuiltin_Dir_RejectsFile(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=dir"`
	}
	f, err := os.CreateTemp(t.TempDir(), "*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	assertViolation(t, validateWith[cfg](t, f.Name()), "kongfig.dir")
}

func TestBuiltin_Exists_Missing(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=exists"`
	}
	assertViolation(t, validateWith[cfg](t, "/no/such/path"), "kongfig.exists")
}

func TestBuiltin_Exists_File(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=exists"`
	}
	f, err := os.CreateTemp(t.TempDir(), "*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	assertClean(t, validateWith[cfg](t, f.Name()))
}

func TestBuiltin_Exists_Dir(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=exists"`
	}
	assertClean(t, validateWith[cfg](t, t.TempDir()))
}

// ── Compile ───────────────────────────────────────────────────────────────────

func TestCompile_AllKnown(t *testing.T) {
	type cfg struct {
		Host string `kongfig:"host,validate=required"`
		Port int    `kongfig:"port,validate=all(min(1) max(65535))"`
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[cfg]())
	if err := v.Compile(); err != nil {
		t.Fatalf("Compile returned unexpected error: %v", err)
	}
}

func TestCompile_UnknownAnnotation(t *testing.T) {
	type cfg struct {
		Host string `kongfig:"host,validate=typo_annotation"`
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[cfg]())
	if err := v.Compile(); err == nil {
		t.Fatal("Compile should return error for unknown annotation")
	}
}

func TestCompile_AfterRegisterAnnotation(t *testing.T) {
	// Registering an annotation after AddSchema but before Compile must succeed.
	type cfg struct {
		Mode string `kongfig:"mode,validate=latehandler"`
	}
	reg := validation.NewEmptyRegistry()
	v := mustNewWith(t, reg)
	v.AddSchema(validation.Schema[cfg]())

	// Handler not yet registered — Compile should fail.
	if err := v.Compile(); err == nil {
		t.Fatal("Compile should fail before handler is registered")
	}

	reg.Register("latehandler", validation.AnnotationFieldFunc(func(_ validation.AnnotationEvent) []validation.FieldViolation {
		return nil
	}))

	// Handler now registered — Compile should pass.
	if err := v.Compile(); err != nil {
		t.Fatalf("Compile should pass after handler is registered, got: %v", err)
	}
}

func TestCompile_EmptyValidator(t *testing.T) {
	// No schemas registered — Compile always succeeds.
	v := validation.NewWithDefaults()
	if err := v.Compile(); err != nil {
		t.Fatalf("Compile on empty validator: %v", err)
	}
}

// ── skipped ───────────────────────────────────────────────────────────────────

// TestBuiltin_AbsentFieldSkipped verifies that all built-ins are no-ops for absent fields
// (except required/notempty which explicitly check presence).
func TestBuiltin_AbsentFieldSkipped(t *testing.T) {
	type cfg struct {
		F string `kongfig:"f,validate=all(min(999) max(0) len(999) oneof(x) pattern('[0-9]{99}') email url hostname ip ipv4 ipv6)"`
	}
	k := kongfig.New()
	if err := k.Load(t.Context(), &staticProvider{data: map[string]any{}, source: "test"}); err != nil {
		t.Fatal(err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[cfg]())
	assertClean(t, v.Validate(k)) // absent field → all skipped
}

func TestBuiltin_Port_AbsentSkipped(t *testing.T) {
	type cfg struct {
		F int `kongfig:"f,validate=port"`
	}
	k := kongfig.New()
	if err := k.Load(t.Context(), &staticProvider{data: map[string]any{}, source: "test"}); err != nil {
		t.Fatal(err)
	}
	v := validation.NewWithDefaults()
	v.AddSchema(validation.Schema[cfg]())
	assertClean(t, v.Validate(k))
}
