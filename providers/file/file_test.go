package file_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	kongfig "github.com/pmarschik/kongfig"
	fileprovider "github.com/pmarschik/kongfig/providers/file"
)

// recordingStyler records SourceData calls for assertion.
type recordingStyler struct {
	sourceDataCalls []string
}

func (*recordingStyler) Key(s string) string           { return s }
func (*recordingStyler) String(s string) string        { return s }
func (*recordingStyler) Number(s string) string        { return s }
func (*recordingStyler) Bool(s string) string          { return s }
func (*recordingStyler) Null(s string) string          { return s }
func (*recordingStyler) Syntax(s string) string        { return s }
func (*recordingStyler) Comment(s string) string       { return s }
func (*recordingStyler) Annotation(_, s string) string { return s }
func (*recordingStyler) SourceKind(s string) string    { return s }
func (r *recordingStyler) SourceData(s string) string {
	r.sourceDataCalls = append(r.sourceDataCalls, s)
	return s
}
func (*recordingStyler) SourceKey(s string) string { return s }
func (*recordingStyler) Redacted(s string) string  { return s }
func (*recordingStyler) Codec(s string) string     { return s }

func TestSourceDataRenderAnnotation_PathOnly(t *testing.T) {
	s := &recordingStyler{}
	d := fileprovider.SourceData{Path: "/etc/app/config.yaml"}
	got := d.RenderAnnotation(context.Background(), s, "")
	if got != "/etc/app/config.yaml" {
		t.Errorf("got %q, want %q", got, "/etc/app/config.yaml")
	}
	if len(s.sourceDataCalls) != 1 || s.sourceDataCalls[0] != "/etc/app/config.yaml" {
		t.Errorf("SourceData calls: %v", s.sourceDataCalls)
	}
}

func TestSourceDataRenderAnnotation_DisplayPath(t *testing.T) {
	s := &recordingStyler{}
	d := fileprovider.SourceData{Path: "/home/user/.config/app/config.yaml", DisplayPath: "$XDG_CONFIG_HOME/app/config.yaml"}
	got := d.RenderAnnotation(context.Background(), s, "")
	if got != "$XDG_CONFIG_HOME/app/config.yaml" {
		t.Errorf("got %q, want %q", got, "$XDG_CONFIG_HOME/app/config.yaml")
	}
	if len(s.sourceDataCalls) != 1 || s.sourceDataCalls[0] != "$XDG_CONFIG_HOME/app/config.yaml" {
		t.Errorf("SourceData calls: %v", s.sourceDataCalls)
	}
}

func TestSourceDataRenderAnnotation_FileRawPathsKey_True(t *testing.T) {
	s := &recordingStyler{}
	d := fileprovider.SourceData{Path: "/home/user/.config/app/config.yaml", DisplayPath: "$XDG_CONFIG_HOME/app/config.yaml"}
	ctx := kongfig.WithRenderFileRawPathsCtx(context.Background())
	got := d.RenderAnnotation(ctx, s, "")
	if got != "/home/user/.config/app/config.yaml" {
		t.Errorf("got %q, want %q", got, "/home/user/.config/app/config.yaml")
	}
	if len(s.sourceDataCalls) != 1 || s.sourceDataCalls[0] != "/home/user/.config/app/config.yaml" {
		t.Errorf("SourceData calls: %v", s.sourceDataCalls)
	}
}

func TestSourceDataRenderAnnotation_FileRawPathsKey_False(t *testing.T) {
	s := &recordingStyler{}
	d := fileprovider.SourceData{Path: "/home/user/.config/app/config.yaml", DisplayPath: "$XDG_CONFIG_HOME/app/config.yaml"}
	// Default context has fileRawPaths=false, so display path is used.
	got := d.RenderAnnotation(context.Background(), s, "")
	if got != "$XDG_CONFIG_HOME/app/config.yaml" {
		t.Errorf("got %q, want %q", got, "$XDG_CONFIG_HOME/app/config.yaml")
	}
}

// mockParser is a minimal key=value parser for tests.
type mockParser struct{}

