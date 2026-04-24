package validation_test

import (
	"slices"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/validation"
)

// ── test helpers ──────────────────────────────────────────────────────────────

func assertFieldViol(t *testing.T, viols []validation.FieldViolation, wantCode string) {
	t.Helper()
	if slices.ContainsFunc(viols, func(v validation.FieldViolation) bool { return v.Code == wantCode }) {
		return
	}
	t.Fatalf("expected violation %q; got: %+v", wantCode, viols)
}

func assertFieldViolClean(t *testing.T, viols []validation.FieldViolation) {
	t.Helper()
	if len(viols) != 0 {
		t.Fatalf("expected no violations; got: %+v", viols)
	}
}

// ── ExactlyOneOf ──────────────────────────────────────────────────────────────

func TestExactlyOneOf_NoneSet(t *testing.T) {
	type cfg struct {
		A string `kongfig:"a"`
		B string `kongfig:"b"`
	}
	c := cfg{}
	assertFieldViol(t, validation.ExactlyOneOf(&c, &c.A, &c.B), "kongfig.exactly_one_of")
}

func TestExactlyOneOf_OneSet(t *testing.T) {
	type cfg struct {
		A string `kongfig:"a"`
		B string `kongfig:"b"`
	}
	c := cfg{A: "value"}
	assertFieldViolClean(t, validation.ExactlyOneOf(&c, &c.A, &c.B))
}

func TestExactlyOneOf_BothSet(t *testing.T) {
	type cfg struct {
		A string `kongfig:"a"`
		B string `kongfig:"b"`
	}
	c := cfg{A: "value", B: "other"}
	assertFieldViol(t, validation.ExactlyOneOf(&c, &c.A, &c.B), "kongfig.exactly_one_of")
}

// ── AtLeastOneOf ──────────────────────────────────────────────────────────────

func TestAtLeastOneOf_NoneSet(t *testing.T) {
	type cfg struct {
		A string `kongfig:"a"`
		B string `kongfig:"b"`
	}
	c := cfg{}
	assertFieldViol(t, validation.AtLeastOneOf(&c, &c.A, &c.B), "kongfig.at_least_one_of")
}

func TestAtLeastOneOf_OneSet(t *testing.T) {
	type cfg struct {
		A string `kongfig:"a"`
		B string `kongfig:"b"`
	}
	c := cfg{B: "value"}
	assertFieldViolClean(t, validation.AtLeastOneOf(&c, &c.A, &c.B))
}

func TestAtLeastOneOf_AllSet(t *testing.T) {
	type cfg struct {
		A string `kongfig:"a"`
		B string `kongfig:"b"`
	}
	c := cfg{A: "x", B: "y"}
	assertFieldViolClean(t, validation.AtLeastOneOf(&c, &c.A, &c.B))
}

// ── MutuallyExclusive ─────────────────────────────────────────────────────────

func TestMutuallyExclusive_NoneSet(t *testing.T) {
	type cfg struct {
		A string `kongfig:"a"`
		B string `kongfig:"b"`
	}
	c := cfg{}
	assertFieldViolClean(t, validation.MutuallyExclusive(&c, &c.A, &c.B))
}

func TestMutuallyExclusive_OneSet(t *testing.T) {
	type cfg struct {
		A string `kongfig:"a"`
		B string `kongfig:"b"`
	}
	c := cfg{A: "value"}
	assertFieldViolClean(t, validation.MutuallyExclusive(&c, &c.A, &c.B))
}

func TestMutuallyExclusive_BothSet(t *testing.T) {
	type cfg struct {
		A string `kongfig:"a"`
		B string `kongfig:"b"`
	}
	c := cfg{A: "x", B: "y"}
	assertFieldViol(t, validation.MutuallyExclusive(&c, &c.A, &c.B), "kongfig.mutually_exclusive")
}

// ── AllOrNone ─────────────────────────────────────────────────────────────────

func TestAllOrNone_NoneSet(t *testing.T) {
	type cfg struct {
		Cert string `kongfig:"cert"`
		Key  string `kongfig:"key"`
	}
	c := cfg{}
	assertFieldViolClean(t, validation.AllOrNone(&c, &c.Cert, &c.Key))
}

func TestAllOrNone_AllSet(t *testing.T) {
	type cfg struct {
		Cert string `kongfig:"cert"`
		Key  string `kongfig:"key"`
	}
	c := cfg{Cert: "/cert.pem", Key: "/key.pem"}
	assertFieldViolClean(t, validation.AllOrNone(&c, &c.Cert, &c.Key))
}

func TestAllOrNone_Partial(t *testing.T) {
	type cfg struct {
		Cert string `kongfig:"cert"`
		Key  string `kongfig:"key"`
	}
	c := cfg{Cert: "/cert.pem"}
	assertFieldViol(t, validation.AllOrNone(&c, &c.Cert, &c.Key), "kongfig.all_or_none")
}

