package kongfig_test

import (
	"testing"
	"time"

	kongfig "github.com/pmarschik/kongfig"
)

// A value read back from a parser is rarely spelled the way the caller wrote it:
// an integer comes back as int64, a nested table as map[string]any. Comparing
// those with == or reflect.DeepEqual reports a difference where the document
// holds exactly what was asked for, which is why the check needs its own answer
// to "same value".
func TestEqualValues_SameNumberDifferentGoType(t *testing.T) {
	for _, tc := range []struct {
		a    any
		b    any
		name string
	}{
		{name: "int and int64", a: 3, b: int64(3)},
		{name: "int64 and float64", a: int64(3), b: 3.0},
		{name: "uint and int", a: uint(7), b: 7},
		{name: "float32 and float64", a: float32(1.5), b: 1.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !kongfig.EqualValues(tc.a, tc.b) {
				t.Errorf("EqualValues(%#v, %#v) = false, want true", tc.a, tc.b)
			}
			if !kongfig.EqualValues(tc.b, tc.a) {
				t.Errorf("EqualValues is not symmetric for %#v and %#v", tc.a, tc.b)
			}
		})
	}
}

// Numbers that differ are still different, and a number is not the string that
// spells it — a rewrite that quoted a port has changed the document.
func TestEqualValues_DifferentValues(t *testing.T) {
	for _, tc := range []struct {
		a    any
		b    any
		name string
	}{
		{name: "different ints", a: 3, b: 4},
		{name: "int and its string", a: 3, b: "3"},
		{name: "true and 1", a: true, b: 1},
		{name: "nil and empty string", a: nil, b: ""},
		{name: "float rounding", a: 1.5, b: 2.0},
		{name: "large int64 and float", a: int64(1) << 62, b: float64(int64(1) << 62)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if kongfig.EqualValues(tc.a, tc.b) {
				t.Errorf("EqualValues(%#v, %#v) = true, want false", tc.a, tc.b)
			}
		})
	}
}

// A map is the same map whichever of the two shapes kongfig uses for it, and
// nesting does not change that.
func TestEqualValues_MapShapesAgree(t *testing.T) {
	a := kongfig.ConfigData{"server": kongfig.ConfigData{"port": 8080}}
	b := map[string]any{"server": map[string]any{"port": int64(8080)}}

	if !kongfig.EqualValues(a, b) {
		t.Error("a ConfigData and the same map[string]any compared unequal")
	}
	if kongfig.EqualValues(a, map[string]any{"server": map[string]any{"port": 8080, "host": "h"}}) {
		t.Error("an extra key compared equal")
	}
	if kongfig.EqualValues(a, map[string]any{}) {
		t.Error("a missing key compared equal")
	}
}

// A key holding nil is not the same as a key that is not there: a rewrite that
// dropped a key wrote a different document, and the caller has to hear about it.
func TestEqualValues_NilValueIsNotAMissingKey(t *testing.T) {
	if kongfig.EqualValues(kongfig.ConfigData{"a": nil}, kongfig.ConfigData{}) {
		t.Error("a key holding nil compared equal to no key at all")
	}
}

// Lists are compared element by element, so a typed slice from the caller and
// the []any a parser produces agree — and order still matters.
func TestEqualValues_SliceShapesAgree(t *testing.T) {
	if !kongfig.EqualValues([]string{"a", "b"}, []any{"a", "b"}) {
		t.Error("a []string and the same []any compared unequal")
	}
	if !kongfig.EqualValues([]int{1, 2}, []any{int64(1), 2.0}) {
		t.Error("a []int and the same numbers as []any compared unequal")
	}
	if kongfig.EqualValues([]any{"a", "b"}, []any{"b", "a"}) {
		t.Error("a reordered list compared equal")
	}
	if kongfig.EqualValues([]any{"a"}, []any{"a", "b"}) {
		t.Error("a longer list compared equal")
	}
	if kongfig.EqualValues([]any{}, "") {
		t.Error("an empty list compared equal to an empty string")
	}
}

// Render wrapping is kongfig's own bookkeeping, not part of the value: data that
// went through a render path still holds what the document holds.
func TestEqualValues_UnwrapsRenderedValues(t *testing.T) {
	wrapped := kongfig.ConfigData{"port": kongfig.RenderedValue{Value: int64(8080)}}
	if !kongfig.EqualValues(wrapped, kongfig.ConfigData{"port": 8080}) {
		t.Error("a RenderedValue compared unequal to the value it wraps")
	}
	redacted := kongfig.ConfigData{"token": kongfig.RenderedValue{
		Value: "s3cret", Redacted: true, RedactedDisplay: "***",
	}}
	if !kongfig.EqualValues(redacted, kongfig.ConfigData{"token": "s3cret"}) {
		t.Error("a redacted RenderedValue compared against its display instead of its value")
	}
}

// A TOML datetime comes back as a time.Time, whose == also compares the location
// it was parsed in — the same instant written two ways is one value.
func TestEqualValues_TimesCompareByInstant(t *testing.T) {
	utc := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	elsewhere := utc.In(time.FixedZone("plus2", 2*60*60))

	if !kongfig.EqualValues(utc, elsewhere) {
		t.Error("the same instant in two zones compared unequal")
	}
	if kongfig.EqualValues(utc, utc.Add(time.Second)) {
		t.Error("two instants a second apart compared equal")
	}
}

// EqualConfigData is the entry point the document check uses, and reports on two
// whole trees rather than one value.
func TestEqualConfigData(t *testing.T) {
	want := kongfig.ConfigData{
		"archive": []any{"*.log", "*.tmp"},
		"aux":     []any{map[string]any{"dir": "a"}},
	}
	got := kongfig.ConfigData{
		"archive": []string{"*.log", "*.tmp"},
		"aux":     []any{kongfig.ConfigData{"dir": "a"}},
	}
	if !kongfig.EqualConfigData(got, want) {
		t.Error("two spellings of the same document compared unequal")
	}
	if kongfig.EqualConfigData(got, kongfig.ConfigData{}) {
		t.Error("an empty document compared equal to a full one")
	}
	if !kongfig.EqualConfigData(nil, kongfig.ConfigData{}) {
		t.Error("nil and an empty document compared unequal")
	}
}
