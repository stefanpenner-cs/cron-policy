// Command cronbot is the brain of the cron-bot cron-intake flow. It parses an
// approved issue-form request, validates it, builds the provisioning plan, and
// (optionally) upserts the central registry. The intake workflow then uses the
// emitted plan to create the branch, edit the workflow, and merge the PR as the
// cron-bot App.
//
// Usage:
//
//	cronbot --issue-body issue.md --request-url URL \
//	        --registry registry.json --json-out plan.json
//
// Reads the issue body from --issue-body, or stdin when that is "-" or empty.
// Exits 1 when the request is invalid (with errors suitable to comment back).
//
// With --commit-push it also commits the registry change and pushes it to
// --branch, retrying on a non-fast-forward by re-syncing and re-applying (see
// internal/gitsync). Run it from the repo root (so --git-dir "." matches cwd).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"fixcron/internal/cronbot"
	"fixcron/internal/gitsync"
	"fixcron/internal/intake"
	"fixcron/internal/registry"
)

// commitConfig controls the optional commit-and-push of the registry change.
type commitConfig struct {
	enabled                                      bool
	gitDir, remote, branch, message, name, email string
	maxAttempts                                  int
}

func main() {
	bodyPath := flag.String("issue-body", "-", "issue-form body file ('-' or empty = stdin)")
	requestURL := flag.String("request-url", "", "URL of the request issue (recorded in the registry)")
	registryPath := flag.String("registry", "", "central registry JSON to upsert into (omit to skip the write)")
	jsonOut := flag.String("json-out", "", "write the plan as JSON to this path")
	remove := flag.Bool("remove", false, "process a removal request: de-register instead of register")
	commitPush := flag.Bool("commit-push", false, "commit the registry change and push it to --branch, retrying on conflict")
	gitDir := flag.String("git-dir", ".", "git working tree for --commit-push (should equal the current directory)")
	remote := flag.String("remote", "origin", "git remote to push to with --commit-push")
	branch := flag.String("branch", "", "branch to push to (required with --commit-push)")
	maxAttempts := flag.Int("max-attempts", 8, "max sync+apply+push tries for --commit-push")
	commitMessage := flag.String("commit-message", "", "commit message for --commit-push (defaulted when empty)")
	gitName := flag.String("git-name", "cron-bot[bot]", "commit author/committer name for --commit-push")
	gitEmail := flag.String("git-email", "cron-bot[bot]@users.noreply.github.com", "commit author/committer email for --commit-push")
	flag.Parse()

	co := commitConfig{
		enabled: *commitPush, gitDir: *gitDir, remote: *remote, branch: *branch,
		message: *commitMessage, name: *gitName, email: *gitEmail, maxAttempts: *maxAttempts,
	}
	if co.enabled {
		if *registryPath == "" || co.branch == "" {
			fmt.Fprintln(os.Stderr, "--commit-push requires --registry and --branch")
			os.Exit(2)
		}
		if co.message == "" {
			co.message = defaultCommitMessage(*remove)
		}
	}

	body, err := readBody(*bodyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read issue body:", err)
		os.Exit(2)
	}

	req := intake.Parse(body)

	if *remove {
		runRemoval(req, *requestURL, *registryPath, *jsonOut, co)
		return
	}

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
		var added bool
		apply := func() error {
			reg, err := registry.Load(*registryPath)
			if err != nil {
				return err
			}
			added = reg.Upsert(plan.Entry)
			return reg.Save(*registryPath)
		}
		if co.enabled {
			syncRegistry(*registryPath, apply, co, os.Stdout)
		} else {
			if err := apply(); err != nil {
				fmt.Fprintln(os.Stderr, "update registry:", err)
				os.Exit(2)
			}
			verb := "updated"
			if added {
				verb = "added"
			}
			fmt.Fprintf(os.Stdout, "\nregistry %s: %s\n", verb, plan.Entry.Key())
		}
	}

	if *jsonOut != "" {
		writePlanJSON(plan, *jsonOut)
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

// runRemoval validates an approved removal request, prints the removal plan, and
// (when --registry is given) de-registers the cron from the central catalog. The
// workflow performs the target-repo schedule deletion as the cron-bot App.
func runRemoval(req intake.CronRequest, requestURL, registryPath, jsonOut string, co commitConfig) {
	if errs := req.ValidateRemoval(); len(errs) > 0 {
		fmt.Fprintln(os.Stderr, "Invalid cron removal request:")
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  -", e)
		}
		os.Exit(1)
	}

	plan := cronbot.BuildRemovalPlan(req, requestURL)
	printRemovalPlan(plan, os.Stdout)

	if registryPath != "" {
		var removed bool
		apply := func() error {
			reg, err := registry.Load(registryPath)
			if err != nil {
				return err
			}
			removed = reg.Remove(plan.RegistryKey)
			return reg.Save(registryPath)
		}
		if co.enabled {
			syncRegistry(registryPath, apply, co, os.Stdout)
		} else {
			if err := apply(); err != nil {
				fmt.Fprintln(os.Stderr, "update registry:", err)
				os.Exit(2)
			}
			if removed {
				fmt.Fprintf(os.Stdout, "\nregistry de-registered: %s\n", plan.RegistryKey)
			} else {
				fmt.Fprintf(os.Stdout, "\nregistry: %s was not tracked (nothing to de-register)\n", plan.RegistryKey)
			}
		}
	}

	if jsonOut != "" {
		writePlanJSON(plan, jsonOut)
	}
}

