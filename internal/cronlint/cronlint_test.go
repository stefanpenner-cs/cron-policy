package cronlint

import (
	"reflect"
	"testing"
)

func TestParseCrons(t *testing.T) {
	content := "on:\n" +
		"  schedule:\n" +
		"    - cron: '0 9 * * *'\n" +
		"    - cron: \"*/5 * * * *\"  # every 5 min\n" +
		"    - cron: 0 0 * * 0\n" +
		"  push: {}\n" +
		"# - cron: '1 2 3 4 5'\n"

	got := ParseCrons(content)
	want := []CronRef{
		{Line: 3, Expr: "0 9 * * *"},
		{Line: 4, Expr: "*/5 * * * *"},
		{Line: 5, Expr: "0 0 * * 0"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseCrons:\n got %#v\nwant %#v", got, want)
	}
}

func TestParseCronsNone(t *testing.T) {
	if got := ParseCrons("on:\n  push: {}\n"); len(got) != 0 {
		t.Fatalf("expected no crons, got %#v", got)
	}
}

func TestLintDefaultUnregistered(t *testing.T) {
	files := []WorkflowFile{
		{Path: ".github/workflows/cron.yml", Content: "    - cron: '0 9 * * *'\n"},
	}
	v := Lint(files, Config{})
	if len(v) != 1 {
		t.Fatalf("want 1 violation, got %d: %#v", len(v), v)
	}
	if v[0].Rule != "unregistered-cron" || v[0].Line != 1 || v[0].Expr != "0 9 * * *" {
		t.Fatalf("unexpected violation: %#v", v[0])
	}
}

func TestLintDefaultRegisteredPasses(t *testing.T) {
	files := []WorkflowFile{
		{Path: ".github/workflows/cron.yml", Content: "    - cron: '0 9 * * *'\n"},
	}
	cfg := Config{Registry: map[string]bool{".github/workflows/cron.yml": true}}
	if v := Lint(files, cfg); len(v) != 0 {
		t.Fatalf("registered file should pass, got %#v", v)
	}
}

func TestLintBanAll(t *testing.T) {
	files := []WorkflowFile{
		{Path: ".github/workflows/cron.yml", Content: "    - cron: '0 9 * * *'\n"},
	}
	// Registered, but ban-all ignores the registry.
	cfg := Config{BanAll: true, Registry: map[string]bool{".github/workflows/cron.yml": true}}
	v := Lint(files, cfg)
	if len(v) != 1 || v[0].Rule != "no-new-crons" {
		t.Fatalf("ban-all should reject even registered crons, got %#v", v)
	}
}

func TestLintAllowListExempts(t *testing.T) {
	files := []WorkflowFile{
		{Path: "vendor/upstream/.github/workflows/mirror.yml", Content: "    - cron: '0 9 * * *'\n"},
	}
	cfg := Config{BanAll: true, Allow: []string{"vendor/**"}}
	if v := Lint(files, cfg); len(v) != 0 {
		t.Fatalf("allow-listed file should be exempt, got %#v", v)
	}
}

func TestLintAllowListBasename(t *testing.T) {
	files := []WorkflowFile{
		{Path: ".github/workflows/blessed.yml", Content: "    - cron: '0 9 * * *'\n"},
	}
	cfg := Config{Allow: []string{"blessed.yml"}}
	if v := Lint(files, cfg); len(v) != 0 {
		t.Fatalf("basename allow pattern should exempt, got %#v", v)
	}
}

func TestLintNoCronNoViolation(t *testing.T) {
	files := []WorkflowFile{
		{Path: ".github/workflows/push.yml", Content: "on:\n  push: {}\n"},
	}
	if v := Lint(files, Config{}); len(v) != 0 {
		t.Fatalf("file without crons should pass, got %#v", v)
	}
}
