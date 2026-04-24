package mergefuncs_test

import (
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/mergefuncs"
)

func applyMerge(t *testing.T, fn kongfig.MergeFunc, dst, src any) any {
	t.Helper()
	result, err := fn(dst, src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return result
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func TestAppendSlice(t *testing.T) {
	got, ok := applyMerge(t, mergefuncs.AppendSlice, toAny([]string{"a", "b"}), toAny([]string{"c", "d"})).([]any)
	if !ok {
		t.Fatal("result is not []any")
	}
	want := toAny([]string{"a", "b", "c", "d"})
	if len(got) != len(want) {
		t.Fatalf("AppendSlice got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AppendSlice[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestAppendSlice_NilDst(t *testing.T) {
	got, ok := applyMerge(t, mergefuncs.AppendSlice, nil, toAny([]string{"x"})).([]any)
	if !ok || len(got) != 1 || got[0] != "x" {
		t.Fatalf("AppendSlice(nil, [x]) = %v", got)
	}
}

func TestAppendSlice_ErrorOnNonSlice(t *testing.T) {
	if _, err := mergefuncs.AppendSlice(nil, "not-a-slice"); err == nil {
		t.Fatal("expected error for non-slice src")
	}
}

func TestReplaceSlice(t *testing.T) {
	got, ok := applyMerge(t, mergefuncs.ReplaceSlice, toAny([]string{"a", "b"}), toAny([]string{"c"})).([]any)
	if !ok || len(got) != 1 || got[0] != "c" {
		t.Fatalf("ReplaceSlice got %v, want [c]", got)
	}
}

func TestUnionSet(t *testing.T) {
	got, ok := applyMerge(t, mergefuncs.UnionSet, toAny([]string{"a", "b"}), toAny([]string{"b", "c"})).([]any)
	if !ok {
		t.Fatal("result is not []any")
	}
	want := toAny([]string{"a", "b", "c"})
	if len(got) != len(want) {
		t.Fatalf("UnionSet got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("UnionSet[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestUnionSet_Dedup(t *testing.T) {
	got, ok := applyMerge(t, mergefuncs.UnionSet, toAny([]string{"x"}), toAny([]string{"x", "y"})).([]any)
	if !ok {
		t.Fatal("result is not []any")
	}
	if len(got) != 2 {
		t.Fatalf("UnionSet dedup got %v, want [x y]", got)
	}
}
