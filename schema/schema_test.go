package schema_test

import (
	"net"
	"reflect"
	"testing"

	"github.com/pmarschik/kongfig/casing"
	"github.com/pmarschik/kongfig/schema"
)

// ---------------------------------------------------------------------------
// ParseFieldTag
// ---------------------------------------------------------------------------

func TestParseFieldTag_EmptyTagFallsBackToKebab(t *testing.T) {
	ft := schema.ParseFieldTag("", "MyField")
	if ft.Name != "my-field" {
		t.Errorf("Name = %q, want my-field", ft.Name)
	}
	if ft.Skip || ft.Squash || ft.Redacted != nil || len(ft.Extras) != 0 {
		t.Errorf("unexpected flags: %+v", ft)
	}
}

func TestParseFieldTag_ExplicitName(t *testing.T) {
	ft := schema.ParseFieldTag("host", "MyField")
	if ft.Name != "host" {
		t.Errorf("Name = %q, want host", ft.Name)
	}
}

func TestParseFieldTag_EmptyNameSegmentFallsBack(t *testing.T) {
	// ",required" — empty name before the comma → field name fallback
	ft := schema.ParseFieldTag(",required", "Port")
	if ft.Name != "port" {
		t.Errorf("Name = %q, want port", ft.Name)
	}
	if len(ft.Extras) != 1 || ft.Extras[0] != "required" {
		t.Errorf("Extras = %v, want [required]", ft.Extras)
	}
}

func TestParseFieldTag_DashSkip(t *testing.T) {
	ft := schema.ParseFieldTag("-", "MyField")
	if !ft.Skip {
		t.Error("expected Skip=true for tag \"-\"")
	}
}

func TestParseFieldTag_Squash(t *testing.T) {
	ft := schema.ParseFieldTag(",squash", "Inner")
	if !ft.Squash {
		t.Error("expected Squash=true")
	}
	if len(ft.Extras) != 0 {
		t.Errorf("squash must not appear in Extras, got %v", ft.Extras)
	}
}

func TestParseFieldTag_Redacted(t *testing.T) {
	ft := schema.ParseFieldTag("password,redacted", "Password")
	if ft.Redacted == nil || !*ft.Redacted {
		t.Errorf("Redacted = %v, want &true", ft.Redacted)
	}
	if len(ft.Extras) != 0 {
		t.Errorf("redacted must not appear in Extras, got %v", ft.Extras)
	}
}

func TestParseFieldTag_RedactedFalse(t *testing.T) {
	ft := schema.ParseFieldTag("host,redacted=false", "Host")
	if ft.Redacted == nil || *ft.Redacted {
		t.Errorf("Redacted = %v, want &false", ft.Redacted)
	}
}

func TestParseFieldTag_DefaultValue(t *testing.T) {
	ft := schema.ParseFieldTag("host,default=localhost", "Host")
	if ft.Default == nil || *ft.Default != "localhost" {
		t.Errorf("Default = %v, want &localhost", ft.Default)
	}
	if len(ft.Extras) != 0 {
		t.Errorf("default= must not appear in Extras, got %v", ft.Extras)
	}
}

func TestParseFieldTag_DefaultQuotedComma(t *testing.T) {
	ft := schema.ParseFieldTag("sep,default=','", "Sep")
	if ft.Default == nil || *ft.Default != "," {
		t.Errorf("Default = %v, want &\",\"", ft.Default)
	}
	if len(ft.Extras) != 0 {
		t.Errorf("quoted default= must not appear in Extras, got %v", ft.Extras)
	}
}

func TestParseFieldTag_NoDefaultAnnotation(t *testing.T) {
	ft := schema.ParseFieldTag("host", "Host")
	if ft.Default != nil {
		t.Errorf("Default = %v, want nil when no default= tag is present", ft.Default)
	}
}

func TestParseFieldTag_CodecOption(t *testing.T) {
	ft := schema.ParseFieldTag("addr,codec=ip", "Addr")
	if ft.Codec != "ip" {
		t.Errorf("Codec = %q, want ip", ft.Codec)
	}
	if len(ft.Extras) != 0 {
		t.Errorf("codec= must not appear in Extras, got %v", ft.Extras)
	}
}

