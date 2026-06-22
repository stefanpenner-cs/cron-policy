package gitsync

import (
	"os/exec"
	"path/filepath"
	"testing"

	"fixcron/internal/registry"
)

// --- real-git integration tests ------------------------------------------------

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s failed: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

const (
	tName  = "cron-bot[bot]"
	tEmail = "cron-bot[bot]@users.noreply.github.com"
)

// bareRemoteWithEmptyRegistry creates a bare repo whose main branch holds an
// empty registry.json, and returns its path.
func bareRemoteWithEmptyRegistry(t *testing.T) string {
	t.Helper()
	remote := t.TempDir()
	mustGit(t, remote, "init", "--bare", "-b", "main", ".")

	seed := t.TempDir()
	mustGit(t, seed, "clone", remote, ".")
	mustGit(t, seed, "checkout", "-b", "main")
	if err := (&registry.Registry{}).Save(filepath.Join(seed, "registry.json")); err != nil {
		t.Fatal(err)
	}
	mustGit(t, seed, "add", "registry.json")
	mustGit(t, seed, "-c", "user.name="+tName, "-c", "user.email="+tEmail, "commit", "-m", "init registry")
	mustGit(t, seed, "push", "origin", "main")
	return remote
}

func cloneOf(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "clone", remote, ".")
	return dir
}

func entry(repo string) registry.Entry {
	return registry.Entry{
		Repo: repo, Path: ".github/workflows/stale.yml",
		Expr: "0 9 * * *", OwnerTeam: "cron-reviewers",
		Request: "https://example.com/issues/1",
	}
}

// upsertApply returns an idempotent apply that adds e to the registry file.
func upsertApply(path string, e registry.Entry) func() error {
	return func() error {
		reg, err := registry.Load(path)
		if err != nil {
			return err
		}
		reg.Upsert(e)
		return reg.Save(path)
	}
}

// remoteEntries reads the registry.json on the remote's main branch.
func remoteEntries(t *testing.T, remote string) []registry.Entry {
	t.Helper()
	reg, err := registry.Load(filepath.Join(cloneOf(t, remote), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	return reg.Entries
}

func shellRepo(dir string) *ShellRepo {
	return &ShellRepo{Dir: dir, Remote: "origin", Branch: "main", Name: tName, Email: tEmail}
}

func TestShellRepoPushesCleanlyWhenUncontended(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	remote := bareRemoteWithEmptyRegistry(t)
	work := cloneOf(t, remote)

	res, err := Run(shellRepo(work), upsertApply(filepath.Join(work, "registry.json"), entry("octo-org/foo")),
		Options{Message: "add foo", Paths: []string{"registry.json"}, MaxAttempts: 5, Backoff: noBackoff})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Pushed || res.Attempts != 1 {
		t.Fatalf("want a clean push in 1 attempt, got %+v", res)
	}
	if got := remoteEntries(t, remote); len(got) != 1 || got[0].Repo != "octo-org/foo" {
		t.Fatalf("remote should have the one entry, got %#v", got)
	}
}

func TestShellRepoReRunIsNoOp(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	remote := bareRemoteWithEmptyRegistry(t)
	work := cloneOf(t, remote)
	path := filepath.Join(work, "registry.json")
	e := entry("octo-org/foo")

	if _, err := Run(shellRepo(work), upsertApply(path, e),
		Options{Message: "add foo", Paths: []string{"registry.json"}, MaxAttempts: 5, Backoff: noBackoff}); err != nil {
		t.Fatal(err)
	}
	headBefore := mustGit(t, work, "rev-parse", "origin/main")

	// Applying the same entry again must change nothing: no commit, no push.
	res, err := Run(shellRepo(work), upsertApply(path, e),
		Options{Message: "add foo again", Paths: []string{"registry.json"}, MaxAttempts: 5, Backoff: noBackoff})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.NoOp || res.Pushed {
		t.Fatalf("re-applying the same entry should be a no-op, got %+v", res)
	}
	if headAfter := mustGit(t, work, "rev-parse", "origin/main"); headAfter != headBefore {
		t.Fatalf("remote head must not move on a no-op: %s -> %s", headBefore, headAfter)
	}
}

// racingRepo advances the remote once, immediately before the first push, to
// deterministically force a real non-fast-forward rejection.
type racingRepo struct {
	*ShellRepo
	before func()
	done   bool
}

func (r *racingRepo) Push() error {
	if !r.done {
		r.done = true
		r.before()
	}
	return r.ShellRepo.Push()
}

func TestShellRepoRecoversFromRealNonFastForward(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	remote := bareRemoteWithEmptyRegistry(t)
	work := cloneOf(t, remote)
	path := filepath.Join(work, "registry.json")

	// Just before our first push, a competing writer lands entry X on main, so
	// our push is rejected and the loop must re-sync, re-apply Y, and retry.
	racer := &racingRepo{ShellRepo: shellRepo(work), before: func() {
		other := cloneOf(t, remote)
		if err := upsertApply(filepath.Join(other, "registry.json"), entry("octo-org/x"))(); err != nil {
			t.Fatal(err)
		}
		mustGit(t, other, "add", "registry.json")
		mustGit(t, other, "-c", "user.name="+tName, "-c", "user.email="+tEmail, "commit", "-m", "add x")
		mustGit(t, other, "push", "origin", "main")
	}}

	res, err := Run(racer, upsertApply(path, entry("octo-org/y")),
		Options{Message: "add y", Paths: []string{"registry.json"}, MaxAttempts: 5, Backoff: noBackoff})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Pushed || res.Attempts != 2 {
		t.Fatalf("want recovery on the 2nd attempt, got %+v", res)
	}

	// Both writers' entries must survive — no lost update, no conflict.
	got := remoteEntries(t, remote)
	keys := map[string]bool{}
	for _, e := range got {
		keys[e.Repo] = true
	}
	if len(got) != 2 || !keys["octo-org/x"] || !keys["octo-org/y"] {
		t.Fatalf("both x and y should be present, got %#v", got)
	}
}
