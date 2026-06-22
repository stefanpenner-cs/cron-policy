package cronbot

import (
	"testing"

	"fixcron/internal/intake"
)

func req() intake.CronRequest {
	return intake.CronRequest{
		Repo: "linkedin-actions/foo",
		Path: ".github/workflows/nightly.yml",
		Expr: "0 9 * * *",
	}
}

func TestBuildPlanRewritesScheduleNeutral(t *testing.T) {
	p := BuildPlan(req(), "https://github.com/o/r/issues/1")
	if !p.CanRewrite {
		t.Fatal("expected a safe rewrite for '0 9 * * *'")
	}
	if p.NewExpr == p.OldExpr {
		t.Fatalf("rewrite must change the string, got %q", p.NewExpr)
	}
	if p.OldExpr != "0 9 * * *" {
		t.Fatalf("old expr changed: %q", p.OldExpr)
	}
}

func TestBuildPlanBranchAndEntry(t *testing.T) {
	p := BuildPlan(req(), "https://github.com/o/r/issues/1")
	if p.Branch != "li-cron/linkedin-actions-foo-github-workflows-nightly-yml" {
		t.Fatalf("unexpected branch: %q", p.Branch)
	}
	if p.Entry.OwnerTeam != OwnerTeam || p.Entry.Request != "https://github.com/o/r/issues/1" {
		t.Fatalf("entry not populated: %#v", p.Entry)
	}
	if p.Entry.Expr != "0 9 * * *" {
		t.Fatalf("registry should record the real schedule, got %q", p.Entry.Expr)
	}
}
