package kongfig_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newKongfigForMigrate() *kongfig.Kongfig { return kongfig.New() }

func loadData(t *testing.T, k *kongfig.Kongfig, data map[string]any, source string) error {
	t.Helper()
	return k.Load(context.Background(), &staticProvider{data: data, source: source})
}

// ── AddRename ─────────────────────────────────────────────────────────────────

func TestAddRename_AbsentKey_NoOp(t *testing.T) {
	k := newKongfigForMigrate()
	var called bool
	k.AddRename("old.host", "new.host", kongfig.MigrationPolicy{
		OnFirst: func(kongfig.MigrationEvent) kongfig.MigrationResult { called = true; return kongfig.MigrationResult{} },
	})

	if err := loadData(t, k, map[string]any{"other": "val"}, "test"); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("handler must not fire when old key is absent")
	}
}

func TestAddRename_MovesValue(t *testing.T) {
	k := newKongfigForMigrate()
	var got kongfig.MigrationEvent
	k.AddRename("db.host", "database.host", kongfig.MigrationPolicy{
		OnFirst: func(e kongfig.MigrationEvent) kongfig.MigrationResult { got = e; return kongfig.MigrationResult{} },
	})

	if err := loadData(t, k, map[string]any{"db": map[string]any{"host": "localhost"}}, "file"); err != nil {
		t.Fatal(err)
	}

	ev, ok := got.(kongfig.RenameEvent)
	if !ok {
		t.Fatalf("expected RenameEvent, got %T", got)
	}
	if ev.OldPath != "db.host" || ev.NewPath != "database.host" {
		t.Errorf("paths: got old=%q new=%q", ev.OldPath, ev.NewPath)
	}
	if ev.Value != "localhost" {
		t.Errorf("value: got %v", ev.Value)
	}
	if ev.SourceName != "file" {
		t.Errorf("source: got %q", ev.SourceName)
	}
	if ev.Occurrence != 1 {
		t.Errorf("occurrence: got %d", ev.Occurrence)
	}

	// old key must be gone; new key must be set
	all := k.All()
	if _, exists := all.LookupPath("db.host"); exists {
		t.Error("old key should be absent after rename")
	}
	if v, exists := all.LookupPath("database.host"); !exists || v != "localhost" {
		t.Errorf("new key should carry value %q, got %v (exists=%v)", "localhost", v, exists)
	}
}

func TestAddRename_Conflict_DropsOld(t *testing.T) {
	k := newKongfigForMigrate()
	var got kongfig.MigrationEvent
	k.AddRename("old", "new", kongfig.MigrationPolicy{
		OnFirst: func(e kongfig.MigrationEvent) kongfig.MigrationResult { got = e; return kongfig.MigrationResult{} },
	})

	if err := loadData(t, k, map[string]any{"old": "old-val", "new": "new-val"}, "file"); err != nil {
		t.Fatal(err)
	}

	ev, ok := got.(kongfig.ConflictEvent)
	if !ok {
		t.Fatalf("expected ConflictEvent, got %T", got)
	}
	if ev.OldValue != "old-val" || ev.NewValue != "new-val" {
		t.Errorf("values: got old=%v new=%v", ev.OldValue, ev.NewValue)
	}

	all := k.All()
	if _, exists := all.LookupPath("old"); exists {
		t.Error("old key should be dropped on conflict")
	}
	if v, _ := all.LookupPath("new"); v != "new-val" {
		t.Errorf("new key should be preserved, got %v", v)
	}
}

