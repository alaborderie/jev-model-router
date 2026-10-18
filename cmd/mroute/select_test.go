package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alaborderie/f-0923-jev-model-router/internal/selection"
)

const fixtureDir = "../../testdata/selection-v1/"

const secretMarker = "sk-SECRET-MARKER-7f3a"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(fixtureDir + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeClassifier answers without network access.
type fakeClassifier struct {
	calls int
	resp  selection.Response
	err   error
	block bool
	// entered, when non-nil, is closed once a blocking Choose has started.
	entered chan struct{}
}

func (f *fakeClassifier) Choose(ctx context.Context, req selection.Request) (selection.Response, error) {
	f.calls++
	if f.block {
		if f.entered != nil {
			close(f.entered)
		}
		<-ctx.Done()
		return selection.Response{}, ctx.Err()
	}
	return f.resp, f.err
}

func ptr[T any](v T) *T { return &v }

func classified(id string, confidence float64) *fakeClassifier {
	return &fakeClassifier{resp: selection.Response{
		CandidateID:   ptr(id),
		Confidence:    ptr(confidence),
		SelectorModel: ptr("jev-1.13.0"),
		Usage:         &selection.Usage{InputTokens: 10, OutputTokens: 1},
	}}
}

// isolate points every user directory at empty disk-backed temp trees.
func isolate(t *testing.T) (home, xdgConfig, xdgState string) {
	t.Helper()
	home, xdgConfig, xdgState = t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)
	t.Setenv("XDG_STATE_HOME", xdgState)
	t.Setenv("TYPESAFE_API_KEY", "")
	return home, xdgConfig, xdgState
}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("%s is not empty: %v", dir, entries)
	}
}

// runFake runs the select pipeline after argument parsing with an injected classifier.
func runFake(ctx context.Context, stdin []byte, c selection.Classifier) (stdout, stderr string, code int) {
	var out, errOut bytes.Buffer
	err := report(&errOut, runSelect(ctx, bytes.NewReader(stdin), &out, c))
	return out.String(), errOut.String(), exitCodeOf(err)
}

// runCommand runs the full select command, including argument parsing and real classifier wiring.
func runCommand(args []string, stdin io.Reader) (stdout, stderr string, code int) {
	var out, errOut bytes.Buffer
	err := selectCommand(args, stdin, &out, &errOut)
	return out.String(), errOut.String(), exitCodeOf(err)
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var status exitStatus
	if errors.As(err, &status) {
		return status.ExitCode()
	}
	return -1
}

func decodeOne(t *testing.T, stdout string) selection.Response {
	t.Helper()
	if strings.Count(stdout, "\n") != 1 || !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("stdout is not exactly one line: %q", stdout)
	}
	dec := json.NewDecoder(strings.NewReader(stdout))
	dec.DisallowUnknownFields()
	var got selection.Response
	if err := dec.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if dec.More() {
		t.Fatalf("stdout has more than one JSON value: %q", stdout)
	}
	return got
}

func TestSelectDoesNotLoadUserConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	b := fixture(t, "empty.json")
	var out bytes.Buffer
	if err := runSelect(context.Background(), bytes.NewReader(b), &out, nil); err != nil {
		t.Fatal(err)
	}
	var got selection.Response
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Reason != "no_candidates" {
		t.Fatal(got.Reason)
	}
}

func TestSelectSingletonExactBytes(t *testing.T) {
	isolate(t)
	stdout, stderr, code := runCommand([]string{"--json"}, bytes.NewReader(fixture(t, "singleton.json")))
	want := `{"schema_version":1,"request_id":"req-singleton-0001","candidate_set_hash":"04c55cbc25c880a61e31d9d5138e0b7dd5a01b6d608670224bec64018daeb5e5","status":"selected","candidate_id":"opencode/test-provider/model-a:high","reason":"singleton","confidence":null,"selector_model":null,"latency_ms":0,"usage":null}` + "\n"
	if stdout != want || stderr != "" || code != 0 {
		t.Fatalf("stdout=%q stderr=%q code=%d", stdout, stderr, code)
	}
}

