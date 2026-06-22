package cronequiv

import (
	"strings"
	"testing"
)

func TestDailyExpandsAStarField(t *testing.T) {
	// minute/hour fixed; month "*" is the first always-safe star.
	if got, ok := Rewrite("0 9 * * *"); !ok || got != "0 9 * 1-12 *" {
		t.Errorf("got %q ok=%v, want '0 9 * 1-12 *'", got, ok)
	}
}

func TestMonthlyDomExpandsMonth(t *testing.T) {
	if got, _ := Rewrite("0 9 1 * *"); got != "0 9 1 1-12 *" {
		t.Errorf("got %q, want '0 9 1 1-12 *'", got)
	}
}

func TestWeeklyDowExpandsMonthNotDom(t *testing.T) {
	got, _ := Rewrite("0 9 * * 1")
	if got != "0 9 * 1-12 1" {
		t.Errorf("got %q, want '0 9 * 1-12 1'", got)
	}
	if strings.Fields(got)[2] != "*" {
		t.Errorf("dom must stay '*', got %q", got)
	}
}

func TestDomRestrictedDowStarNotExpanded(t *testing.T) {
	got, _ := Rewrite("0 9 5 6 *")
	if strings.Fields(got)[4] != "*" {
		t.Errorf("dow must stay '*', got %q", got)
	}
	if got == "0 9 5 6 *" {
		t.Errorf("expected a change, got identical")
	}
}

func TestEvery15MinExpandsHour(t *testing.T) {
	if got, _ := Rewrite("*/15 * * * *"); got != "*/15 0-23 * * *" {
		t.Errorf("got %q, want '*/15 0-23 * * *'", got)
	}
}

func TestCommaFallbackReordersList(t *testing.T) {
	// No safely-expandable plain star -> reorder a comma list.
	if got, _ := Rewrite("0 9,17 1 1 *"); got != "0 17,9 1 1 *" {
		t.Errorf("got %q, want '0 17,9 1 1 *'", got)
	}
}

func TestFullySpecifiedUsesSingleValueRange(t *testing.T) {
	if got, _ := Rewrite("30 4 1 1 0"); got != "30-30 4 1 1 0" {
		t.Errorf("got %q, want '30-30 4 1 1 0'", got)
	}
}

func TestResultAlwaysDiffersAndHasFiveFields(t *testing.T) {
	for _, expr := range []string{
		"0 9 * * *", "0 9 1 * *", "0 9 * * 1",
		"*/15 * * * *", "0 9,17 1 1 *", "30 4 1 1 0",
	} {
		got, ok := Rewrite(expr)
		if !ok {
			t.Errorf("Rewrite(%q) not ok", expr)
		}
		if got == expr {
			t.Errorf("Rewrite(%q) unchanged", expr)
		}
		if len(strings.Fields(got)) != 5 {
			t.Errorf("Rewrite(%q) = %q, want 5 fields", expr, got)
		}
	}
}

func TestWhitespaceIsNormalized(t *testing.T) {
	if got, _ := Rewrite("0   9   *  * *"); got != "0 9 * 1-12 *" {
		t.Errorf("got %q, want '0 9 * 1-12 *'", got)
	}
}

func TestNonFiveFieldReturnsNotOk(t *testing.T) {
	for _, expr := range []string{"not a cron", "* * * *", ""} {
		if _, ok := Rewrite(expr); ok {
			t.Errorf("Rewrite(%q) ok=true, want false", expr)
		}
	}
}

func TestSymmetricCommaListStillRewrites(t *testing.T) {
	// A symmetric comma list ("0,0") reverses to itself, so the comma reorder is
	// a no-op. Rewrite must fall through to another transform and still produce a
	// textually different cron, else the bot's "edit" won't re-home the actor.
	got, ok := Rewrite("0,0 4 1 1 0")
	if !ok || got == "0,0 4 1 1 0" {
		t.Errorf("Rewrite('0,0 4 1 1 0') = %q ok=%v, want a different 5-field cron", got, ok)
	}
	if len(strings.Fields(got)) != 5 {
		t.Errorf("got %q, want 5 fields", got)
	}
}
