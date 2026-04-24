package kongfig_test

import (
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/providers/file/discover"
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
		OnFirst: func(kongfig.MigrationEvent) error { called = true; return nil },
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
		OnFirst: func(e kongfig.MigrationEvent) error { got = e; return nil },
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
		OnFirst: func(e kongfig.MigrationEvent) error { got = e; return nil },
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
	appendOcc := func(e kongfig.MigrationEvent) error {
		if ev, ok := e.(kongfig.RenameEvent); ok {
			occs = append(occs, ev.Occurrence)
		}
		return nil
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

// ── discover.Deprecated ───────────────────────────────────────────────────────

// recordingDiscoverer is a fake Discoverer that returns a fixed path.
type recordingDiscoverer struct {
	name   string
	path   string
	callsN int
}

func (d *recordingDiscoverer) Name() string { return d.name }
func (d *recordingDiscoverer) Discover(_ context.Context, _ []string) (string, error) {
	d.callsN++
	return d.path, nil
}

func TestDeprecated_FiresLegacyFileEvent(t *testing.T) {
	inner := &recordingDiscoverer{name: "xdg", path: "/old/config.yaml"}
	var got kongfig.MigrationEvent
	policy := kongfig.MigrationPolicy{
		OnFirst: func(e kongfig.MigrationEvent) error { got = e; return nil },
	}

	wrapped := discover.Deprecated(inner, "/new/config.yaml", policy)

	path, err := wrapped.Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/old/config.yaml" {
		t.Errorf("path: got %q", path)
	}

	ev, ok := got.(kongfig.LegacyFileEvent)
	if !ok {
		t.Fatalf("expected LegacyFileEvent, got %T", got)
	}
	if ev.FilePath != "/old/config.yaml" {
		t.Errorf("FilePath: got %q", ev.FilePath)
	}
	if ev.PreferredPath != "/new/config.yaml" {
		t.Errorf("PreferredPath: got %q", ev.PreferredPath)
	}
	if ev.SourceName != "xdg" {
		t.Errorf("SourceName: got %q", ev.SourceName)
	}
	if ev.Occurrence != 1 {
		t.Errorf("Occurrence: got %d", ev.Occurrence)
	}
}

func TestDeprecated_NoFileFound_NoEvent(t *testing.T) {
	inner := &recordingDiscoverer{name: "xdg", path: ""} // returns empty = not found
	var called bool
	wrapped := discover.Deprecated(inner, "/new/config.yaml", kongfig.MigrationPolicy{
		OnFirst: func(kongfig.MigrationEvent) error { called = true; return nil },
	})

	path, err := wrapped.Discover(context.Background(), []string{".yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Errorf("expected empty path, got %q", path)
	}
	if called {
		t.Error("handler must not fire when no file is found")
	}
}

func TestDeprecated_MigrationFail_PropagatesError(t *testing.T) {
	inner := &recordingDiscoverer{name: "xdg", path: "/old/config.yaml"}
	wrapped := discover.Deprecated(inner, "/new/config.yaml", kongfig.MigrationPolicy{
		OnFirst: kongfig.MigrationFail,
	})

	_, err := wrapped.Discover(context.Background(), []string{".yaml"})
	if err == nil {
		t.Fatal("expected error from MigrationFail, got nil")
	}
}

func TestDeprecated_OccurrenceTracking(t *testing.T) {
	inner := &recordingDiscoverer{name: "workdir", path: "/old.yaml"}
	var occs []int
	appendOcc := func(e kongfig.MigrationEvent) error {
		if ev, ok := e.(kongfig.LegacyFileEvent); ok {
			occs = append(occs, ev.Occurrence)
		}
		return nil
	}
	wrapped := discover.Deprecated(inner, "/new.yaml", kongfig.MigrationPolicy{OnFirst: appendOcc, OnRepeat: appendOcc})

	for range 3 {
		if _, err := wrapped.Discover(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
	}

	if len(occs) != 3 || occs[0] != 1 || occs[1] != 2 || occs[2] != 3 {
		t.Errorf("occurrence sequence: got %v", occs)
	}
}

func TestDeprecated_Name_DelegatesToInner(t *testing.T) {
	inner := &recordingDiscoverer{name: "git-root"}
	wrapped := discover.Deprecated(inner, "/new.yaml")
	if wrapped.Name() != "git-root" {
		t.Errorf("Name: got %q", wrapped.Name())
	}
}

func TestDeprecated_DisplayPath_ForwardedWhenSupported(t *testing.T) {
	inner := &displayingDiscoverer{name: "xdg", path: "/old.yaml", display: "$XDG/config.yaml"}
	wrapped := discover.Deprecated(inner, "/new.yaml")

	type displayPather interface {
		DisplayPath(context.Context, string) string
	}
	dp, ok := any(wrapped).(displayPather)
	if !ok {
		t.Fatal("expected wrapped to implement DisplayPath")
	}
	if got := dp.DisplayPath(context.Background(), "/old.yaml"); got != "$XDG/config.yaml" {
		t.Errorf("DisplayPath: got %q", got)
	}
}

// displayingDiscoverer is a fake that also implements DisplayPath.
type displayingDiscoverer struct {
	name    string
	path    string
	display string
}

func (d *displayingDiscoverer) Name() string { return d.name }
func (d *displayingDiscoverer) Discover(_ context.Context, _ []string) (string, error) {
	return d.path, nil
}
func (d *displayingDiscoverer) DisplayPath(_ context.Context, _ string) string { return d.display }

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