func TestAddRename_OccurrenceTracking(t *testing.T) {
	k := newKongfigForMigrate()
	var occs []int
	appendOcc := func(e kongfig.MigrationEvent) kongfig.MigrationResult {
		if ev, ok := e.(kongfig.RenameEvent); ok {
			occs = append(occs, ev.Occurrence)
		}
		return kongfig.MigrationResult{}
	}
	policy := kongfig.MigrationPolicy{OnFirst: appendOcc, OnRepeat: appendOcc}
	k.AddRename("old", "new", policy)

	for range 3 {
		if err := loadData(t, k, map[string]any{"old": "v"}, "test"); err != nil {
			t.Fatal(err)
		}
	}

	if len(occs) != 3 || occs[0] != 1 || occs[1] != 2 || occs[2] != 3 {
		t.Errorf("occurrence sequence: got %v", occs)
	}
}

func TestAddRename_MigrationFail(t *testing.T) {
	k := newKongfigForMigrate()
	k.AddRename("old", "new", kongfig.MigrationPolicy{OnFirst: kongfig.MigrationFail})

	err := loadData(t, k, map[string]any{"old": "v"}, "file")
	if err == nil {
		t.Fatal("expected error from MigrationFail, got nil")
	}
}

func TestAddRename_SubtreeRename(t *testing.T) {
	k := newKongfigForMigrate()
	k.AddRename("database", "db")

	if err := loadData(t, k, map[string]any{"database": map[string]any{"host": "h", "port": 5432}}, "file"); err != nil {
		t.Fatal(err)
	}

	all := k.All()
	if _, exists := all.LookupPath("database"); exists {
		t.Error("old subtree should be absent")
	}
	if v, exists := all.LookupPath("db.host"); !exists || v != "h" {
		t.Errorf("db.host should be moved, got %v (exists=%v)", v, exists)
	}
}

func TestAddRename_DefaultPolicy_Silent(t *testing.T) {
	// Verify default policy does not error (MigrationWarn on first, MigrationDebug on repeat)
	k := newKongfigForMigrate()
	k.AddRename("old", "new")

	if err := loadData(t, k, map[string]any{"old": "v"}, "test"); err != nil {
		t.Fatalf("default policy must not error: %v", err)
	}
}

func TestAddRename_MultipleRenames(t *testing.T) {
	k := newKongfigForMigrate()
	k.AddRename("a", "x")
	k.AddRename("b", "y")

	if err := loadData(t, k, map[string]any{"a": 1, "b": 2}, "test"); err != nil {
		t.Fatal(err)
	}

	all := k.All()
	if v, _ := all.LookupPath("x"); v != 1 {
		t.Errorf("x should be 1, got %v", v)
	}
	if v, _ := all.LookupPath("y"); v != 2 {
		t.Errorf("y should be 2, got %v", v)
	}
}

// ── MigrationWarnResult / warning accumulation ───────────────────────────────

func TestMigrationWarnResult_NonFatal(t *testing.T) {
	k := newKongfigForMigrate()
	k.AddRename("old", "new", kongfig.MigrationPolicy{OnFirst: kongfig.MigrationWarnResult})

	if err := loadData(t, k, map[string]any{"old": "v"}, "test"); err != nil {
		t.Fatalf("MigrationWarnResult must not fail Load: %v", err)
	}
}

func TestMigrationWarnResult_AccumulatesWarning(t *testing.T) {
	k := newKongfigForMigrate()
	k.AddRename("old.key", "new.key", kongfig.MigrationPolicy{OnFirst: kongfig.MigrationWarnResult})

	if err := loadData(t, k, map[string]any{"old": map[string]any{"key": "v"}}, "myfile"); err != nil {
		t.Fatal(err)
	}

	warnings := k.MigrationWarnings()
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	for _, want := range []string{"old.key", "new.key", "myfile"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("warning should mention %q; got: %s", want, warnings[0])
		}
	}
}

func TestMigrationWarnings_AccumulatesAcrossLoads(t *testing.T) {
	k := newKongfigForMigrate()
	k.AddRename("old", "new", kongfig.MigrationPolicy{
		OnFirst:  kongfig.MigrationWarnResult,
		OnRepeat: kongfig.MigrationWarnResult,
	})

	for range 3 {
		if err := loadData(t, k, map[string]any{"old": "v"}, "test"); err != nil {
			t.Fatal(err)
		}
	}

	if got := len(k.MigrationWarnings()); got != 3 {
		t.Errorf("expected 3 warnings, got %d", got)
	}
}

