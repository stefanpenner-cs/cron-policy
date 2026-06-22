// Package gitsync commits a working-tree change and pushes it to a remote branch
// under optimistic concurrency. If the push is rejected because the branch moved
// (a concurrent writer won the race), it re-syncs to the new tip, RE-APPLIES the
// change, and retries.
//
// Re-applying — rather than rebasing the rejected commit — means a concurrent
// writer can never produce a textual merge conflict, as long as the change is
// idempotent. The registry Upsert/Remove operations are exactly that, so two
// requests racing to update registry.json both land, in any order, with no lost
// update and no conflict markers.
package gitsync

import (
	"errors"
	"fmt"
	"math/rand"
	"time"
)

// ErrRejected is returned by Repo.Push when the push was rejected because the
// remote branch advanced (a non-fast-forward). It is the signal to retry.
var ErrRejected = errors.New("push rejected: remote branch moved")

// Repo is the minimal git surface the retry loop needs. ShellRepo drives a real
// checkout; tests use a fake.
type Repo interface {
	// SyncToRemote makes the working tree exactly match the current remote
	// branch tip (fetch + hard reset), discarding any local commit or edit.
	SyncToRemote() error
	// Commit stages paths and commits with msg. It reports committed=false when
	// there is nothing to commit (the change was already present upstream).
	Commit(paths []string, msg string) (committed bool, err error)
	// Push pushes HEAD to the remote branch. It returns ErrRejected when the
	// branch moved under it (retryable) and any other error otherwise (fatal).
	Push() error
}

// Result reports how Run finished.
type Result struct {
	Pushed   bool // a new commit was pushed
	NoOp     bool // nothing to commit (the change was already applied upstream)
	Attempts int  // how many sync+apply+commit+push cycles ran
}

// Options configures Run.
type Options struct {
	// Message is the commit message.
	Message string
	// Paths are the files to stage and commit (relative to the repo).
	Paths []string
	// MaxAttempts caps the sync+apply+push cycles. Values < 1 mean 1.
	MaxAttempts int
	// Backoff sleeps before a retry. It is called as Backoff(n) before the
	// (n+1)-th attempt (so Backoff(1) precedes attempt 2). Nil uses a small
	// jittered default; tests pass a no-op.
	Backoff func(retry int)
}

// Run applies a change and pushes it, retrying on a moved branch.
//
// Each cycle: SyncToRemote (fresh tip) -> apply (idempotent mutation) -> Commit
// -> Push. On ErrRejected it loops, up to MaxAttempts. apply must be safe to run
// repeatedly; for the registry it loads the just-synced file, Upserts/Removes,
// and saves.
func Run(repo Repo, apply func() error, opt Options) (Result, error) {
	if opt.MaxAttempts < 1 {
		opt.MaxAttempts = 1
	}
	if opt.Backoff == nil {
		opt.Backoff = defaultBackoff
	}
	var res Result
	for attempt := 1; attempt <= opt.MaxAttempts; attempt++ {
		res.Attempts = attempt
		if attempt > 1 {
			opt.Backoff(attempt - 1)
		}
		if err := repo.SyncToRemote(); err != nil {
			return res, fmt.Errorf("sync to remote: %w", err)
		}
		if err := apply(); err != nil {
			return res, fmt.Errorf("apply change: %w", err)
		}
		committed, err := repo.Commit(opt.Paths, opt.Message)
		if err != nil {
			return res, fmt.Errorf("commit: %w", err)
		}
		if !committed {
			res.NoOp = true
			return res, nil
		}
		switch err := repo.Push(); {
		case err == nil:
			res.Pushed = true
			return res, nil
		case errors.Is(err, ErrRejected):
			// branch moved; loop to re-sync and re-apply onto the new tip
			continue
		default:
			return res, fmt.Errorf("push: %w", err)
		}
	}
	return res, fmt.Errorf("push still rejected after %d attempts: %w", opt.MaxAttempts, ErrRejected)
}

func defaultBackoff(retry int) {
	base := time.Duration(retry) * 100 * time.Millisecond
	jitter := time.Duration(rand.Int63n(int64(100 * time.Millisecond)))
	time.Sleep(base + jitter)
}
