// Package cronbot is the brain of the intake bot. It turns an approved cron
// request into a concrete provisioning plan: the central-registry entry plus the
// schedule-neutral "bot touch" that, once merged by li-cron[bot], makes the bot
// the durable actor.
//
// The plan is pure and testable. The actual git/PR/merge is performed by the
// intake workflow using `gh` authenticated as the li-cron App (the hands).
package cronbot

import (
	"regexp"
	"strings"

	"fixcron/internal/cronequiv"
	"fixcron/internal/intake"
	"fixcron/internal/registry"
	"fixcron/internal/rehome"
)

// OwnerTeam is the single team accountable for every managed cron and the only
// crew allowed to approve and merge cron changes. It is fixed policy, not a
// per-request field.
const OwnerTeam = "ci-cd-platform-reviewers"

// Plan is everything the intake workflow needs to land a li-cron-owned cron.
type Plan struct {
	Repo          string         `json:"repo"`
	Path          string         `json:"path"`
	OldExpr       string         `json:"old_expr"`
	NewExpr       string         `json:"new_expr"`
	CanRewrite    bool           `json:"can_rewrite"`
	Branch        string         `json:"branch"`
	PRTitle       string         `json:"pr_title"`
	CommitMessage string         `json:"commit_message"`
	MergeMethod   string         `json:"merge_method"`
	Entry         registry.Entry `json:"entry"`
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = nonSlug.ReplaceAllString(strings.ToLower(s), "-")
	return strings.Trim(s, "-")
}

// BuildPlan computes the provisioning plan for an approved request. The bot's
// edit rewrites the cron to a schedule-equivalent but textually different value
// (via cronequiv); merging that as li-cron[bot] re-attributes the actor without
// changing when it runs.
func BuildPlan(req intake.CronRequest, requestURL string) Plan {
	newExpr, ok := cronequiv.Rewrite(req.Expr)
	if !ok {
		newExpr = req.Expr
	}
	return Plan{
		Repo:          req.Repo,
		Path:          req.Path,
		OldExpr:       req.Expr,
		NewExpr:       newExpr,
		CanRewrite:    ok,
		Branch:        "li-cron/" + slug(req.Repo+"-"+req.Path),
		PRTitle:       "chore(cron): li-cron-owned schedule for " + req.Path,
		CommitMessage: "chore(cron): re-home " + req.Path + " onto li-cron[bot]",
		MergeMethod:   rehome.MergeMethod,
		Entry: registry.Entry{
			Repo:      req.Repo,
			Path:      req.Path,
			Expr:      req.Expr,
			OwnerTeam: OwnerTeam,
			Request:   requestURL,
		},
	}
}
