// Package intake turns a GitHub issue-form submission into a validated cron
// request. GitHub renders an issue form as markdown: each field is a "### Label"
// header followed by the value (or "_No response_" when left blank).
package intake

import (
	"fmt"
	"regexp"
	"strings"
)

// CronRequest is one parsed cron-provisioning request. The owning team is not a
// request field — it is fixed policy (see cronbot.OwnerTeam). Cadence is not
// stored either; the cron expression already encodes it.
type CronRequest struct {
	Repo          string
	Path          string
	Expr          string
	Justification string
}

// label header -> struct field setter.
var headerField = map[string]func(*CronRequest, string){
	"target repository": func(r *CronRequest, v string) { r.Repo = v },
	"workflow path":     func(r *CronRequest, v string) { r.Path = v },
	"cron expression":   func(r *CronRequest, v string) { r.Expr = v },
	"justification":     func(r *CronRequest, v string) { r.Justification = v },
}

var repoRE = regexp.MustCompile(`^[^/\s]+/[^/\s]+$`)
var workflowRE = regexp.MustCompile(`^\.github/workflows/[^/]+\.ya?ml$`)

// Parse reads an issue-form body into a CronRequest. Unknown sections are
// ignored; "_No response_" becomes empty.
func Parse(body string) CronRequest {
	var req CronRequest
	var label string
	var value []string

	flush := func() {
		if label == "" {
			return
		}
		v := strings.TrimSpace(strings.Join(value, "\n"))
		if strings.EqualFold(v, "_No response_") {
			v = ""
		}
		if set, ok := headerField[strings.ToLower(label)]; ok {
			set(&req, v)
		}
	}

	for _, line := range strings.Split(body, "\n") {
		if h, ok := strings.CutPrefix(line, "### "); ok {
			flush()
			label = strings.TrimSpace(h)
			value = nil
			continue
		}
		value = append(value, line)
	}
	flush()
	return req
}

// Validate returns one error per problem with the request.
func (r CronRequest) Validate() []error {
	var errs []error
	if !repoRE.MatchString(r.Repo) {
		errs = append(errs, fmt.Errorf("target repository %q must be owner/name", r.Repo))
	}
	if !workflowRE.MatchString(r.Path) {
		errs = append(errs, fmt.Errorf("workflow path %q must be .github/workflows/*.yml", r.Path))
	}
	if len(strings.Fields(r.Expr)) != 5 {
		errs = append(errs, fmt.Errorf("cron expression %q must be 5 fields", r.Expr))
	}
	return errs
}