// ── RequiredWith ──────────────────────────────────────────────────────────────

func TestRequiredWith_TriggerUnset(t *testing.T) {
	type cfg struct {
		Username string `kongfig:"username"`
		Password string `kongfig:"password"`
	}
	c := cfg{}
	assertFieldViolClean(t, validation.RequiredWith(&c, &c.Password, &c.Username))
}

func TestRequiredWith_TriggerSetFieldSet(t *testing.T) {
	type cfg struct {
		Username string `kongfig:"username"`
		Password string `kongfig:"password"`
	}
	c := cfg{Username: "alice", Password: "secret"}
	assertFieldViolClean(t, validation.RequiredWith(&c, &c.Password, &c.Username))
}

func TestRequiredWith_TriggerSetFieldUnset(t *testing.T) {
	type cfg struct {
		Username string `kongfig:"username"`
		Password string `kongfig:"password"`
	}
	c := cfg{Username: "alice"}
	assertFieldViol(t, validation.RequiredWith(&c, &c.Password, &c.Username), "kongfig.required_with")
}

// ── RequiredWithout ───────────────────────────────────────────────────────────

func TestRequiredWithout_FallbackSet(t *testing.T) {
	type cfg struct {
		DSN  string `kongfig:"dsn"`
		Host string `kongfig:"host"`
	}
	c := cfg{Host: "localhost"}
	assertFieldViolClean(t, validation.RequiredWithout(&c, &c.DSN, &c.Host))
}

func TestRequiredWithout_FallbackUnsetFieldSet(t *testing.T) {
	type cfg struct {
		DSN  string `kongfig:"dsn"`
		Host string `kongfig:"host"`
	}
	c := cfg{DSN: "postgres://localhost/db"}
	assertFieldViolClean(t, validation.RequiredWithout(&c, &c.DSN, &c.Host))
}

func TestRequiredWithout_FallbackUnsetFieldUnset(t *testing.T) {
	type cfg struct {
		DSN  string `kongfig:"dsn"`
		Host string `kongfig:"host"`
	}
	c := cfg{}
	assertFieldViol(t, validation.RequiredWithout(&c, &c.DSN, &c.Host), "kongfig.required_without")
}

// ── Nested struct path resolution ─────────────────────────────────────────────

func TestRuleHelpers_NestedPath(t *testing.T) {
	type db struct {
		Host string `kongfig:"host"`
		Port int    `kongfig:"port"`
	}
	type cfg struct {
		Primary db `kongfig:"primary"`
		Replica db `kongfig:"replica"`
	}
	c := cfg{Primary: db{Host: "db1"}}
	viols := validation.MutuallyExclusive(&c, &c.Primary.Host, &c.Replica.Host)
	assertFieldViolClean(t, viols)

	c2 := cfg{Primary: db{Host: "db1"}, Replica: db{Host: "db2"}}
	viols2 := validation.MutuallyExclusive(&c2, &c2.Primary.Host, &c2.Replica.Host)
	assertFieldViol(t, viols2, "kongfig.mutually_exclusive")
}

// ── Rule integration ──────────────────────────────────────────────────────────

func loadKongfig(t *testing.T, data map[string]any) *kongfig.Kongfig {
	t.Helper()
	k := kongfig.New()
	if err := k.Load(t.Context(), &staticProvider{data: data, source: "test"}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return k
}

func TestRuleHelpers_ViaRule(t *testing.T) {
	type tlsCfg struct {
		CertFile string `kongfig:"cert_file"`
		KeyFile  string `kongfig:"key_file"`
	}

	k := loadKongfig(t, map[string]any{"cert_file": "/cert.pem"}) // key_file absent
	v := validation.NewWithDefaults()
	v.AddRule(validation.Rule(func(c tlsCfg) []validation.FieldViolation {
		return validation.AllOrNone(&c, &c.CertFile, &c.KeyFile)
	}))
	assertViolation(t, v.Validate(k), "kongfig.all_or_none")
}

func TestRuleHelpers_ViaRule_BothSet(t *testing.T) {
	type tlsCfg struct {
		CertFile string `kongfig:"cert_file"`
		KeyFile  string `kongfig:"key_file"`
	}

	k := loadKongfig(t, map[string]any{"cert_file": "/cert.pem", "key_file": "/key.pem"})
	v := validation.NewWithDefaults()
	v.AddRule(validation.Rule(func(c tlsCfg) []validation.FieldViolation {
		return validation.AllOrNone(&c, &c.CertFile, &c.KeyFile)
	}))
	assertClean(t, v.Validate(k))
}