// syncRegistry runs apply against the latest remote tip, then commits and pushes
// registryPath, retrying on a non-fast-forward by re-syncing and re-applying.
func syncRegistry(registryPath string, apply func() error, co commitConfig, w io.Writer) {
	repo := &gitsync.ShellRepo{
		Dir: co.gitDir, Remote: co.remote, Branch: co.branch,
		Name: co.name, Email: co.email,
	}
	res, err := gitsync.Run(repo, apply, gitsync.Options{
		Message:     co.message,
		Paths:       []string{registryPath},
		MaxAttempts: co.maxAttempts,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "commit+push registry:", err)
		os.Exit(2)
	}
	if res.NoOp {
		fmt.Fprintf(w, "\nregistry already up to date; nothing to push (%d attempt(s))\n", res.Attempts)
		return
	}
	fmt.Fprintf(w, "\nregistry committed and pushed (%d attempt(s))\n", res.Attempts)
}

// defaultCommitMessage is used when --commit-push is set without an explicit
// --commit-message.
func defaultCommitMessage(remove bool) string {
	if remove {
		return "chore(cron): de-register cron from registry"
	}
	return "chore(cron): register cron in registry"
}

// writePlanJSON marshals any plan value and writes it to path (with a trailing
// newline).
func writePlanJSON(plan any, path string) {
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal plan:", err)
		os.Exit(2)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write plan:", err)
		os.Exit(2)
	}
}

func printRemovalPlan(p cronbot.RemovalPlan, w io.Writer) {
	fmt.Fprintf(w, "Cron removal: %s :: %s\n", p.Repo, p.Path)
	fmt.Fprintf(w, "  de-register: %s\n", p.RegistryKey)
	fmt.Fprintf(w, "  branch     : %s\n", p.Branch)
	fmt.Fprintf(w, "  merge      : %s\n", p.MergeMethod)
}

func printPlan(p cronbot.Plan, w io.Writer) {
	fmt.Fprintf(w, "Cron request: %s :: %s\n", p.Repo, p.Path)
	fmt.Fprintf(w, "  owner team : %s\n", p.Entry.OwnerTeam)
	fmt.Fprintf(w, "  schedule   : '%s'  (bot edit -> '%s')\n", p.OldExpr, p.NewExpr)
	if !p.CanRewrite {
		fmt.Fprintln(w, "  WARNING: no safe schedule-neutral rewrite; bot edit equals the original")
	}
	fmt.Fprintf(w, "  branch     : %s\n", p.Branch)
	fmt.Fprintf(w, "  merge      : %s (so cron-bot[bot] becomes the actor)\n", p.MergeMethod)
}
