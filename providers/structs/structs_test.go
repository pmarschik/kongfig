package structs_test

import (
	"context"
	"os"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	structsprovider "github.com/pmarschik/kongfig/providers/structs"
)

type serverConfig struct {
	Host     string `env:"TEST_HOST"      kongfig:"host"`
	LogLevel string `env:"TEST_LOG_LEVEL" kongfig:"log-level"`
	Port     int    `kongfig:"port"`
}

type appConfig struct {
	Server serverConfig `kongfig:"server"`
	Debug  bool         `kongfig:"debug"`
}

type embeddedBase struct {
	Host string `kongfig:"host"`
}

type embeddedConfig struct {
	embeddedBase
	Port int `kongfig:"port"`
}

func TestDefaults(t *testing.T) {
	cfg := serverConfig{Host: "prod.example.com", Port: 9090}
	p := structsprovider.Defaults(cfg)
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data["host"] != "prod.example.com" {
		t.Errorf("host: got %v", data["host"])
	}
	if data["port"] != 9090 {
		t.Errorf("port: got %v", data["port"])
	}
	// Zero value string omitted.
	if _, ok := data["log-level"]; ok {
		t.Error("log-level should be omitted (zero value)")
	}
}

func TestDefaultsNested(t *testing.T) {
	cfg := appConfig{
		Server: serverConfig{Host: "localhost", Port: 8080},
		Debug:  true,
	}
	p := structsprovider.Defaults(cfg)
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	server, ok := data["server"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("server not a map: %T", data["server"])
	}
	if server["host"] != "localhost" {
		t.Errorf("server.host: got %v", server["host"])
	}
}

// serverConfigWithDefaults exercises the default= tag annotation.
type serverConfigWithDefaults struct {
	Host     string `kongfig:"host,default=localhost"`
	Port     string `kongfig:"port,default=8080"`
	LogLevel string `kongfig:"log-level"` // no default= — omitted
}

type appConfigWithDefaults struct {
	Server serverConfigWithDefaults `kongfig:"server"`
	Debug  bool                     `kongfig:"debug,default=true"`
}

func TestTagDefaults(t *testing.T) {
	// Fields with no default= annotation: empty result.
	p := structsprovider.TagDefaults[serverConfig]()
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty map for struct with no default= tags, got %v", data)
	}
}

func TestTagDefaultsAnnotations(t *testing.T) {
	p := structsprovider.TagDefaults[serverConfigWithDefaults]()
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data["host"] != "localhost" {
		t.Errorf("host: got %v, want localhost", data["host"])
	}
	if data["port"] != "8080" {
		t.Errorf("port: got %v, want 8080", data["port"])
	}
	if _, ok := data["log-level"]; ok {
		t.Error("log-level should be omitted (no default= annotation)")
	}
}

func TestTagDefaultsNested(t *testing.T) {
	p := structsprovider.TagDefaults[appConfigWithDefaults]()
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	server, ok := data["server"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("server not a map: %T", data["server"])
	}
	if server["host"] != "localhost" {
		t.Errorf("server.host: got %v, want localhost", server["host"])
	}
	// bool field with default=true comes through as a string; mapstructure handles conversion on Get.
	if data["debug"] != "true" {
		t.Errorf("debug: got %v, want true", data["debug"])
	}
}

func TestTagEnv(t *testing.T) {
	t.Setenv("TEST_HOST", "envhost")
	t.Setenv("TEST_LOG_LEVEL", "debug")

	p := structsprovider.TagEnv[serverConfig]()
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data["host"] != "envhost" {
		t.Errorf("host: got %v", data["host"])
	}
	if data["log-level"] != "debug" {
		t.Errorf("log-level: got %v", data["log-level"])
	}
	// port has no env tag — should not appear.
	if _, ok := data["port"]; ok {
		t.Error("port should not appear (no env tag)")
	}
}

