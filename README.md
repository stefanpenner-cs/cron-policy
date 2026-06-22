# fix-cron — fix GHA cron rot

A set of small, tested Go tools plus this design doc.

They make GitHub Actions crons durable, observable, and managed.

Dry-run / CI-template only. Nothing here writes to any repo.

## The problem

GHA crons rot in four ways:

- They are hard to maintain.
- The cron "actor" is just the last person to merge a cron-syntax change.
- That person leaves the company, and the cron silently stops.
- There is no alerting, no deadman switch, no health check.

## Root cause (already proven)

Source: `github.com/stefanpenner-cs/cron-debugging`, `../who_owns_cron.md`.

- A scheduled workflow runs as the push actor of the last change to a
  `cron:` value.
  - Squash / merge-commit → the merger.
  - Rebase merge → the original commit author.
  - Direct push → the pusher.
- Only a deleted or deprovisioned (EMU-anonymized) actor stops crons.
  Removing the user from the org does not.
- A comment or whitespace edit does not move the actor. The `cron:`
  value itself must change.
- A cron change does not re-enable a disabled workflow.

## The fix: four pillars

### Pillar A — durable ownership (`cmd/rehome`)

Move a fragile cron onto a durable service/bot account.

The trick: edit the `cron:` value in a way that does not change when it
runs. `internal/cronequiv` rewrites the expression to an equal-but-different
string (for example `0 9 * * *` → `0 9 * 1-12 *`). That real edit moves the
actor; the schedule stays identical.

`cmd/rehome`:

- Reads the cron inventory and the last actual `schedule` run per file.
- Picks crons whose run-actor needs re-homing
  (deprovisioned, personal, or external).
- Emits the exact planned edit: old → new expression, and whether the
  workflow must be re-enabled.
- Sorts worst-first (deprovisioned before human).
- Dry-run only. No commits, no pushes, no API writes.

To actually move the actor later, land each edit as a durable account
(svc-* or `li-cron[bot]`) via squash or merge-commit. Never rebase-merge —
that keeps the old author.

### Pillar B — observability (`cmd/deadman`)

Catch crons that have silently stopped.

`cmd/deadman`:

- Reads the inventory and the last `schedule` run per file.
- Compares expected cadence (from the cron expression) against days since
  the last real run.
- Flags two failure states:
  - `never_fired` — no scheduled run on record.
  - `stale` — missed roughly three expected fires (floored at 14 days).
- Prints a worst-first table and can write JSON.
- The alert step is one `Emit()` function, so a Slack or GitHub-issue sink
  can drop in later.

### Pillar C — prevention (`cmd/cronlint`)

Stop new unmanaged crons from landing at all.

`cmd/cronlint` is a CI-time check. It reads workflow files (the changed
files in a PR, or a whole tree with `--dir`) and fails on crons that break
policy:

- default — a cron-bearing workflow must be in the registry (owner on
  record) or the allow-list.
- `--ban-all` — every cron is rejected unless the file is allow-listed.

What a lint can and cannot do:

- It cannot guarantee a durable owner. A cron's actor is set at merge time
  by whoever pushes the cron-syntax change. That identity is not in the PR
  diff, so no lint can know it.
- It can force every cron to be registered or allow-listed, and fail the
  rest. That is the prevention half.

### Pillar D — managed ownership (the intake bot)

Built, not just described. Three new pieces turn "only li-cron owns crons"
into a real flow:

- **Identity gate (`cmd/cronguard`).** The enterprise required check. It diffs
  each changed workflow against the base and fails any PR that adds or changes
  a cron value unless the PR author is `li-cron[bot]`. Humans are forced
  through intake. (Removing a cron is always allowed.)
- **Intake bot (`cmd/cronbot` + `.github/workflows/cron-intake.yml`).** A person
  files a `cron-request` issue form. `cronbot` parses and validates it, and the
  workflow comments the result back on the issue. The crew signs off via a
  GitHub Environment gate. Then `cronbot` updates the central registry and the
  li-cron App lands the change (squash merge), so the bot is the actor.
- **Central registry (`internal/registry`, `registry.json`).** The catalog of
  every managed cron: repo, path, schedule, owner team, request link. The
  source of truth that deadman/rehome consume. The owning team is fixed policy
  (`cronbot.OwnerTeam`, not a request field). Cadence is not stored — the cron
  expression already encodes it.

