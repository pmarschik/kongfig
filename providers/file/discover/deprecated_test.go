package discover_test

import (
	"context"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/providers/file/discover"
)

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
