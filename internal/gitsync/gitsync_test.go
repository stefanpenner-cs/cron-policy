package gitsync

import (
	"errors"
	"strings"
	"testing"
)

// fakeRepo records the order of calls and lets a test script the results of
// each method, so the retry loop can be exercised deterministically.
type fakeRepo struct {
	log []string

	syncErr   error
	commitErr error
	committed bool // what Commit reports (when commitErr is nil)

	pushResults []error // consumed one per Push call
	pushCalls   int
}

func (f *fakeRepo) SyncToRemote() error {
	f.log = append(f.log, "sync")
	return f.syncErr
}

func (f *fakeRepo) Commit(paths []string, msg string) (bool, error) {
	f.log = append(f.log, "commit")
	if f.commitErr != nil {
		return false, f.commitErr
	}
	return f.committed, nil
}

func (f *fakeRepo) Push() error {
	f.log = append(f.log, "push")
	var err error
	if f.pushCalls < len(f.pushResults) {
		err = f.pushResults[f.pushCalls]
	}
	f.pushCalls++
	return err
}

func noBackoff(int) {}

func TestRunHappyPathPushesOnFirstAttempt(t *testing.T) {
	f := &fakeRepo{committed: true, pushResults: []error{nil}}
	applies := 0
	res, err := Run(f, func() error { applies++; return nil }, Options{
		Message: "m", Paths: []string{"registry.json"}, MaxAttempts: 5, Backoff: noBackoff,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Pushed || res.NoOp || res.Attempts != 1 {
		t.Fatalf("want pushed in 1 attempt, got %+v", res)
	}
	if applies != 1 {
		t.Fatalf("apply should run once, ran %d", applies)
	}
	if got := strings.Join(f.log, ","); got != "sync,commit,push" {
		t.Fatalf("want sync,commit,push; got %s", got)
	}
}

func TestRunReAppliesAfterRejectionThenSucceeds(t *testing.T) {
	// Reject twice, then accept: the loop must re-sync and re-apply each time.
	f := &fakeRepo{committed: true, pushResults: []error{ErrRejected, ErrRejected, nil}}
	applies := 0
	res, err := Run(f, func() error { applies++; return nil }, Options{MaxAttempts: 5, Backoff: noBackoff})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Pushed || res.Attempts != 3 {
		t.Fatalf("want pushed on attempt 3, got %+v", res)
	}
	if applies != 3 {
		t.Fatalf("apply should re-run once per attempt (3), ran %d", applies)
	}
	if got := strings.Join(f.log, ","); got != "sync,commit,push,sync,commit,push,sync,commit,push" {
		t.Fatalf("each attempt must sync before apply/commit/push; got %s", got)
	}
}

func TestRunGivesUpAfterMaxAttempts(t *testing.T) {
	f := &fakeRepo{committed: true, pushResults: []error{ErrRejected, ErrRejected, ErrRejected}}
	res, err := Run(f, func() error { return nil }, Options{MaxAttempts: 3, Backoff: noBackoff})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("want ErrRejected after exhausting attempts, got %v", err)
	}
	if res.Attempts != 3 || res.Pushed {
		t.Fatalf("want 3 attempts and not pushed, got %+v", res)
	}
	if f.pushCalls != 3 {
		t.Fatalf("want 3 push attempts, got %d", f.pushCalls)
	}
}

func TestRunNoOpWhenNothingToCommit(t *testing.T) {
	// Commit reports nothing staged (change already upstream): no push, success.
	f := &fakeRepo{committed: false}
	res, err := Run(f, func() error { return nil }, Options{MaxAttempts: 5, Backoff: noBackoff})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.NoOp || res.Pushed || res.Attempts != 1 {
		t.Fatalf("want a no-op in 1 attempt, got %+v", res)
	}
	if f.pushCalls != 0 {
		t.Fatalf("a no-op must not push, pushed %d times", f.pushCalls)
	}
}

func TestRunFatalPushErrorDoesNotRetry(t *testing.T) {
	boom := errors.New("remote rejected: protected branch")
	f := &fakeRepo{committed: true, pushResults: []error{boom}}
	res, err := Run(f, func() error { return nil }, Options{MaxAttempts: 5, Backoff: noBackoff})
	if err == nil || errors.Is(err, ErrRejected) {
		t.Fatalf("want the fatal error surfaced, got %v", err)
	}
	if !strings.Contains(err.Error(), "protected branch") {
		t.Fatalf("want the underlying message, got %v", err)
	}
	if res.Attempts != 1 || f.pushCalls != 1 {
		t.Fatalf("a fatal push error must not retry, got attempts=%d pushes=%d", res.Attempts, f.pushCalls)
	}
}

func TestRunSurfacesApplyError(t *testing.T) {
	f := &fakeRepo{committed: true}
	want := errors.New("disk full")
	_, err := Run(f, func() error { return want }, Options{MaxAttempts: 5, Backoff: noBackoff})
	if !errors.Is(err, want) {
		t.Fatalf("want apply error wrapped, got %v", err)
	}
	if got := strings.Join(f.log, ","); got != "sync" {
		t.Fatalf("apply runs after sync and before commit; got %s", got)
	}
}

func TestRunSurfacesSyncError(t *testing.T) {
	f := &fakeRepo{syncErr: errors.New("network down")}
	applies := 0
	_, err := Run(f, func() error { applies++; return nil }, Options{MaxAttempts: 5, Backoff: noBackoff})
	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("want sync error surfaced, got %v", err)
	}
	if applies != 0 {
		t.Fatalf("apply must not run when sync fails")
	}
}

func TestRunBackoffCalledBetweenAttempts(t *testing.T) {
	f := &fakeRepo{committed: true, pushResults: []error{ErrRejected, nil}}
	var retries []int
	_, err := Run(f, func() error { return nil }, Options{
		MaxAttempts: 5,
		Backoff:     func(r int) { retries = append(retries, r) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(retries) != 1 || retries[0] != 1 {
		t.Fatalf("want one backoff before attempt 2 with retry=1, got %v", retries)
	}
}

func TestRunZeroMaxAttemptsMeansOne(t *testing.T) {
	f := &fakeRepo{committed: true, pushResults: []error{nil}}
	res, err := Run(f, func() error { return nil }, Options{MaxAttempts: 0, Backoff: noBackoff})
	if err != nil || res.Attempts != 1 || !res.Pushed {
		t.Fatalf("want a single successful attempt, got %+v err=%v", res, err)
	}
}