func TestSelectEmptyAbstains(t *testing.T) {
	isolate(t)
	stdout, stderr, code := runCommand([]string{"--json", "--jev-model", "jev-1.13.0"}, bytes.NewReader(fixture(t, "empty.json")))
	got := decodeOne(t, stdout)
	if got.Status != selection.StatusAbstained || got.Reason != selection.ReasonNoCandidates || stderr != "" || code != 0 {
		t.Fatalf("got=%+v stderr=%q code=%d", got, stderr, code)
	}
}

func TestSelectClassifiedAndLowConfidence(t *testing.T) {
	b := fixture(t, "unicode.json")
	req, err := selection.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	id := req.Candidates[1].CandidateID
	for _, tc := range []struct {
		confidence float64
		status     string
		reason     string
	}{
		{0.83, selection.StatusSelected, selection.ReasonClassified},
		{0.20, selection.StatusAbstained, selection.ReasonLowConfidence},
	} {
		f := classified(id, tc.confidence)
		stdout, stderr, code := runFake(context.Background(), b, f)
		got := decodeOne(t, stdout)
		if got.Status != tc.status || got.Reason != tc.reason || stderr != "" || code != 0 || f.calls != 1 {
			t.Fatalf("confidence %v: got=%+v stderr=%q code=%d calls=%d", tc.confidence, got, stderr, code, f.calls)
		}
		if got.RequestID != req.RequestID || got.CandidateSetHash != req.CandidateSetHash {
			t.Fatalf("identity not echoed: %+v", got)
		}
	}
}

func TestSelectOutputDoesNotEscapeHTML(t *testing.T) {
	b := fixture(t, "unicode.json")
	req, err := selection.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runSelect(context.Background(), bytes.NewReader(b), &out, classified(req.Candidates[0].CandidateID, 0.9)); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out.Bytes(), []byte(`<`)) || bytes.Contains(out.Bytes(), []byte(`&`)) {
		t.Fatalf("HTML-escaped output: %s", out.Bytes())
	}
	keys := []string{"schema_version", "request_id", "candidate_set_hash", "status", "candidate_id", "reason", "confidence", "selector_model", "latency_ms", "usage"}
	last := -1
	for _, k := range keys {
		i := bytes.Index(out.Bytes(), []byte(`"`+k+`":`))
		if i <= last {
			t.Fatalf("key %s out of contract order in %s", k, out.Bytes())
		}
		last = i
	}
}

func TestSelectUsageErrors(t *testing.T) {
	isolate(t)
	for _, args := range [][]string{
		nil,
		{},
		{"--jev-model", "jev-1.13.0"},
		{"--json", "--json"},
		{"--json", "--jev-model"},
		{"--json", "--jev-model", "jev-latest"},
		{"--json", "--jev-model", "jev-preview"},
		{"--json", "--jev-model=jev-latest"},
		{"--json", "--jev-model="},
		{"--json", "--jev-model", "jev-1.13.0", "--jev-model", "jev-1.13.0"},
		{"--json", "--jev-model=jev-1.13.0", "--jev-model=jev-1.13.0"},
		{"--json", "--verbose"},
		{"--json", "extra"},
		{"--json", "--"},
		{"--json=true"},
		{"-json"},
		{"--JSON"},
	} {
		stdout, stderr, code := runCommand(args, failingReader{})
		if stdout != "" || stderr != "mroute: select: usage\n" || code != 2 {
			t.Errorf("args %q: stdout=%q stderr=%q code=%d", args, stdout, stderr, code)
		}
	}
}

// failingReader proves usage errors never read stdin.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { panic("stdin read on usage error") }

func TestSelectAcceptsBothPinSpellings(t *testing.T) {
	isolate(t)
	for _, args := range [][]string{
		{"--json"},
		{"--json", "--jev-model", "jev-1.13.0"},
		{"--jev-model", "jev-1.13.0", "--json"},
		{"--json", "--jev-model=jev-1.13.0"},
	} {
		stdout, stderr, code := runCommand(args, bytes.NewReader(fixture(t, "singleton.json")))
		if got := decodeOne(t, stdout); got.Reason != selection.ReasonSingleton || stderr != "" || code != 0 {
			t.Errorf("args %q: got=%+v stderr=%q code=%d", args, got, stderr, code)
		}
	}
}

