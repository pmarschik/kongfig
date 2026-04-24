package kongfig_test

import (
	"fmt"
	"net"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

type flatConfig struct {
	Host  string `kongfig:"host"`
	Port  int    `kongfig:"port"`
	Debug bool   `kongfig:"debug"`
}

type nestedConfig struct {
	LogLevel string `kongfig:"log-level"`
	DB       struct {
		Host string `kongfig:"host"`
		Port int    `kongfig:"port"`
	} `kongfig:"db"`
}

func TestGetFlat(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{
		"host":  "localhost",
		"port":  8080,
		"debug": true,
	}, source: "test"})

	cfg, err := kongfig.Get[flatConfig](k)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "localhost" {
		t.Errorf("Host: got %q", cfg.Host)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port: got %d", cfg.Port)
	}
	if !cfg.Debug {
		t.Error("Debug: expected true")
	}
}

func TestGetNested(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{
		"db":        map[string]any{"host": "dbhost", "port": 5432},
		"log-level": "info",
	}, source: "test"})

	cfg, err := kongfig.Get[nestedConfig](k)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Host != "dbhost" {
		t.Errorf("DB.Host: got %q", cfg.DB.Host)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel: got %q", cfg.LogLevel)
	}
}

func TestGetAt(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{
		"db": map[string]any{"host": "dbhost", "port": 5432},
	}, source: "test"})

	type dbConfig struct {
		Host string `kongfig:"host"`
		Port int    `kongfig:"port"`
	}
	cfg, err := kongfig.Get[dbConfig](k, kongfig.At("db"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "dbhost" {
		t.Errorf("Host: got %q", cfg.Host)
	}
}

func TestGetStrictFails(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{
		"host":    "localhost",
		"unknown": "value",
	}, source: "test"})

	_, err := kongfig.Get[flatConfig](k, kongfig.Strict())
	if err == nil {
		t.Error("expected error for unknown key with Strict()")
	}
}

func TestGetWithProvenance(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"host": "localhost"}, source: "defaults"})
	mustLoad(t, k, &staticProvider{data: map[string]any{"host": "prod"}, source: "file"})

	wp, err := kongfig.GetWithProvenance[flatConfig](k)
	if err != nil {
		t.Fatal(err)
	}
	if wp.Value.Host != "prod" {
		t.Errorf("Host: got %q", wp.Value.Host)
	}
	if wp.Prov.SourceMetas()["host"].Layer.Name != "file" {
		t.Errorf("provenance host: got %q", wp.Prov.SourceMetas()["host"].Layer.Name)
	}
}

func TestTypedDecodeHook(t *testing.T) {
	ipCodec := kongfig.Codec[net.IP]{
		Decode: func(v any) (net.IP, error) {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("expected string, got %T", v)
			}
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP: %s", s)
			}
			return ip, nil
		},
	}
	k := kongfig.New()
	k.RegisterCodec("host", kongfig.Of(ipCodec))
	mustLoad(t, k, &staticProvider{data: map[string]any{
		"host": "192.168.1.1",
	}, source: "test"})

	type ipConfig struct {
		Host net.IP `kongfig:"host"`
	}
	cfg, err := kongfig.Get[ipConfig](k)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Host.Equal(net.ParseIP("192.168.1.1")) {
		t.Errorf("Host: got %v", cfg.Host)
	}
}

func TestTypedDecodeHookError(t *testing.T) {
	ipCodec := kongfig.Codec[net.IP]{
		Decode: func(v any) (net.IP, error) {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("expected string, got %T", v)
			}
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP: %s", s)
			}
			return ip, nil
		},
	}
	k := kongfig.New()
	k.RegisterCodec("host", kongfig.Of(ipCodec))
	mustLoad(t, k, &staticProvider{data: map[string]any{
		"host": "not-an-ip",
	}, source: "test"})

	type ipConfig struct {
		Host net.IP `kongfig:"host"`
	}
	_, err := kongfig.Get[ipConfig](k)
	if err == nil {
		t.Error("expected error for invalid IP")
	}
}

func TestGetFlatDotPath(t *testing.T) {
	// Flat key "ui.theme" (from env provider) should decode into nested struct.
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{
		"ui.theme": "dark",
	}, source: "env"})

	type uiConfig struct {
		UI struct {
			Theme string `kongfig:"theme"`
		} `kongfig:"ui"`
	}
	cfg, err := kongfig.Get[uiConfig](k)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Theme != "dark" {
		t.Errorf("UI.Theme: got %q", cfg.UI.Theme)
	}
}
