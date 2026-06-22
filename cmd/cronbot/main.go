// Command cronbot is the brain of the li-cron cron-intake flow. It parses an
// approved issue-form request, validates it, builds the provisioning plan, and
// (optionally) upserts the central registry. The intake workflow then uses the
// emitted plan to create the branch, edit the workflow, and merge the PR as the
// li-cron App.
//
// Usage:
//
//	cronbot --issue-body issue.md --request-url URL \
//	        --registry registry.json --json-out plan.json
//
// Reads the issue body from --issue-body, or stdin when that is "-" or empty.
// Exits 1 when the request is invalid (with errors suitable to comment back).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"fixcron/internal/cronbot"
	"fixcron/internal/intake"
	"fixcron/internal/registry"
)

func main() {
	bodyPath := flag.String("issue-body", "-", "issue-form body file ('-' or empty = stdin)")
	requestURL := flag.String("request-url", "", "URL of the request issue (recorded in the registry)")
	registryPath := flag.String("registry", "", "central registry JSON to upsert into (omit to skip the write)")
	jsonOut := flag.String("json-out", "", "write the plan as JSON to this path")
	flag.Parse()

	body, err := readBody(*bodyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read issue body:", err)
		os.Exit(2)
	}

	req := intake.Parse(body)
	if errs := req.Validate(); len(errs) > 0 {
		fmt.Fprintln(os.Stderr, "Invalid cron request:")
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  -", e)
		}
		os.Exit(1)
	}

	plan := cronbot.BuildPlan(req, *requestURL)
	printPlan(plan, os.Stdout)

	if *registryPath != "" {
		reg, err := registry.Load(*registryPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "load registry:", err)
			os.Exit(2)
		}
		added := reg.Upsert(plan.Entry)
		if err := reg.Save(*registryPath); err != nil {
			fmt.Fprintln(os.Stderr, "save registry:", err)
			os.Exit(2)
		}
		verb := "updated"
		if added {
			verb = "added"
		}
		fmt.Fprintf(os.Stdout, "\nregistry %s: %s\n", verb, plan.Entry.Key())
	}

	if *jsonOut != "" {
		b, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "marshal plan:", err)
			os.Exit(2)
		}
		if err := os.WriteFile(*jsonOut, append(b, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write plan:", err)
			os.Exit(2)
		}
	}
}

func readBody(path string) (string, error) {
	if path == "" || path == "-" {
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
	b, err := os.ReadFile(path)
	return string(b), err
}

func printPlan(p cronbot.Plan, w io.Writer) {
	fmt.Fprintf(w, "Cron request: %s :: %s\n", p.Repo, p.Path)
	fmt.Fprintf(w, "  owner team : %s\n", p.Entry.OwnerTeam)
	fmt.Fprintf(w, "  schedule   : '%s'  (bot edit -> '%s')\n", p.OldExpr, p.NewExpr)
	if !p.CanRewrite {
		fmt.Fprintln(w, "  WARNING: no safe schedule-neutral rewrite; bot edit equals the original")
	}
	fmt.Fprintf(w, "  branch     : %s\n", p.Branch)
	fmt.Fprintf(w, "  merge      : %s (so li-cron[bot] becomes the actor)\n", p.MergeMethod)
}
