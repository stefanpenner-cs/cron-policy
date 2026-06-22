# Enterprise rollout — li-cron cron policy

How to make crons managed and durable across the enterprise.

## The model

- Humans never merge cron changes directly.
- A required check (the identity gate) blocks any human cron change.
- To get a cron, you file an issue. The crew signs off. li-cron[bot] lands it.
- Because the bot does the merge, the bot owns the cron. It is durable.
- A central registry records every managed cron.

## Where each piece lives

- **The tool, the intake flow, and the registry** → this repo (`cron-policy`).
  It holds the Go module, the live issue form
  (`.github/ISSUE_TEMPLATE/cron-request.yml`), the live intake workflow
  (`.github/workflows/cron-intake.yml`), and `registry.json`.
- **The identity-gate check** → shipped to each target repo and registered as a
  required workflow per org (see the files in this `ci/` folder).
- **The approval gate** → a GitHub Environment (`cron-approval`) whose required
  reviewers are the crew that releases ci-workflows/ci-actions.

## The intake flow lives in `.github/` (this repo)

- `.github/ISSUE_TEMPLATE/cron-request.yml` — the request form. Fields: target
  repository, workflow path, cron expression, justification. Labels match what
  cronbot parses. The owning team is fixed policy (not a field); cadence is read
  from the cron expression (not a field).
- `.github/workflows/cron-intake.yml` — the intake workflow. Validates the
  request and comments back, waits for crew sign-off (environment gate), updates
  the registry, and lands the change as the li-cron App.

## The files in this `ci/` folder (ship to target repos)

- `required-workflow.yml` — the identity gate. Orgs register THIS as a required
  workflow. It fails any PR that adds/changes a cron unless the author is
  li-cron[bot].
- `org-ruleset.example.json` — the org ruleset that makes the check required.
- `action.yml`, `cron-registry.example.txt` — the older registry-based lint.
  Optional. Keep it if you also want a per-repo allow-list.

## The flow

```
person files "cron-request" issue
   -> .github/workflows/cron-intake.yml
        validate : cronbot checks the request, comments back
        provision: WAIT for crew sign-off (cron-approval environment)
                   cronbot updates registry.json
                   li-cron App lands the cron in the target repo (squash merge)
                   close the issue
```

A human who edits a cron directly hits the identity gate and is sent here.

## Rollout steps

1. This repo IS the intake repo. The form and intake workflow are already live
   in `.github/`. Ship `ci/required-workflow.yml` to each target repo at
   `.github/workflows/cron-policy.yml`.
2. Create the `cron-approval` Environment. Set required reviewers = the crew.
3. Create the li-cron GitHub App: contents:write, pull_requests:write,
   workflows:write. Install it org-wide. Give it branch-protection bypass so
   its PRs can merge. Store `LI_CRON_APP_ID` (var) and `LI_CRON_APP_KEY`
   (secret). The provision job stays skipped until `LI_CRON_APP_ID` is set.
4. In each org, create the ruleset from `org-ruleset.example.json` (set
   `repository_id` to the target repo). `POST /orgs/{org}/rulesets`.
5. Enterprise-wide = apply that ruleset to every org (scriptable loop).

## What is code vs infra

- Code (in this module, tested): cronbot (request -> plan + registry),
  cronguard (the identity gate), the registry catalog.
- Infra (you set up): the li-cron App, the cron-approval Environment, the org
  rulesets.

## Honest limits

- The identity gate uses the PR author as the actor. Good enough; a later pass
  can check the commit author.
- The merge must be squash or merge-commit, never rebase, or the bot does not
  become the actor.
- One landing step in `.github/workflows/cron-intake.yml` is a marked TODO:
  choose to merge the developer's existing PR as the App (simplest), or have the
  App author the workflow. Pick one for your environment.
- A repo admin with bypass can still force a cron in. deadman + rehome catch it.
