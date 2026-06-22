# cronbot

Make GitHub Actions crons durable, watched, and managed.

## The problem

- A cron's "actor" is just the last person to merge a cron change.
- That person leaves. The cron goes quiet. No one is told.
- No owner. No alert. No health check.

## The fix: four small Go tools

- `rehome` — move a cron onto a durable bot account, without changing when it runs.
- `deadman` — find crons that have gone quiet, and alert.
- `cronguard` — block any human cron change (add, edit, or remove). Only `cron-bot[bot]` may merge one.
- `cronbot` — turn a cron-request issue into a safe, registered plan.

All dry-run or check-only. Nothing here writes to your repos on its own.

## How a cron gets added

1. You file a `cron-request` issue.
2. The bot checks it and comments back.
3. The crew signs off.
4. `cron-bot[bot]` lands it. Now the bot owns it. It is durable.

A human who adds, edits, or deletes a cron directly is stopped by `cronguard`
and sent here.

## How a cron gets removed

There is one way: file a `cron-removal` issue. `cronguard` blocks you from
deleting a managed cron yourself, so removal goes through the same gate as adds.
Stopping a job is a real change, so the crew signs off, then `cron-bot[bot]`
deletes the schedule and de-registers it.

Because every change — add, edit, or remove — goes through the bot, the registry
never drifts from what is really running.

The registry is one shared JSON file, so two requests can land at once. The bot
writes it race-safe: it syncs to the latest, re-applies the change, and pushes —
retrying if someone else pushed first. No lost writes, no merge conflicts.

## Sequence diagrams

How a request flows from issue to durable cron. Same form, same jobs for add and
update; remove has its own form. Every path goes through `cron-bot[bot]`.

### Add

```mermaid
sequenceDiagram
    autonumber
    actor Dev as Developer
    participant Form as cron-request issue
    participant CI as cron-intake workflow
    participant Bot as cronbot
    participant Crew as cron-reviewers
    participant App as cron-bot[bot]
    participant Repo as target repo
    participant Reg as registry.json

    Dev->>Form: open (repo, path, cron, why)
    Form->>CI: on issues -> validate job
    CI->>Bot: --dry-run: Parse + Validate
    alt request is valid
        Bot-->>Form: comment "valid" + plan
        Note over CI,Crew: provision job waits on the<br/>cron-approval environment
        Crew->>CI: approve
        CI->>Bot: --apply
        Bot->>Reg: Upsert (adds a new entry)
        App->>Repo: write cron, merge as the bot
        CI-->>Form: close "Landed by cron-bot[bot]"
    else request is invalid
        Bot-->>Form: comment the errors
        CI-->>Form: check goes red (fix and re-edit)
    end
```

### Update

Same form and jobs as add. Only two things differ, marked below.

```mermaid
sequenceDiagram
    autonumber
    actor Dev as Developer
    participant Form as cron-request issue
    participant CI as cron-intake workflow
    participant Bot as cronbot
    participant Crew as cron-reviewers
    participant App as cron-bot[bot]
    participant Repo as target repo
    participant Reg as registry.json

    Dev->>Form: open (repo, path, NEW cron, why)
    Form->>CI: on issues -> validate job
    CI->>Bot: --dry-run: Parse + Validate
    Bot-->>Form: comment "valid" + plan
    Note over CI,Crew: provision job waits on the<br/>cron-approval environment
    Crew->>CI: approve
    CI->>Bot: --apply
    Bot->>Reg: Upsert (REPLACES the existing entry)
    App->>Repo: CHANGE the existing cron line, merge as the bot
    CI-->>Form: close "Landed by cron-bot[bot]"
```

### Remove

One gated path: the `cron-removal` webform. `cronguard` blocks a human from
deleting a managed cron, so only the bot retires it.

```mermaid
sequenceDiagram
    autonumber
    actor Dev as Developer
    participant Form as cron-removal issue
    participant CI as cron-intake workflow
    participant Bot as cronbot
    participant Crew as cron-reviewers
    participant App as cron-bot[bot]
    participant Repo as target repo
    participant Reg as registry.json

    Dev->>Form: open (repo, path, reason)
    Form->>CI: on issues -> validate-removal job
    CI->>Bot: --remove --dry-run: ValidateRemoval
    Bot-->>Form: comment "valid" + plan
    Note over CI,Crew: provision-removal waits on the<br/>cron-approval environment
    Crew->>CI: approve
    CI->>Bot: --remove --apply
    Bot->>Reg: Remove(key)
    App->>Repo: delete the schedule line, merge as the bot
    CI-->>Form: close "Retired by cron-bot[bot]"
    Note over Dev,Repo: a human deleting the line directly is<br/>blocked by cronguard and sent to this form
```


## See it live

Prototype: https://github.com/stefanpenner-cs/cronbot

- Issue #1 (good): the bot says "valid" and shows the plan.
- Issue #2 (bad): the check goes red and the bot lists the errors.

## Run it

From the repo root:

```
go test ./...                 # all tests
go run ./cmd/deadman          # quiet-cron report
go run ./cmd/rehome           # re-home plan (dry-run)
go run ./cmd/cronbot --issue-body issue.md --request-url URL
go run ./cmd/cronguard --actor "$PR_AUTHOR" --base origin/main path/to/workflow.yml
```

## More

- `ci/README.md` — how to roll this out across an org or the whole enterprise.
- Owning team is fixed (`cron-reviewers`), not a form field.
- Cadence is read from the cron value, so it is not stored twice.
