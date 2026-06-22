package cronguard

import (
	"bytes"
	"strings"
	"testing"
)

const cronA = "on:\n  schedule:\n    - cron: '0 9 * * *'\n"
const cronB = "on:\n  schedule:\n    - cron: '0 10 * * *'\n"

func TestNewCronByHumanFails(t *testing.T) {
	diffs := []FileDiff{{Path: "w.yml", Base: "", Head: cronA}}
	v := Guard(diffs, "alice_LinkedIn", nil)
	if len(v) != 1 || v[0].Expr != "0 9 * * *" {
		t.Fatalf("human adding a cron should fail, got %#v", v)
	}
}

func TestNewCronByBotPasses(t *testing.T) {
	diffs := []FileDiff{{Path: "w.yml", Base: "", Head: cronA}}
	if v := Guard(diffs, "li-cron[bot]", nil); len(v) != 0 {
		t.Fatalf("bot adding a cron should pass, got %#v", v)
	}
}

func TestChangedValueByHumanFails(t *testing.T) {
	diffs := []FileDiff{{Path: "w.yml", Base: cronA, Head: cronB}}
	v := Guard(diffs, "alice_LinkedIn", nil)
	if len(v) != 1 || v[0].Expr != "0 10 * * *" {
		t.Fatalf("changing a cron value should fail on the new value, got %#v", v)
	}
}

func TestRemovedCronPasses(t *testing.T) {
	diffs := []FileDiff{{Path: "w.yml", Base: cronA, Head: "on:\n  push: {}\n"}}
	if v := Guard(diffs, "alice_LinkedIn", nil); len(v) != 0 {
		t.Fatalf("removing a cron should be allowed, got %#v", v)
	}
}

func TestUnchangedCronPasses(t *testing.T) {
	diffs := []FileDiff{{Path: "w.yml", Base: cronA, Head: cronA}}
	if v := Guard(diffs, "alice_LinkedIn", nil); len(v) != 0 {
		t.Fatalf("unchanged cron should pass, got %#v", v)
	}
}

func TestAllowedActorCaseInsensitive(t *testing.T) {
	diffs := []FileDiff{{Path: "w.yml", Base: "", Head: cronA}}
	if v := Guard(diffs, "LI-CRON[BOT]", nil); len(v) != 0 {
		t.Fatalf("allowed-actor match should be case-insensitive, got %#v", v)
	}
}

func TestRemovingCronIsAllowed(t *testing.T) {
	diffs := []FileDiff{{Path: "w.yml", Base: cronA, Head: ""}}
	if v := Guard(diffs, "alice_LinkedIn", nil); len(v) != 0 {
		t.Fatalf("removing a cron should pass, got %#v", v)
	}
}

func TestEmitEmptyIsClean(t *testing.T) {
	var b bytes.Buffer
	if n := Emit(nil, &b); n != 0 {
		t.Fatalf("want 0, got %d", n)
	}
	if !strings.Contains(b.String(), "No unauthorized cron changes") {
		t.Fatalf("unexpected output: %q", b.String())
	}
}

func TestEmitReportsViolations(t *testing.T) {
	var b bytes.Buffer
	v := Guard([]FileDiff{{Path: "w.yml", Base: "", Head: cronA}}, "alice_LinkedIn", nil)
	if n := Emit(v, &b); n != 1 {
		t.Fatalf("want 1, got %d", n)
	}
	out := b.String()
	if !strings.Contains(out, "w.yml") || !strings.Contains(out, "0 9 * * *") {
		t.Fatalf("violation detail missing: %q", out)
	}
}