func TestSelectInvalidInput(t *testing.T) {
	isolate(t)
	singleton := fixture(t, "singleton.json")
	inputs := map[string][]byte{
		"empty stdin":   nil,
		"trailing data": append(append([]byte{}, singleton...), " {}"...),
		"oversize":      append(append([]byte{}, singleton...), bytes.Repeat([]byte(" "), selection.MaxRequestBytes)...),
		"secret echo":   []byte(`{"schema_version":1,"request_id":"` + secretMarker + `"`),
	}
	for _, name := range []string{"duplicate-key-top.json", "duplicate-key-nested-state.json", "oversize.json", "trailing-value.json", "unknown-field.json", "hash-mismatch.json", "lone-surrogate.json"} {
		inputs[name] = fixture(t, "invalid/"+name)
	}
	for name, in := range inputs {
		stdout, stderr, code := runCommand([]string{"--json", "--jev-model", "jev-1.13.0"}, bytes.NewReader(in))
		if stdout != "" || stderr != "mroute: select: invalid_request\n" || code != 2 {
			t.Errorf("%s: stdout=%q stderr=%q code=%d", name, stdout, stderr, code)
		}
	}
}

func TestSelectMultiWithoutPinIsInvalidRequest(t *testing.T) {
	isolate(t)
	t.Setenv("TYPESAFE_API_KEY", secretMarker)
	stdout, stderr, code := runCommand([]string{"--json"}, bytes.NewReader(fixture(t, "unicode.json")))
	got := decodeOne(t, stdout)
	if got.Status != selection.StatusError || got.Reason != selection.ReasonInvalidRequest || stderr != "mroute: select: invalid_request\n" || code != 2 {
		t.Fatalf("got=%+v stderr=%q code=%d", got, stderr, code)
	}
}

func TestSelectMultiWithoutKeyIsAuthenticationWithoutNetwork(t *testing.T) {
	isolate(t)
	stdout, stderr, code := runCommand([]string{"--json", "--jev-model", "jev-1.13.0"}, bytes.NewReader(fixture(t, "unicode.json")))
	got := decodeOne(t, stdout)
	if got.Status != selection.StatusError || got.Reason != selection.ReasonAuthentication || stderr != "mroute: select: authentication\n" || code != 1 {
		t.Fatalf("got=%+v stderr=%q code=%d", got, stderr, code)
	}
}

func TestSelectClassifierErrors(t *testing.T) {
	b := fixture(t, "unicode.json")
	for _, reason := range []string{selection.ReasonTimeout, selection.ReasonAuthentication, selection.ReasonNetwork, selection.ReasonRateLimit, selection.ReasonOverloaded} {
		f := &fakeClassifier{err: &selection.Error{Reason: reason}}
		stdout, stderr, code := runFake(context.Background(), b, f)
		got := decodeOne(t, stdout)
		if got.Status != selection.StatusError || got.Reason != reason || stderr != "mroute: select: "+reason+"\n" || code != 1 {
			t.Errorf("%s: got=%+v stderr=%q code=%d", reason, got, stderr, code)
		}
		if got.CandidateID != nil || got.Confidence != nil || got.SelectorModel != nil || got.Usage != nil {
			t.Errorf("%s: error envelope carries values: %+v", reason, got)
		}
	}
}

func TestSelectNeverEchoesSecrets(t *testing.T) {
	b := fixture(t, "unicode.json")
	req, err := selection.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	for name, f := range map[string]*fakeClassifier{
		"raw error":       {err: errors.New("401 Bearer " + secretMarker)},
		"typed error":     {err: &selection.Error{Reason: secretMarker}},
		"candidate":       classified(secretMarker, 0.9),
		"selector model":  {resp: selection.Response{CandidateID: ptr(req.Candidates[0].CandidateID), Confidence: ptr(0.9), SelectorModel: ptr(secretMarker)}},
		"wrapped context": {err: errors.Join(context.DeadlineExceeded, errors.New(secretMarker))},
	} {
		stdout, stderr, code := runFake(context.Background(), b, f)
		if strings.Contains(stdout, secretMarker) || strings.Contains(stderr, secretMarker) {
			t.Errorf("%s: secret leaked: stdout=%q stderr=%q", name, stdout, stderr)
		}
		got := decodeOne(t, stdout)
		if got.Status != selection.StatusError || code != 1 || stderr != "mroute: select: "+got.Reason+"\n" {
			t.Errorf("%s: got=%+v stderr=%q code=%d", name, got, stderr, code)
		}
	}
}