func TestParseFieldTag_ConfigPath(t *testing.T) {
	ft := schema.ParseFieldTag("cfg-path,config-path", "CfgPath")
	if !ft.IsConfigPath {
		t.Error("expected IsConfigPath=true")
	}
	if ft.ConfigPathPriority != nil {
		t.Errorf("ConfigPathPriority = %v, want nil", ft.ConfigPathPriority)
	}
}

func TestParseFieldTag_ConfigPathWithPriority(t *testing.T) {
	ft := schema.ParseFieldTag("cfg-path,config-path=2", "CfgPath")
	if !ft.IsConfigPath {
		t.Error("expected IsConfigPath=true")
	}
	if ft.ConfigPathPriority == nil || *ft.ConfigPathPriority != 2 {
		t.Errorf("ConfigPathPriority = %v, want &2", ft.ConfigPathPriority)
	}
}

func TestParseFieldTag_QuotedExtra(t *testing.T) {
	// sep=',' — comma inside quotes must not split the option segment.
	ft := schema.ParseFieldTag("tags,sep=','", "Tags")
	if ft.Name != "tags" {
		t.Errorf("Name = %q, want tags", ft.Name)
	}
	if len(ft.Extras) != 1 || ft.Extras[0] != "sep=','" {
		t.Errorf("Extras = %v, want [sep=',']", ft.Extras)
	}
}

func TestParseFieldTag_StructuralOptionsNeverInExtras(t *testing.T) {
	forbidden := map[string]bool{"squash": true, "redacted": true, "redacted=false": true}
	cases := []string{
		"host,squash",
		"host,redacted",
		"host,redacted=false",
		"host,squash,redacted",
		"host,default=localhost",
	}
	for _, tag := range cases {
		ft := schema.ParseFieldTag(tag, "F")
		for _, e := range ft.Extras {
			if forbidden[e] {
				t.Errorf("tag %q: structural option %q leaked into Extras", tag, e)
			}
		}
	}
}

func TestParseFieldTag_DotPathName(t *testing.T) {
	// A dot in the tag value is a path separator — each segment is validated.
	ft := schema.ParseFieldTag("db.host", "DBHost")
	if ft.Name != "db.host" {
		t.Errorf("Name = %q, want db.host", ft.Name)
	}
}

func TestParseFieldTag_CustomNameMapper(t *testing.T) {
	orig := schema.DefaultNameMapper
	defer func() { schema.DefaultNameMapper = orig }()

	schema.DefaultNameMapper = casing.UpperSnake
	ft := schema.ParseFieldTag("", "MyField")
	if ft.Name != "MY_FIELD" {
		t.Errorf("Name = %q, want MY_FIELD with UpperSnake mapper", ft.Name)
	}
}

// ---------------------------------------------------------------------------
// ValidateKeyName
// ---------------------------------------------------------------------------

func TestValidateKeyName_Valid(t *testing.T) {
	valid := []string{"host", "log-level", "api_key", "v2", "my-config", "123", "a"}
	for _, name := range valid {
		if err := schema.ValidateKeyName(name); err != nil {
			t.Errorf("ValidateKeyName(%q) returned unexpected error: %v", name, err)
		}
	}
}

func TestValidateKeyName_ReservedBrackets(t *testing.T) {
	for _, name := range []string{"foo[0]", "foo[", "]bar", "[", "]"} {
		if err := schema.ValidateKeyName(name); err == nil {
			t.Errorf("ValidateKeyName(%q) = nil, want error (reserved bracket chars)", name)
		}
	}
}

func TestValidateKeyName_DotDisallowed(t *testing.T) {
	// Dots in a name segment are disallowed; they are path separators.
	if err := schema.ValidateKeyName("foo.bar"); err == nil {
		t.Error("ValidateKeyName(\"foo.bar\") = nil, want error")
	}
}

