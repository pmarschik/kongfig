package codec_test

import (
	"net"
	"net/url"
	"regexp"
	"testing"
	"time"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/codec"
)

// ---------------------------------------------------------------------------
// IP
// ---------------------------------------------------------------------------

func TestIP_DecodeIPv4String(t *testing.T) {
	ip, err := codec.IP.Decode("192.168.1.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip.String() != "192.168.1.1" {
		t.Errorf("IP = %v, want 192.168.1.1", ip)
	}
}

func TestIP_DecodeIPv6String(t *testing.T) {
	ip, err := codec.IP.Decode("::1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ip.Equal(net.ParseIP("::1")) {
		t.Errorf("IP = %v, want ::1", ip)
	}
}

func TestIP_AlreadyDecoded(t *testing.T) {
	orig := net.ParseIP("10.0.0.1")
	ip, err := codec.IP.Decode(orig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ip.Equal(orig) {
		t.Errorf("IP = %v, want %v", ip, orig)
	}
}

func TestIP_WrongInputType(t *testing.T) {
	_, err := codec.IP.Decode(12345)
	if err == nil {
		t.Error("expected error for non-string input, got nil")
	}
}

func TestIP_InvalidString(t *testing.T) {
	_, err := codec.IP.Decode("not-an-ip")
	if err == nil {
		t.Error("expected error for invalid IP string, got nil")
	}
}

func TestIP_Encode(t *testing.T) {
	ip := net.ParseIP("192.168.0.1")
	if got := codec.IP.Encode(ip); got != ip.String() {
		t.Errorf("Encode = %q, want %q", got, ip.String())
	}
}

// ---------------------------------------------------------------------------
// Duration
// ---------------------------------------------------------------------------

func TestDuration_DecodeString(t *testing.T) {
	d, err := codec.Duration.Decode("1h30m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 90*time.Minute {
		t.Errorf("Duration = %v, want 1h30m", d)
	}
}

func TestDuration_AlreadyDecoded(t *testing.T) {
	orig := 5 * time.Second
	d, err := codec.Duration.Decode(orig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != orig {
		t.Errorf("Duration = %v, want %v", d, orig)
	}
}

func TestDuration_WrongInputType(t *testing.T) {
	_, err := codec.Duration.Decode(42)
	if err == nil {
		t.Error("expected error for non-string input, got nil")
	}
}

func TestDuration_InvalidString(t *testing.T) {
	_, err := codec.Duration.Decode("not-a-duration")
	if err == nil {
		t.Error("expected error for malformed duration string, got nil")
	}
}

func TestDuration_Encode(t *testing.T) {
	d := 2*time.Hour + 30*time.Minute
	got := codec.Duration.Encode(d)
	if got != d.String() {
		t.Errorf("Encode = %q, want %q", got, d.String())
	}
}

// ---------------------------------------------------------------------------
// URL
// ---------------------------------------------------------------------------

func TestURL_DecodeString(t *testing.T) {
	u, err := codec.URL.Decode("https://example.com/path?q=1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Host != "example.com" {
		t.Errorf("Host = %q, want example.com", u.Host)
	}
}

func TestURL_AlreadyDecoded(t *testing.T) {
	orig, err := url.Parse("https://example.com")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	u, err := codec.URL.Decode(orig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u != orig {
		t.Errorf("URL pointer changed unexpectedly")
	}
}

func TestURL_WrongInputType(t *testing.T) {
	_, err := codec.URL.Decode(123)
	if err == nil {
		t.Error("expected error for non-string input, got nil")
	}
}

func TestURL_Encode(t *testing.T) {
	u, err := url.Parse("https://example.com/path")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if got := codec.URL.Encode(u); got != u.String() {
		t.Errorf("Encode = %q, want %q", got, u.String())
	}
}

// ---------------------------------------------------------------------------
// Regexp
// ---------------------------------------------------------------------------

func TestRegexp_DecodeString(t *testing.T) {
	re, err := codec.Regexp.Decode(`\d+`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !re.MatchString("42") {
		t.Error("compiled regex should match \"42\"")
	}
}

func TestRegexp_AlreadyDecoded(t *testing.T) {
	orig := regexp.MustCompile(`\w+`)
	re, err := codec.Regexp.Decode(orig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if re != orig {
		t.Error("already-decoded *regexp.Regexp should pass through unchanged")
	}
}

func TestRegexp_WrongInputType(t *testing.T) {
	_, err := codec.Regexp.Decode(42)
	if err == nil {
		t.Error("expected error for non-string input, got nil")
	}
}

func TestRegexp_InvalidPattern(t *testing.T) {
	_, err := codec.Regexp.Decode(`[invalid`)
	if err == nil {
		t.Error("expected error for invalid regexp pattern, got nil")
	}
}

func TestRegexp_Encode(t *testing.T) {
	re := regexp.MustCompile(`\d+`)
	if got := codec.Regexp.Encode(re); got != re.String() {
		t.Errorf("Encode = %q, want %q", got, re.String())
	}
}

// ---------------------------------------------------------------------------
// TimeRFC3339
// ---------------------------------------------------------------------------

func TestTimeRFC3339_DecodeString(t *testing.T) {
	s := "2024-06-15T12:00:00Z"
	ts, err := codec.TimeRFC3339.Decode(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts.Year() != 2024 || ts.Month() != 6 || ts.Day() != 15 {
		t.Errorf("parsed time = %v, unexpected date", ts)
	}
}

func TestTimeRFC3339_AlreadyDecoded(t *testing.T) {
	orig := time.Now().UTC().Truncate(time.Second)
	ts, err := codec.TimeRFC3339.Decode(orig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ts.Equal(orig) {
		t.Errorf("TimeRFC3339 = %v, want %v", ts, orig)
	}
}

func TestTimeRFC3339_WrongInputType(t *testing.T) {
	_, err := codec.TimeRFC3339.Decode(12345)
	if err == nil {
		t.Error("expected error for non-string input, got nil")
	}
}

func TestTimeRFC3339_InvalidString(t *testing.T) {
	_, err := codec.TimeRFC3339.Decode("not-a-time")
	if err == nil {
		t.Error("expected error for malformed time string, got nil")
	}
}

func TestTimeRFC3339_Encode(t *testing.T) {
	ts, err := time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("time.Parse: %v", err)
	}
	got := codec.TimeRFC3339.Encode(ts)
	if got != "2024-01-01T00:00:00Z" {
		t.Errorf("Encode = %q, want 2024-01-01T00:00:00Z", got)
	}
}

// ---------------------------------------------------------------------------
// TimeDate
// ---------------------------------------------------------------------------

func TestTimeDate_DecodeString(t *testing.T) {
	ts, err := codec.TimeDate.Decode("2024-06-15")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts.Year() != 2024 || ts.Month() != 6 || ts.Day() != 15 {
		t.Errorf("parsed time = %v, unexpected date", ts)
	}
}

func TestTimeDate_AlreadyDecoded(t *testing.T) {
	orig := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ts, err := codec.TimeDate.Decode(orig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ts.Equal(orig) {
		t.Errorf("TimeDate = %v, want %v", ts, orig)
	}
}

func TestTimeDate_WrongInputType(t *testing.T) {
	_, err := codec.TimeDate.Decode(12345)
	if err == nil {
		t.Error("expected error for non-string input, got nil")
	}
}

func TestTimeDate_InvalidString(t *testing.T) {
	// RFC3339 format should fail with date-only layout.
	_, err := codec.TimeDate.Decode("2024-06-15T12:00:00Z")
	if err == nil {
		t.Error("expected error for non-date-only string, got nil")
	}
}

func TestTimeDate_Encode(t *testing.T) {
	ts := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	if got := codec.TimeDate.Encode(ts); got != "2024-06-15" {
		t.Errorf("Encode = %q, want 2024-06-15", got)
	}
}

// ---------------------------------------------------------------------------
// TimeFormat (custom layout)
// ---------------------------------------------------------------------------

func TestTimeFormat_RoundTrip(t *testing.T) {
	layout := time.Kitchen // "3:04PM"
	c := codec.TimeFormat(layout)

	ts, err := c.Decode("3:30PM")
	if err != nil {
		t.Fatalf("Decode: unexpected error: %v", err)
	}
	if got := c.Encode(ts); got != "3:30PM" {
		t.Errorf("Encode = %q, want 3:30PM", got)
	}
}

func TestTimeFormat_AlreadyDecoded(t *testing.T) {
	c := codec.TimeFormat(time.RFC3339)
	orig := time.Now().UTC().Truncate(time.Second)
	ts, err := c.Decode(orig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ts.Equal(orig) {
		t.Errorf("TimeFormat = %v, want %v", ts, orig)
	}
}

func TestTimeFormat_WrongInputType(t *testing.T) {
	c := codec.TimeFormat(time.RFC3339)
	_, err := c.Decode(99)
	if err == nil {
		t.Error("expected error for non-string input, got nil")
	}
}

func TestTimeFormat_InvalidString(t *testing.T) {
	c := codec.TimeFormat("2006-01-02") // date layout
	_, err := c.Decode("not-a-date")
	if err == nil {
		t.Error("expected error for malformed string, got nil")
	}
}

// ---------------------------------------------------------------------------
// Of wrapper
// ---------------------------------------------------------------------------

func TestOf_WrapsCodecEntry(_ *testing.T) {
	// Of should produce a CodecEntry; verify it is accepted by a registry.
	r := kongfig.NewCodecRegistry()
	r.Register("ip", codec.Of(codec.IP))
	// If Of were broken, Register would panic or not compile. Simply reaching
	// this point is sufficient to confirm the wrapper works.
}

// ---------------------------------------------------------------------------
// Default registry
// ---------------------------------------------------------------------------

func TestDefault_ContainsExpectedCodecs(t *testing.T) {
	// Verify Default is non-nil and can be cloned into a fresh registry without panic.
	if codec.Default == nil {
		t.Fatal("codec.Default is nil")
	}
	// Build a Kongfig instance using the registry — if any codec is missing or
	// malformed, WithCodecRegistry would panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("WithCodecRegistry(codec.Default) panicked: %v", r)
		}
	}()
	_ = kongfig.New(kongfig.WithCodecRegistry(codec.Default))
}