func TestClearMigrationWarnings(t *testing.T) {
	k := newKongfigForMigrate()
	k.AddRename("old", "new", kongfig.MigrationPolicy{OnFirst: kongfig.MigrationWarnResult})

	if err := loadData(t, k, map[string]any{"old": "v"}, "test"); err != nil {
		t.Fatal(err)
	}

	k.ClearMigrationWarnings()
	if got := k.MigrationWarnings(); len(got) != 0 {
		t.Errorf("expected empty after clear, got %v", got)
	}
}

func TestMigrationWarnings_EmptyByDefault(t *testing.T) {
	k := newKongfigForMigrate()
	if got := k.MigrationWarnings(); got != nil {
		t.Errorf("expected nil before any load, got %v", got)
	}
}

func TestMigrationWarnResult_NoWarning_WhenKeyAbsent(t *testing.T) {
	k := newKongfigForMigrate()
	k.AddRename("old", "new", kongfig.MigrationPolicy{OnFirst: kongfig.MigrationWarnResult})

	if err := loadData(t, k, map[string]any{"other": "v"}, "test"); err != nil {
		t.Fatal(err)
	}
	if got := k.MigrationWarnings(); len(got) != 0 {
		t.Errorf("expected no warnings when old key absent, got %v", got)
	}
}

func TestMigrationWarnings_NotAccumulated_OnFailedLoad(t *testing.T) {
	k := newKongfigForMigrate()
	// OnFirst fails the load; warning should NOT be accumulated since Err takes precedence.
	k.AddRename("old", "new", kongfig.MigrationPolicy{
		OnFirst: func(kongfig.MigrationEvent) kongfig.MigrationResult {
			return kongfig.MigrationResult{Err: errors.New("forced fail")}
		},
	})

	if err := loadData(t, k, map[string]any{"old": "v"}, "test"); err == nil {
		t.Fatal("expected error from failing handler")
	}
	if got := k.MigrationWarnings(); len(got) != 0 {
		t.Errorf("expected no warnings on failed load, got %v", got)
	}
}

func TestAddWarning_AccumulatesAlongsideMigrationWarnings(t *testing.T) {
	k := newKongfigForMigrate()
	k.AddWarning("app-level diagnostic")
	got := k.MigrationWarnings()
	if len(got) != 1 || got[0] != "app-level diagnostic" {
		t.Errorf("AddWarning: got %v, want [\"app-level diagnostic\"]", got)
	}

	// Interleave with a migration warning to confirm both accumulate.
	k.AddRename("old", "new", kongfig.MigrationPolicy{OnFirst: kongfig.MigrationWarnResult})
	if err := loadData(t, k, map[string]any{"old": "v"}, "test"); err != nil {
		t.Fatal(err)
	}
	if len(k.MigrationWarnings()) != 2 {
		t.Errorf("expected 2 warnings total, got %v", k.MigrationWarnings())
	}
}

// ── MigrationEvent built-in handlers ─────────────────────────────────────────

func TestMigrationSilent(t *testing.T) {
	k := newKongfigForMigrate()
	k.AddRename("old", "new", kongfig.MigrationPolicy{OnFirst: kongfig.MigrationSilent})
	if err := loadData(t, k, map[string]any{"old": "v"}, "test"); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationFail_ErrorContainsKeyInfo(t *testing.T) {
	k := newKongfigForMigrate()
	k.AddRename("old.key", "new.key", kongfig.MigrationPolicy{OnFirst: kongfig.MigrationFail})

	err := loadData(t, k, map[string]any{"old": map[string]any{"key": "v"}}, "myfile")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"old.key", "new.key", "myfile"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message should mention %q; got: %s", want, msg)
		}
	}
}