func (mockParser) Unmarshal(b []byte) (kongfig.ConfigData, error) {
	out := make(kongfig.ConfigData)
	for _, line := range splitLines(string(b)) {
		if line == "" {
			continue
		}
		for i, c := range line {
			if c != ':' {
				continue
			}
			key := line[:i]
			val := ""
			if i+2 < len(line) {
				val = line[i+2:]
			}
			out[key] = val
			break
		}
	}
	return out, nil
}

func (mockParser) Marshal(data kongfig.ConfigData) ([]byte, error) {
	var b []byte
	for k, v := range data {
		b = append(b, []byte(k+": "+fmt.Sprintf("%v", v)+"\n")...)
	}
	return b, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.txt")
	if err := os.WriteFile(path, []byte("host: localhost\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := fileprovider.New(path, mockParser{})
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data["host"] != "localhost" {
		t.Errorf("host: got %v", data["host"])
	}
}

func TestLoadMissingFile(t *testing.T) {
	p := fileprovider.New("/nonexistent/path.yaml", mockParser{})
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal("expected no error for missing file, got:", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty map, got %v", data)
	}
}

func TestLoadBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.txt")
	content := []byte("host: localhost\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	p := fileprovider.New(path, mockParser{})
	b, err := p.LoadBytes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, content) {
		t.Errorf("bytes mismatch")
	}
}

func TestWatchFiresOnWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.txt")
	if err := os.WriteFile(path, []byte("host: before\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	w := fileprovider.NewWatcher(path, mockParser{})
	changed := make(chan kongfig.ConfigData, 1)
	go func() {
		watchErr := w.Watch(ctx, func(e kongfig.WatchEvent) {
			if ev, ok := e.(kongfig.WatchDataEvent); ok {
				changed <- ev.Data
			}
		})
		if watchErr != nil && !errors.Is(watchErr, context.DeadlineExceeded) && !errors.Is(watchErr, context.Canceled) {
			t.Errorf("Watch: %v", watchErr)
		}
	}()

	// Give the watcher time to start.
	time.Sleep(200 * time.Millisecond)

	if err := os.WriteFile(path, []byte("host: after\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case data := <-changed:
		if data["host"] != "after" {
			t.Errorf("host: got %v", data["host"])
		}
	case <-ctx.Done():
		t.Error("timeout: watcher did not fire")
	}
}

func TestWatchCancels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.txt")
	if err := os.WriteFile(path, []byte("host: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	w := fileprovider.NewWatcher(path, mockParser{})
	done := make(chan error, 1)
	go func() {
		done <- w.Watch(ctx, func(kongfig.WatchEvent) {})
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Watch did not return after cancel")
	}
}

// errorParser always returns a parse error, used to test Watch error path.
type errorParser struct{}

func (errorParser) Unmarshal(_ []byte) (kongfig.ConfigData, error) {
	return nil, errors.New("parse error: malformed input")
}

func (errorParser) Marshal(_ kongfig.ConfigData) ([]byte, error) {
	return nil, errors.New("marshal not supported")
}

func TestWatchWithParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.txt")
	if err := os.WriteFile(path, []byte("valid: start\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	w := fileprovider.NewWatcher(path, errorParser{})
	watchErr := make(chan kongfig.WatchEvent, 1)
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- w.Watch(ctx, func(e kongfig.WatchEvent) {
			select {
			case watchErr <- e:
			default:
			}
		})
	}()
	_ = watchDone // Watch error checked at test teardown via ctx cancellation

	// Give the watcher time to start.
	time.Sleep(200 * time.Millisecond)

	// Write malformed content (errorParser always fails).
	if err := os.WriteFile(path, []byte("host: after\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-watchErr:
		errEv, ok := ev.(kongfig.WatchErrorEvent)
		if !ok {
			t.Errorf("expected WatchErrorEvent, got %T", ev)
			return
		}
		if errEv.Err == nil {
			t.Error("expected non-nil error in WatchErrorEvent")
		}
	case <-ctx.Done():
		t.Error("timeout: watcher did not fire error event")
	}

	// Verify the watcher is still running after the error (doesn't crash).
	// Write again and expect another error event (not a dead channel).
	if err := os.WriteFile(path, []byte("another: write\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	select {
	case ev := <-watchErr:
		if _, ok := ev.(kongfig.WatchErrorEvent); !ok {
			t.Errorf("expected second WatchErrorEvent, got %T", ev)
		}
	case <-ctx2.Done():
		t.Error("timeout: watcher did not fire second error event — may have crashed")
	}
}

// namedParser wraps mockParser and advertises a format name and extension.
// This lets Discover() pass the right extension to the discoverer.
type namedParser struct {
	format string
	ext    string
}

func (p namedParser) Format() string       { return p.format }
func (p namedParser) Extensions() []string { return []string{p.ext} }
func (namedParser) Unmarshal(b []byte) (kongfig.ConfigData, error) {
	return mockParser{}.Unmarshal(b)
}

func (namedParser) Marshal(data kongfig.ConfigData) ([]byte, error) {
	return mockParser{}.Marshal(data)
}

// extDiscoverer returns the first path it finds among a set of candidate files.
type extDiscoverer struct {
	files map[string]string
	name  string
}

func (d *extDiscoverer) Name() string { return d.name }

func (d *extDiscoverer) Discover(_ context.Context, exts []string) (string, error) {
	for _, ext := range exts {
		if path, ok := d.files[ext]; ok {
			return path, nil
		}
	}
	return "", nil
}

func TestDiscoverMultipleParsers_FirstMatchWins(t *testing.T) {
	dir := t.TempDir()

	// Create only the ".second" file.
	secondFile := filepath.Join(dir, "config.second")
	if err := os.WriteFile(secondFile, []byte("host: found\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Discoverer only returns a path for ".second"; ".first" is absent.
	d := &extDiscoverer{
		name:  "test",
		files: map[string]string{".second": secondFile},
	}

	firstParser := namedParser{format: "first", ext: ".first"}
	secondParser := namedParser{format: "second", ext: ".second"}

	p, err := fileprovider.Discover(context.Background(), d, firstParser, secondParser)
	if err != nil {
		t.Fatal(err)
	}

	// firstParser was tried first and got "" from discoverer; secondParser matched.
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data["host"] != "found" {
		t.Errorf("host: got %v, want found", data["host"])
	}
	if info := p.ProviderInfo(); info.Name != "test.second" {
		t.Errorf("source: got %q, want test.second", info.Name)
	}
}

func TestDiscoverMultipleParsers_SecondParserIgnoredWhenFirstFinds(t *testing.T) {
	dir := t.TempDir()

	firstFile := filepath.Join(dir, "config.first")
	secondFile := filepath.Join(dir, "config.second")
	if err := os.WriteFile(firstFile, []byte("host: first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondFile, []byte("host: second\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Discoverer returns paths for both extensions.
	d := &extDiscoverer{
		name:  "test",
		files: map[string]string{".first": firstFile, ".second": secondFile},
	}

	firstParser := namedParser{format: "first", ext: ".first"}
	secondParser := namedParser{format: "second", ext: ".second"}

	p, err := fileprovider.Discover(context.Background(), d, firstParser, secondParser)
	if err != nil {
		t.Fatal(err)
	}

	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// firstParser is tried first and finds a file → secondParser is not tried.
	if data["host"] != "first" {
		t.Errorf("host: got %v, want first", data["host"])
	}
	if info := p.ProviderInfo(); info.Name != "test.first" {
		t.Errorf("source: got %q, want test.first", info.Name)
	}
}

// failingDiscoverer always returns an error from Discover.
type failingDiscoverer struct{}

func (*failingDiscoverer) Name() string { return "fail" }
func (*failingDiscoverer) Discover(_ context.Context, _ []string) (string, error) {
	return "", errors.New("discover: internal failure")
}

func TestMustLoadDiscovered_PanicsOnDiscoverError(t *testing.T) {
	kf := kongfig.New()
	d := &failingDiscoverer{}

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when discoverer returns error, got none")
		}
	}()
	fileprovider.MustLoadDiscovered(context.Background(), kf, d, []kongfig.Parser{mockParser{}})
}
