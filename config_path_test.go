package kongfig_test

import (
	"context"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/schema"
)

func TestConfigPaths_Basic(t *testing.T) {
	type cfg struct {
		SystemConfig string `kongfig:"system-config,config-path=0"`
		EnvConfig    string `kongfig:"env-config,config-path=1"`
		UserConfig   string `kongfig:"user-config,config-path"`
	}
	got := schema.ConfigPaths[cfg]()
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(got), got)
	}
	// Priority 0 first
	if got[0].Key != "system-config" || !got[0].HasPriority || got[0].Priority != 0 {
		t.Errorf("entry[0]: got %+v, want system-config prio=0", got[0])
	}
	// Priority 1 second
	if got[1].Key != "env-config" || !got[1].HasPriority || got[1].Priority != 1 {
		t.Errorf("entry[1]: got %+v, want env-config prio=1", got[1])
	}
	// No priority last
	if got[2].Key != "user-config" || got[2].HasPriority {
		t.Errorf("entry[2]: got %+v, want user-config no-prio", got[2])
	}
}

func TestConfigPaths_NoPriority_DiscoveryOrder(t *testing.T) {
	type cfg struct {
		A string `kongfig:"a,config-path"`
		B string `kongfig:"b,config-path"`
		C string `kongfig:"c,config-path"`
	}
	got := schema.ConfigPaths[cfg]()
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	if got[0].Key != "a" || got[1].Key != "b" || got[2].Key != "c" {
		t.Errorf("wrong discovery order: got %v %v %v", got[0].Key, got[1].Key, got[2].Key)
	}
}

func TestConfigPaths_NonStringIgnored(t *testing.T) {
	type cfg struct {
		Path   string `kongfig:"path,config-path=0"`
		NotStr int    `kongfig:"notstr,config-path=0"`
	}
	got := schema.ConfigPaths[cfg]()
	if len(got) != 1 || got[0].Key != "path" {
		t.Errorf("non-string field should be skipped; got %+v", got)
	}
}

func TestConfigPaths_NoTagNoEntry(t *testing.T) {
	type cfg struct {
		Host string `kongfig:"host"`
		Port int    `kongfig:"port"`
	}
	got := schema.ConfigPaths[cfg]()
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestNewFor_ConfigPaths(t *testing.T) {
	type cfg struct {
		ConfigFile string `kongfig:"config-file,config-path"`
	}
	kf := kongfig.NewFor[cfg]()
	entries := kf.ConfigPaths()
	if len(entries) != 1 || entries[0].Key != "config-file" {
		t.Errorf("expected config-file entry, got %+v", entries)
	}
}

func TestKongfig_ConfigPaths_NoRegistration(t *testing.T) {
	kf := kongfig.New()
	if kf.ConfigPaths() != nil {
		t.Error("expected nil for Kongfig with no registered config paths")
	}
}

func TestLoadConfigPaths_Integration(t *testing.T) {
	// Verify that MustLoadConfigPaths is a no-op when keys are absent.
	type cfg struct {
		ConfigFile string `kongfig:"config-file,config-path"`
	}
	kf := kongfig.NewFor[cfg]()
	kf.MustLoad(context.Background(), &staticProvider{
		data:   map[string]any{"host": "localhost"},
		source: "defaults",
	})
	// config-file is not set → MustLoadConfigPaths should not panic
	// (requires fileprovider, so just verify the entries are returned correctly
	// and the kf.ConfigPaths() API works as expected)
	entries := kf.ConfigPaths()
	if len(entries) != 1 {
		t.Fatalf("expected 1 config path entry, got %d", len(entries))
	}
	if entries[0].Key != "config-file" {
		t.Errorf("expected key=config-file, got %q", entries[0].Key)
	}
}