func TestTagEnvMissing(t *testing.T) {
	os.Unsetenv("TEST_HOST")
	os.Unsetenv("TEST_LOG_LEVEL")

	p := structsprovider.TagEnv[serverConfig]()
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty map when env vars unset, got %v", data)
	}
}

func TestDefaultsEmbedded(t *testing.T) {
	cfg := embeddedConfig{embeddedBase: embeddedBase{Host: "embedded-host"}, Port: 1234}
	p := structsprovider.Defaults(cfg)
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Embedded fields squashed into top level.
	if data["host"] != "embedded-host" {
		t.Errorf("host: got %v", data["host"])
	}
	if data["port"] != 1234 {
		t.Errorf("port: got %v", data["port"])
	}
}

// --- Pointer-to-struct fields ---

type dbConfig struct {
	Host string `kongfig:"host"`
	Port int    `kongfig:"port"`
}

type appWithPtrSub struct {
	DB      *dbConfig `kongfig:"db"`
	Timeout int       `kongfig:"timeout"`
}

func TestDefaults_PointerToStruct_NonNil(t *testing.T) {
	cfg := appWithPtrSub{
		DB:      &dbConfig{Host: "db.local", Port: 5432},
		Timeout: 30,
	}
	p := structsprovider.Defaults(cfg)
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	db, ok := data["db"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("db not a map: %T", data["db"])
	}
	if db["host"] != "db.local" {
		t.Errorf("db.host: got %v", db["host"])
	}
	if db["port"] != 5432 {
		t.Errorf("db.port: got %v", db["port"])
	}
	if data["timeout"] != 30 {
		t.Errorf("timeout: got %v", data["timeout"])
	}
}

func TestDefaults_PointerToStruct_Nil(t *testing.T) {
	cfg := appWithPtrSub{Timeout: 10}
	// DB is nil pointer — should be skipped (not panic).
	p := structsprovider.Defaults(cfg)
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := data["db"]; ok {
		t.Error("db should be absent when pointer is nil")
	}
	if data["timeout"] != 10 {
		t.Errorf("timeout: got %v", data["timeout"])
	}
}

// --- TagEnv with nested struct ---

type nestedEnvInner struct {
	Password string `env:"TEST_DB_PASS" kongfig:"password"`
	Host     string `env:"TEST_DB_HOST" kongfig:"host"`
}

type nestedEnvOuter struct {
	DB    nestedEnvInner `kongfig:"db"`
	Level string         `env:"TEST_LOG_LEVEL" kongfig:"level"`
}

func TestTagEnv_NestedStruct(t *testing.T) {
	t.Setenv("TEST_DB_PASS", "secret")
	t.Setenv("TEST_DB_HOST", "db.prod")
	t.Setenv("TEST_LOG_LEVEL", "warn")

	p := structsprovider.TagEnv[nestedEnvOuter]()
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	db, ok := data["db"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("db not a map: %T", data["db"])
	}
	if db["password"] != "secret" {
		t.Errorf("db.password: got %v", db["password"])
	}
	if db["host"] != "db.prod" {
		t.Errorf("db.host: got %v", db["host"])
	}
	if data["level"] != "warn" {
		t.Errorf("level: got %v", data["level"])
	}
}

// --- Mixed embedded + explicit fields ---

type mixedBase struct {
	Region string `kongfig:"region"`
}

type mixedConfig struct {
	mixedBase
	Env  string `kongfig:"env"`
	Zone string `kongfig:"zone"`
}

func TestDefaults_MixedEmbeddedAndExplicit(t *testing.T) {
	cfg := mixedConfig{
		mixedBase: mixedBase{Region: "us-east-1"},
		Env:       "production",
		Zone:      "a",
	}
	p := structsprovider.Defaults(cfg)
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Embedded field squashed into top level.
	if data["region"] != "us-east-1" {
		t.Errorf("region: got %v", data["region"])
	}
	if data["env"] != "production" {
		t.Errorf("env: got %v", data["env"])
	}
	if data["zone"] != "a" {
		t.Errorf("zone: got %v", data["zone"])
	}
}
