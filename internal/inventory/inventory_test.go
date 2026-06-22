package inventory

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestKey(t *testing.T) {
	if got := Key("o/r", ".github/workflows/x.yml"); got != "o/r::.github/workflows/x.yml" {
		t.Fatalf("Key = %q", got)
	}
}

func TestLoadCronsRoundTrip(t *testing.T) {
	p := write(t, "crons.json", `[
	  {"repo":"o/r","path":".github/workflows/x.yml","cron_expression":"0 9 * * *",
	   "workflow_name":"Nightly","state":"active","default_branch":"main","first_cron_line":7}
	]`)
	crons, err := LoadCrons(p)
	if err != nil {
		t.Fatalf("LoadCrons: %v", err)
	}
	if len(crons) != 1 {
		t.Fatalf("want 1 cron, got %d", len(crons))
	}
	c := crons[0]
	if c.Repo != "o/r" || c.CronExpression != "0 9 * * *" || c.FirstCronLine != 7 {
		t.Fatalf("unexpected cron: %#v", c)
	}
}

func TestLoadLastRunsErrorEntryIsZero(t *testing.T) {
	// A normal run plus an error-only entry (which has no run fields).
	p := write(t, "last_runs.json", `{
	  "o/r::.github/workflows/x.yml":{"actor":"li-cron[bot]","conclusion":"success","last_run":"2026-01-01T00:00:00Z","url":"http://x"},
	  "o/r::.github/workflows/bad.yml":{"error":"not found"}
	}`)
	runs, err := LoadLastRuns(p)
	if err != nil {
		t.Fatalf("LoadLastRuns: %v", err)
	}
	if runs[Key("o/r", ".github/workflows/x.yml")].Actor != "li-cron[bot]" {
		t.Fatalf("normal entry not parsed: %#v", runs)
	}
	if got := runs[Key("o/r", ".github/workflows/bad.yml")]; got != (RunEvidence{}) {
		t.Fatalf("error entry should be zero value, got %#v", got)
	}
}

func TestLoadCronsMissingFileErrors(t *testing.T) {
	if _, err := LoadCrons(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("missing file should error")
	}
}

func TestLoadCronsMalformedErrors(t *testing.T) {
	p := write(t, "crons.json", "{not json")
	if _, err := LoadCrons(p); err == nil {
		t.Fatal("malformed JSON should error")
	}
}
