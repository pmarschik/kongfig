package kongfig_test

import (
	"testing"

	"github.com/pmarschik/kongfig/casing"
	"github.com/pmarschik/kongfig/schema"
)

func TestParseFieldTag_NameFallback(t *testing.T) {
	ft := schema.ParseFieldTag("", "MyField")
	if ft.Name != "my-field" {
		t.Errorf("Name = %q, want my-field (KebabCase default)", ft.Name)
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

func TestParseFieldTag_EmptyNameSegment(t *testing.T) {
	// kongfig:",required" — empty name before comma falls back to field name
	ft := schema.ParseFieldTag(",required", "Port")
	if ft.Name != "port" {
		t.Errorf("Name = %q, want port", ft.Name)
	}
	if len(ft.Extras) != 1 || ft.Extras[0] != "required" {
		t.Errorf("Extras = %v, want [required]", ft.Extras)
	}
}

func TestParseFieldTag_Skip(t *testing.T) {
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
		t.Error("expected Redacted=&true")
	}
	if len(ft.Extras) != 0 {
		t.Errorf("redacted must not appear in Extras, got %v", ft.Extras)
	}
}

func TestParseFieldTag_RedactedFalse(t *testing.T) {
	ft := schema.ParseFieldTag("host,redacted=false", "Host")
	if ft.Redacted == nil || *ft.Redacted {
		t.Error("expected Redacted=&false")
	}
	if len(ft.Extras) != 0 {
		t.Errorf("redacted=false must not appear in Extras, got %v", ft.Extras)
	}
}

func TestParseFieldTag_Extras(t *testing.T) {
	ft := schema.ParseFieldTag("port,required,min=1", "Port")
	if ft.Name != "port" {
		t.Errorf("Name = %q, want port", ft.Name)
	}
	if len(ft.Extras) != 2 {
		t.Fatalf("Extras = %v, want [required min=1]", ft.Extras)
	}
	if ft.Extras[0] != "required" || ft.Extras[1] != "min=1" {
		t.Errorf("Extras = %v, want [required min=1]", ft.Extras)
	}
}

func TestParseFieldTag_StructuralOptionsNotInExtras(t *testing.T) {
	// squash, redacted, redacted=false, default= must never appear in Extras.
	cases := []string{
		"host,squash",
		"host,redacted",
		"host,redacted=false",
		"host,squash,redacted",
		"host,default=localhost",
	}
	forbidden := map[string]bool{"squash": true, "redacted": true, "redacted=false": true}
	for _, tag := range cases {
		ft := schema.ParseFieldTag(tag, "F")
		for _, e := range ft.Extras {
			if forbidden[e] {
				t.Errorf("tag %q: structural option %q leaked into Extras", tag, e)
			}
		}
	}
}

func TestParseFieldTag_Default(t *testing.T) {
	ft := schema.ParseFieldTag("host,default=localhost", "Host")
	if ft.Name != "host" {
		t.Errorf("Name = %q, want host", ft.Name)
	}
	if ft.Default == nil || *ft.Default != "localhost" {
		t.Errorf("Default = %v, want &localhost", ft.Default)
	}
	if len(ft.Extras) != 0 {
		t.Errorf("default= must not appear in Extras, got %v", ft.Extras)
	}
}

func TestParseFieldTag_DefaultQuoted(t *testing.T) {
	// default=',' — comma inside quotes.
	ft := schema.ParseFieldTag("sep,default=','", "Sep")
	if ft.Default == nil || *ft.Default != "," {
		t.Errorf("Default = %v, want &','", ft.Default)
	}
	if len(ft.Extras) != 0 {
		t.Errorf("quoted default= must not appear in Extras, got %v", ft.Extras)
	}
}

func TestParseFieldTag_DefaultNoAnnotation(t *testing.T) {
	ft := schema.ParseFieldTag("host", "Host")
	if ft.Default != nil {
		t.Errorf("Default = %v, want nil for tag without default=", ft.Default)
	}
}

func TestParseFieldTag_Mixed(t *testing.T) {
	// Full combination: name + structural + validation extras.
	ft := schema.ParseFieldTag("api-key,redacted,required", "APIKey")
	if ft.Name != "api-key" {
		t.Errorf("Name = %q, want api-key", ft.Name)
	}
	if ft.Redacted == nil || !*ft.Redacted {
		t.Error("expected Redacted=&true")
	}
	if len(ft.Extras) != 1 || ft.Extras[0] != "required" {
		t.Errorf("Extras = %v, want [required]", ft.Extras)
	}
}

func TestKebabCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"LogLevel", "log-level"},
		{"DBConfig", "db-config"},
		{"APIKey", "api-key"},
		{"ID", "id"},
		{"Port", "port"},
		{"MyField", "my-field"},
		{"URL", "url"},
		{"HTTPSPort", "https-port"},
		{"V2", "v2"},
	}
	for _, c := range cases {
		got := casing.KebabCase(c.in)
		if got != c.want {
			t.Errorf("KebabCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseFieldTag_QuotedExtra(t *testing.T) {
	// sep=',' — comma inside quotes must not split the option.
	ft := schema.ParseFieldTag("tags,sep=','", "Tags")
	if ft.Name != "tags" {
		t.Errorf("Name = %q, want tags", ft.Name)
	}
	if len(ft.Extras) != 1 || ft.Extras[0] != "sep=','" {
		t.Errorf("Extras = %v, want [sep=',']", ft.Extras)
	}
}

func TestParseFieldTag_QuotedExtraWithFollowingOption(t *testing.T) {
	ft := schema.ParseFieldTag("tags,sep=',',redacted", "Tags")
	if len(ft.Extras) != 1 || ft.Extras[0] != "sep=','" {
		t.Errorf("Extras = %v, want [sep=',']", ft.Extras)
	}
	if ft.Redacted == nil || !*ft.Redacted {
		t.Error("expected Redacted=&true after quoted extra")
	}
}

func TestParseExtraValue(t *testing.T) {
	extras := []string{"sep=','", "required", "min=1"}

	val, ok := schema.ParseExtraValue(extras, "sep")
	if !ok || val != "," {
		t.Errorf("ParseExtraValue sep: got %q %v, want %q true", val, ok, ",")
	}

	val, ok = schema.ParseExtraValue(extras, "required")
	// "required" has no "=" so no match
	if ok {
		t.Errorf("ParseExtraValue required: unexpected match %q", val)
	}

	val, ok = schema.ParseExtraValue(extras, "min")
	if !ok || val != "1" {
		t.Errorf("ParseExtraValue min: got %q %v, want 1 true", val, ok)
	}

	_, ok = schema.ParseExtraValue(extras, "missing")
	if ok {
		t.Error("ParseExtraValue missing: expected not found")
	}
}

func TestParseExtraValue_MapSep(t *testing.T) {
	ft := schema.ParseFieldTag("labels,sep=',',kvsep='='", "Labels")
	sep, ok := schema.ParseExtraValue(ft.Extras, "sep")
	if !ok || sep != "," {
		t.Errorf("sep: got %q %v, want , true", sep, ok)
	}
	kvSep, ok := schema.ParseExtraValue(ft.Extras, "kvsep")
	if !ok || kvSep != "=" {
		t.Errorf("kvsep: got %q %v, want = true", kvSep, ok)
	}
}

func TestUpperKebab(t *testing.T) {
	cases := []struct{ in, want string }{
		{"LogLevel", "LOG-LEVEL"},
		{"APIKey", "API-KEY"},
		{"DBConfig", "DB-CONFIG"},
		{"Port", "PORT"},
	}
	for _, c := range cases {
		got := casing.UpperKebab(c.in)
		if got != c.want {
			t.Errorf("UpperKebab(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLowerKebab(t *testing.T) {
	// LowerKebab is an alias for KebabCase.
	cases := []struct{ in, want string }{
		{"LogLevel", "log-level"},
		{"APIKey", "api-key"},
	}
	for _, c := range cases {
		if got := casing.LowerKebab(c.in); got != c.want {
			t.Errorf("LowerKebab(%q) = %q, want %q", c.in, got, c.want)
		}
		if got := casing.KebabCase(c.in); got != c.want {
			t.Errorf("KebabCase(%q) = %q, want %q (alias check)", c.in, got, c.want)
		}
	}
}

func TestUpperSnake(t *testing.T) {
	cases := []struct{ in, want string }{
		{"LogLevel", "LOG_LEVEL"},
		{"APIKey", "API_KEY"},
		{"DBConfig", "DB_CONFIG"},
	}
	for _, c := range cases {
		got := casing.UpperSnake(c.in)
		if got != c.want {
			t.Errorf("UpperSnake(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLowerSnake(t *testing.T) {
	// LowerSnake is an alias for SnakeCase.
	cases := []struct{ in, want string }{
		{"LogLevel", "log_level"},
		{"APIKey", "api_key"},
	}
	for _, c := range cases {
		if got := casing.LowerSnake(c.in); got != c.want {
			t.Errorf("LowerSnake(%q) = %q, want %q", c.in, got, c.want)
		}
		if got := casing.SnakeCase(c.in); got != c.want {
			t.Errorf("SnakeCase(%q) = %q, want %q (alias check)", c.in, got, c.want)
		}
	}
}

func TestPascalCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"LogLevel", "LogLevel"},
		{"APIKey", "ApiKey"},
		{"DBConfig", "DbConfig"},
		{"Port", "Port"},
		{"ID", "Id"},
	}
	for _, c := range cases {
		got := casing.PascalCase(c.in)
		if got != c.want {
			t.Errorf("PascalCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCamelCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"LogLevel", "logLevel"},
		{"APIKey", "apiKey"},
		{"DBConfig", "dbConfig"},
		{"Port", "port"},
	}
	for _, c := range cases {
		got := casing.CamelCase(c.in)
		if got != c.want {
			t.Errorf("CamelCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAsIs(t *testing.T) {
	cases := []string{"LogLevel", "log-level", "LOG_LEVEL", "Port"}
	for _, s := range cases {
		if got := casing.AsIs(s); got != s {
			t.Errorf("AsIs(%q) = %q, want %q", s, got, s)
		}
	}
}