func TestSelectCancellation(t *testing.T) {
	b := fixture(t, "unicode.json")
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeClassifier{block: true, entered: make(chan struct{})}
	done := make(chan struct{})
	var stdout, stderr string
	var code int
	go func() {
		defer close(done)
		stdout, stderr, code = runFake(ctx, b, f)
	}()
	<-f.entered // cancel during inference, after stdin was fully read
	cancel()
	<-done
	got := decodeOne(t, stdout)
	if got.Reason != selection.ReasonCancelled || stderr != "mroute: select: cancelled\n" || code != 1 {
		t.Fatalf("got=%+v stderr=%q code=%d", got, stderr, code)
	}
}

func TestSelectDeadline(t *testing.T) {
	b := fixture(t, "unicode.json")
	// Long enough to finish reading stdin, so the deadline elapses during inference.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	stdout, stderr, code := runFake(ctx, b, &fakeClassifier{block: true})
	if got := decodeOne(t, stdout); got.Reason != selection.ReasonTimeout || stderr != "mroute: select: timeout\n" || code != 1 {
		t.Fatalf("got=%+v stderr=%q code=%d", got, stderr, code)
	}
}

// runBlockedStdin runs the pipeline over a stdin that never delivers data or EOF.
func runBlockedStdin(t *testing.T, ctx context.Context) (stdout, stderr string, code int) {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	var out, errOut bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- report(&errOut, runSelect(ctx, pr, &out, &fakeClassifier{block: true})) }()
	select {
	case err := <-done:
		return out.String(), errOut.String(), exitCodeOf(err)
	case <-time.After(5 * time.Second):
		t.Fatal("runSelect ignored the context while reading stdin")
		return "", "", 0
	}
}

func TestSelectStdinDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	stdout, stderr, code := runBlockedStdin(t, ctx)
	if stdout != "" || stderr != "mroute: select: timeout\n" || code != 1 {
		t.Fatalf("stdout=%q stderr=%q code=%d", stdout, stderr, code)
	}
}

func TestSelectStdinCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	defer cancel()
	stdout, stderr, code := runBlockedStdin(t, ctx)
	if stdout != "" || stderr != "mroute: select: cancelled\n" || code != 1 {
		t.Fatalf("stdout=%q stderr=%q code=%d", stdout, stderr, code)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe " + secretMarker) }

func TestSelectOutputFailure(t *testing.T) {
	var errOut bytes.Buffer
	err := report(&errOut, runSelect(context.Background(), bytes.NewReader(fixture(t, "singleton.json")), failingWriter{}, nil))
	if exitCodeOf(err) != 1 || errOut.String() != "mroute: select: output\n" {
		t.Fatalf("err=%v stderr=%q", err, errOut.String())
	}
}

// withStdio runs run(args) with the process stdio replaced by files.
func withStdio(t *testing.T, stdin []byte, args []string) (stdout, stderr string, err error) {
	t.Helper()
	dir := t.TempDir()
	inPath := filepath.Join(dir, "stdin")
	if err := os.WriteFile(inPath, stdin, 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(inPath)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	outF, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	defer outF.Close()
	errF, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	defer errF.Close()
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = in, outF, errF
	err = run(args)
	os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr
	o, rerr := os.ReadFile(outF.Name())
	if rerr != nil {
		t.Fatal(rerr)
	}
	e, rerr := os.ReadFile(errF.Name())
	if rerr != nil {
		t.Fatal(rerr)
	}
	return string(o), string(e), err
}

// fakeEngines puts claude/opencode/codex on PATH that would record any launch.
func fakeEngines(t *testing.T) (marker string) {
	t.Helper()
	bin := t.TempDir()
	marker = filepath.Join(t.TempDir(), "engine-launched")
	for _, name := range []string{"claude", "opencode", "codex"} {
		script := "#!/bin/sh\necho " + name + " >> '" + marker + "'\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	return marker
}

func TestSelectHasNoUserSideEffects(t *testing.T) {
	home, xdgConfig, xdgState := isolate(t)
	marker := fakeEngines(t)
	cases := []struct {
		fixture string
		args    []string
		code    int
	}{
		{"empty.json", []string{"select", "--json"}, 0},
		{"singleton.json", []string{"select", "--json"}, 0},
		{"unicode.json", []string{"select", "--json"}, 2},
		{"unicode.json", []string{"select", "--json", "--jev-model", "jev-1.13.0"}, 1},
	}
	for _, tc := range cases {
		stdout, _, err := withStdio(t, fixture(t, tc.fixture), tc.args)
		decodeOne(t, stdout)
		if exitCodeOf(err) != tc.code {
			t.Errorf("%s %q: err=%v", tc.fixture, tc.args, err)
		}
	}
	b := fixture(t, "unicode.json")
	req, err := selection.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, code := runFake(context.Background(), b, classified(req.Candidates[0].CandidateID, 0.9)); code != 0 {
		t.Fatalf("fake classified exit %d", code)
	}
	for _, dir := range []string{home, xdgConfig, xdgState} {
		assertEmptyDir(t, dir)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an engine was launched: %v", err)
	}
}

func TestSelectBranchesBeforeConfigLoad(t *testing.T) {
	_, xdgConfig, _ := isolate(t)
	if err := os.MkdirAll(filepath.Join(xdgConfig, "mroute"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdgConfig, "mroute", "config.toml"), []byte("this is [not toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"route", "x"}); err == nil || !strings.Contains(err.Error(), "load config") {
		t.Fatalf("precondition: invalid config should break legacy commands, got %v", err)
	}
	stdout, stderr, err := withStdio(t, fixture(t, "singleton.json"), []string{"select", "--json"})
	if err != nil || stderr != "" || decodeOne(t, stdout).Reason != selection.ReasonSingleton {
		t.Fatalf("stdout=%q stderr=%q err=%v", stdout, stderr, err)
	}
}

func TestSelectWithoutHome(t *testing.T) {
	isolate(t)
	os.Unsetenv("HOME")
	os.Unsetenv("XDG_CONFIG_HOME")
	os.Unsetenv("XDG_STATE_HOME")
	stdout, stderr, err := withStdio(t, fixture(t, "empty.json"), []string{"select", "--json"})
	if err != nil || stderr != "" || decodeOne(t, stdout).Reason != selection.ReasonNoCandidates {
		t.Fatalf("stdout=%q stderr=%q err=%v", stdout, stderr, err)
	}
}

func TestSelectIgnoresUnwritableDirectories(t *testing.T) {
	home, xdgConfig, xdgState := isolate(t)
	logDir := filepath.Join(xdgState, "mroute")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{logDir, xdgState, xdgConfig, home} {
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o700) })
	}
	stdout, stderr, err := withStdio(t, fixture(t, "singleton.json"), []string{"select", "--json"})
	if err != nil || stderr != "" || decodeOne(t, stdout).Reason != selection.ReasonSingleton {
		t.Fatalf("stdout=%q stderr=%q err=%v", stdout, stderr, err)
	}
}

func TestSelectUsageThroughRun(t *testing.T) {
	isolate(t)
	stdout, stderr, err := withStdio(t, nil, []string{"select", "--bogus"})
	if stdout != "" || stderr != "mroute: select: usage\n" || exitCodeOf(err) != 2 {
		t.Fatalf("stdout=%q stderr=%q err=%v", stdout, stderr, err)
	}
}

func TestLegacyErrorsUnchanged(t *testing.T) {
	isolate(t)
	if err := run([]string{}); err == nil || err.Error() != "usage: mroute init|route|run|log|explain|eval|doctor|serve" || exitCodeOf(err) != -1 {
		t.Fatalf("empty args: %v", err)
	}
	if err := run([]string{"bogus"}); err == nil || exitCodeOf(err) != -1 {
		t.Fatalf("unknown command: %v", err)
	}
}
