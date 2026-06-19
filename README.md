# fix-cron — fix GHA cron rot

Three small, tested Go tools plus this design doc.

They make GitHub Actions crons durable and observable.

Dry-run only. Nothing here writes to any repo.

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

## The fix: two pillars

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

- default — a cron-bearing workflow must be in the registry (owner +
  cadence on record) or the allow-list.
- `--ban-all` — every cron is rejected unless the file is allow-listed.

What a lint can and cannot do:

- It cannot guarantee a durable owner. A cron's actor is set at merge time
  by whoever pushes the cron-syntax change. That identity is not in the PR
  diff, so no lint can know it.
- It can force every cron to be registered or allow-listed, and fail the
  rest. That is the prevention half.

#### Ensuring only `li-cron` merges cron changes

There is no native GitHub rule that says "PRs touching a `cron:` must be
merged by li-cron." Merger-identity rules cannot key off changed paths. You
get there by stacking:

1. Lock the branch (native, broad). Branch protection → restrict who can
   push to the default branch → only the li-cron app. Every merge is the
   bot, so every cron becomes bot-owned. Cost: all merges go through the bot.
2. Required check + bot-merge (native, targeted). Run `cronlint` as a
   required status check so humans cannot merge a cron PR (red check). The
   li-cron app is the only allowed merger and lands the squash, so the bot
   is the actor. Use `cronlint --list-touched` to tell that bot-merge which
   PRs touch a cron. Catch: a repo admin with bypass can still override.
3. Backstop (eventual). `cmd/deadman` + `cmd/rehome` sweep anything that
   slips: admin bypass, repos not yet on the policy.

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
fix-cron/
  go.mod                       module fixcron
  internal/
    cronsched/   cadence + staleness (IntervalDays, FiringLabel, Health)
    cronequiv/   schedule-neutral cron rewriter (Rewrite)
    actor/       run-actor durability classifier (Class, NeedsRehome)
    inventory/   crons.json / last_runs.json types + loaders
    deadman/     deadman assessment (CollapseFiles, Assess, Missed, Emit)
    rehome/      dry-run re-home planner (Plan, Emit)
    cronlint/    prevention lint (ParseCrons, Lint, Config, Emit)
  cmd/
    deadman/     deadman CLI
    rehome/      re-home CLI
    cronlint/    prevention-lint CLI
```

Each package has a sibling `*_test.go`. Go's built-in `testing`.

## Run it

Run from this `fix-cron/` directory:

```
go run ./cmd/deadman
go run ./cmd/deadman --json-out ../reports/cron/deadman.json

go run ./cmd/rehome
go run ./cmd/rehome --json-out ../reports/cron/rehome_plan.json

go run ./cmd/cronlint --registry cron-registry.txt path/to/.github/workflows/foo.yml
go run ./cmd/cronlint --ban-all --allow 'vendor/**' --dir ..
go run ./cmd/cronlint --list-touched --dir ..

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
- A li-cron bot-merge action that consumes `cronlint --list-touched` and
  lands cron PRs as the durable account.
- Real alert sinks: Slack, GitHub issues.
- A cron registry: expected cadence and owning team as the source of truth.