Why an identity gate beats a path rule: there is no native GitHub rule that
says "PRs touching a `cron:` must be merged by li-cron." Merger-identity rules
cannot key off changed paths. So we gate on the PR author instead, and make the
intake bot the only way to get a bot-authored cron change.

Backstop: a repo admin with bypass can still force a cron in. `cmd/deadman` +
`cmd/rehome` sweep anything that slips.

Enterprise rollout (one shared tool repo, central registry, the intake flow,
one ruleset per org) is spelled out in `ci/README.md`. The intake flow lives in
this repo at `.github/workflows/cron-intake.yml` and
`.github/ISSUE_TEMPLATE/cron-request.yml`. The identity gate ships to each
target repo via `ci/required-workflow.yml` + `ci/org-ruleset.example.json`.

The earlier `cmd/cronlint` registry/allow-list check is retained as an optional
per-repo lint.

## Data flow

Both tools reuse the existing pipeline. They fetch nothing new.

```
scripts/cron_inventory.py   ->  data/cron/linkedin-actions/crons.json
scripts/cron_last_runs.py   ->  data/cron/linkedin-actions/last_runs.json
                                        |
                 +----------------------+----------------------+
                 v                                             v
            cmd/deadman                                    cmd/rehome
        (missed / dead report)                        (dry-run re-home plan)
```

## Layout

```
cron-policy/
  go.mod                       module fixcron
  internal/
    cronsched/   cadence + staleness (IntervalDays, FiringLabel, Health)
    cronequiv/   schedule-neutral cron rewriter (Rewrite)
    actor/       run-actor durability classifier (Class, NeedsRehome)
    inventory/   crons.json / last_runs.json types + loaders
    deadman/     deadman assessment (CollapseFiles, Assess, Missed, Emit)
    rehome/      dry-run re-home planner (Plan, Emit)
    cronlint/    prevention lint (ParseCrons, Lint, Config, Emit)
    cronguard/   identity gate: bot-only cron changes (Guard, Emit)
    registry/    central cron catalog (Entry, Load, Upsert, Save, Validate)
    intake/      issue-form -> validated CronRequest (Parse, Validate)
    cronbot/     intake brain: request -> provisioning Plan (BuildPlan)
  cmd/
    deadman/     deadman CLI
    rehome/      re-home CLI
    cronlint/    prevention-lint CLI
    cronguard/   identity-gate required check
    cronbot/     intake brain CLI (validate + plan + registry upsert)
  .github/
    ISSUE_TEMPLATE/cron-request.yml   live intake form
    workflows/cron-intake.yml         live intake flow (validate + provision)
  ci/            rollout kit for target repos (identity gate, org ruleset)
```

Each package has a sibling `*_test.go`. Go's built-in `testing`.

## Run it

Run from the repo root:

```
go run ./cmd/deadman
go run ./cmd/deadman --json-out ../reports/cron/deadman.json

go run ./cmd/rehome
go run ./cmd/rehome --json-out ../reports/cron/rehome_plan.json

go run ./cmd/cronlint --registry cron-registry.txt path/to/.github/workflows/foo.yml
go run ./cmd/cronlint --ban-all --allow 'vendor/**' --dir ..
go run ./cmd/cronlint --list-touched --dir ..

# identity gate (required check): block non-bot cron changes
go run ./cmd/cronguard --actor "$PR_AUTHOR" --base origin/main path/to/.github/workflows/foo.yml

# intake brain: validate a request, plan it, upsert the central registry
go run ./cmd/cronbot --issue-body issue.md --request-url URL --registry registry.json

go test ./...
```

The deadman and rehome tools default to
`../data/cron/linkedin-actions/{crons,last_runs}.json`.
Override with `--crons` / `--last-runs`.

`cronlint` takes workflow file paths as arguments (for example
`git diff --name-only` output) or walks a tree with `--dir`. It exits 1 on
violations, so it works as a required CI check.

## Safety

- Re-home is dry-run. It only prints the planned edit.
- The rewrite never changes when a cron fires. It only changes the string.
- Day-of-month and day-of-week are a union when both are set, so the
  rewriter never expands one while the other is restricted.

## Future work (out of scope for this prototype)

- Apply mode: open a draft PR per repo for the re-home edit.
- Live landing step in `.github/workflows/cron-intake.yml`: have the li-cron App
  merge the developer's PR (or author the workflow). Needs an App token + a
  target repo, so it stays a marked TODO in the workflow.
- Identity gate: check the commit author, not just the PR author.
- Real alert sinks: Slack, GitHub issues.