func TestValidateKeyName_EmptyString(t *testing.T) {
	// An empty name has no invalid characters, so it should pass validation.
	if err := schema.ValidateKeyName(""); err != nil {
		t.Errorf("ValidateKeyName(\"\") = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// ParseExtraValue
// ---------------------------------------------------------------------------

func TestParseExtraValue(t *testing.T) {
	extras := []string{"sep=','", "required", "min=1"}

	val, ok := schema.ParseExtraValue(extras, "sep")
	if !ok || val != "," {
		t.Errorf("sep: got (%q, %v), want (\",\", true)", val, ok)
	}

	// "required" has no "=" so it cannot be matched as key=value.
	_, ok = schema.ParseExtraValue(extras, "required")
	if ok {
		t.Error("required: expected not found (no '=')")
	}

	val, ok = schema.ParseExtraValue(extras, "min")
	if !ok || val != "1" {
		t.Errorf("min: got (%q, %v), want (\"1\", true)", val, ok)
	}

	_, ok = schema.ParseExtraValue(extras, "missing")
	if ok {
		t.Error("missing: expected not found")
	}
}

func TestParseExtraValue_EmptyExtras(t *testing.T) {
	_, ok := schema.ParseExtraValue(nil, "sep")
	if ok {
		t.Error("nil extras: expected not found")
	}
}

// ---------------------------------------------------------------------------
// RedactedPaths
// ---------------------------------------------------------------------------

type redactedTop struct {
	Public   string `kongfig:"public"`
	Secret   string `kongfig:"secret,redacted"`
	Untagged string
}

func TestRedactedPaths_Simple(t *testing.T) {
	got := schema.RedactedPaths[redactedTop]()
	if got["secret"] != true {
		t.Error("expected secret to be redacted")
	}
	if got["public"] {
		t.Error("expected public to NOT be redacted")
	}
}

type sensitiveInner struct {
	Host     string `kongfig:"host,redacted=false"` // overrides parent
	Password string `kongfig:"password"`            // inherits redacted
}

type redactedNested struct {
	DB sensitiveInner `kongfig:"db,redacted"`
}

func TestRedactedPaths_InheritedAndOverridden(t *testing.T) {
	got := schema.RedactedPaths[redactedNested]()
	if got["db.password"] != true {
		t.Errorf("db.password should be redacted (inherited); got %v", got)
	}
	if got["db.host"] {
		t.Errorf("db.host should NOT be redacted (overridden with redacted=false); got %v", got)
	}
}

type skipStruct struct {
	Visible string `kongfig:"visible"`
	Hidden  string `kongfig:"-"`
}

func TestRedactedPaths_SkippedFieldsIgnored(t *testing.T) {
	got := schema.RedactedPaths[skipStruct]()
	if _, exists := got["hidden"]; exists {
		t.Error("skipped field should not appear in RedactedPaths")
	}
}

// ---------------------------------------------------------------------------
// SplitPaths
// ---------------------------------------------------------------------------

type splitConfig struct {
	Plain  string   `kongfig:"plain"`
	Tags   []string `kongfig:"tags,sep=','"`
	Labels []string `kongfig:"labels,sep=';'"`
}

func TestSplitPaths_SliceFields(t *testing.T) {
	got := schema.SplitPaths[splitConfig]()
	if got["tags"] != "," {
		t.Errorf("tags sep = %q, want \",\"", got["tags"])
	}
	if got["labels"] != ";" {
		t.Errorf("labels sep = %q, want \";\"", got["labels"])
	}
	if _, ok := got["plain"]; ok {
		t.Error("plain (non-slice) should not appear in SplitPaths")
	}
}

type noSplitConfig struct {
	Name string `kongfig:"name"`
}

func TestSplitPaths_NilWhenNone(t *testing.T) {
	got := schema.SplitPaths[noSplitConfig]()
	if got != nil {
		t.Errorf("SplitPaths = %v, want nil when no split fields exist", got)
	}
}

// ---------------------------------------------------------------------------
// MapSplitPaths
// ---------------------------------------------------------------------------

type mapSplitConfig struct {
	Labels map[string]string `kongfig:"labels,sep=',',kvsep='='"`
	Other  string            `kongfig:"other"`
}

func TestMapSplitPaths(t *testing.T) {
	got := schema.MapSplitPaths[mapSplitConfig]()
	spec, ok := got["labels"]
	if !ok {
		t.Fatal("labels not found in MapSplitPaths")
	}
	if spec.Sep != "," {
		t.Errorf("labels Sep = %q, want \",\"", spec.Sep)
	}
	if spec.KVSep != "=" {
		t.Errorf("labels KVSep = %q, want \"=\"", spec.KVSep)
	}
	if _, ok := got["other"]; ok {
		t.Error("other (non-map) should not appear in MapSplitPaths")
	}
}

type noMapSplitConfig struct {
	Name string `kongfig:"name"`
}

func TestMapSplitPaths_NilWhenNone(t *testing.T) {
	got := schema.MapSplitPaths[noMapSplitConfig]()
	if got != nil {
		t.Errorf("MapSplitPaths = %v, want nil when no map-split fields exist", got)
	}
}

// ---------------------------------------------------------------------------
// ConfigPaths
// ---------------------------------------------------------------------------

type configPathConfig struct {
	MainCfg  string `kongfig:"main-cfg,config-path=0"`
	ExtraCfg string `kongfig:"extra-cfg,config-path=2"`
	Override string `kongfig:"override,config-path"`
	Name     string `kongfig:"name"`
}

func TestConfigPaths_Sorting(t *testing.T) {
	got := schema.ConfigPaths[configPathConfig]()
	// Expect: [main-cfg(priority=0), extra-cfg(priority=2), override(no priority)]
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3; got %v", len(got), got)
	}
	if got[0].Key != "main-cfg" || !got[0].HasPriority || got[0].Priority != 0 {
		t.Errorf("[0] = %+v, want {Key:main-cfg Priority:0 HasPriority:true}", got[0])
	}
	if got[1].Key != "extra-cfg" || !got[1].HasPriority || got[1].Priority != 2 {
		t.Errorf("[1] = %+v, want {Key:extra-cfg Priority:2 HasPriority:true}", got[1])
	}
	if got[2].Key != "override" || got[2].HasPriority {
		t.Errorf("[2] = %+v, want {Key:override HasPriority:false}", got[2])
	}
}

func TestConfigPaths_NonStringFieldsIgnored(t *testing.T) {
	type mixedConfig struct {
		Path string `kongfig:"path,config-path"`
		Port int    `kongfig:"port,config-path"` // int — must be ignored
	}
	got := schema.ConfigPaths[mixedConfig]()
	if len(got) != 1 || got[0].Key != "path" {
		t.Errorf("got %v, want only [{Key:path}]", got)
	}
}

func TestConfigPaths_NilWhenNone(t *testing.T) {
	type simple struct {
		Name string `kongfig:"name"`
	}
	if got := schema.ConfigPaths[simple](); got != nil {
		t.Errorf("ConfigPaths = %v, want nil", got)
	}
}

// ---------------------------------------------------------------------------
// CodecPaths
// ---------------------------------------------------------------------------

func TestCodecPaths_NonPrimitiveFieldsIncluded(t *testing.T) {
	type codecConfig struct {
		Name    string `kongfig:"name"`
		Addr    net.IP `kongfig:"addr"`
		Timeout int    `kongfig:"timeout"`
	}
	got := schema.CodecPaths[codecConfig]()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1; got %v", len(got), got)
	}
	if got[0].Path != "addr" {
		t.Errorf("Path = %q, want addr", got[0].Path)
	}
	if got[0].GoType != reflect.TypeFor[net.IP]() {
		t.Errorf("GoType = %v, want net.IP", got[0].GoType)
	}
}

func TestCodecPaths_ExplicitCodecTagOverrides(t *testing.T) {
	type explicitCodec struct {
		Created string `kongfig:"created,codec=time-rfc3339"` // string but has explicit codec=
	}
	got := schema.CodecPaths[explicitCodec]()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1; got %v", len(got), got)
	}
	if got[0].CodecName != "time-rfc3339" {
		t.Errorf("CodecName = %q, want time-rfc3339", got[0].CodecName)
	}
}

func TestCodecPaths_EmptyForAllPrimitives(t *testing.T) {
	type primitiveOnly struct {
		Name    string  `kongfig:"name"`
		Port    int     `kongfig:"port"`
		Enabled bool    `kongfig:"enabled"`
		Weight  float64 `kongfig:"weight"`
	}
	got := schema.CodecPaths[primitiveOnly]()
	if len(got) != 0 {
		t.Errorf("CodecPaths = %v, want empty for all-primitive struct", got)
	}
}
