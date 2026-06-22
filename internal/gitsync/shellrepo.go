package gitsync

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// ShellRepo implements Repo by shelling out to git in a checkout directory. It
// is the production driver used by cmd/cronbot in the intake workflow.
type ShellRepo struct {
	Dir    string // working tree to run git in
	Remote string // e.g. "origin"
	Branch string // e.g. "main"
	Name   string // commit author/committer name (optional)
	Email  string // commit author/committer email (optional)
}

func (r *ShellRepo) git(args ...string) (stdout, stderr string, err error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	return out.String(), errb.String(), err
}

// SyncToRemote fetches the remote branch and hard-resets the working tree to it,
// discarding any local commit or edit so the next apply runs on the fresh tip.
func (r *ShellRepo) SyncToRemote() error {
	if _, e, err := r.git("fetch", r.Remote, r.Branch); err != nil {
		return fmt.Errorf("fetch %s %s: %w: %s", r.Remote, r.Branch, err, strings.TrimSpace(e))
	}
	if _, e, err := r.git("reset", "--hard", "FETCH_HEAD"); err != nil {
		return fmt.Errorf("reset --hard FETCH_HEAD: %w: %s", err, strings.TrimSpace(e))
	}
	return nil
}

// Commit stages paths and commits. It returns committed=false when nothing is
// staged (the change already matches upstream), so the loop can stop without an
// empty commit or a pointless push.
func (r *ShellRepo) Commit(paths []string, msg string) (bool, error) {
	if _, e, err := r.git(append([]string{"add", "--"}, paths...)...); err != nil {
		return false, fmt.Errorf("add: %w: %s", err, strings.TrimSpace(e))
	}
	// `git diff --cached --quiet` exits 0 when nothing is staged, 1 otherwise.
	if _, _, err := r.git("diff", "--cached", "--quiet"); err == nil {
		return false, nil
	}
	args := []string{}
	if r.Name != "" {
		args = append(args, "-c", "user.name="+r.Name)
	}
	if r.Email != "" {
		args = append(args, "-c", "user.email="+r.Email)
	}
	args = append(args, "commit", "-m", msg)
	if _, e, err := r.git(args...); err != nil {
		return false, fmt.Errorf("commit: %w: %s", err, strings.TrimSpace(e))
	}
	return true, nil
}

// Push pushes HEAD to the remote branch, mapping a non-fast-forward rejection to
// ErrRejected (retryable) and anything else (auth, protected branch, ...) to a
// fatal error.
func (r *ShellRepo) Push() error {
	_, e, err := r.git("push", r.Remote, "HEAD:"+r.Branch)
	if err == nil {
		return nil
	}
	if isNonFastForward(e) {
		return ErrRejected
	}
	return fmt.Errorf("git push: %w: %s", err, strings.TrimSpace(e))
}

func isNonFastForward(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "non-fast-forward") ||
		strings.Contains(s, "fetch first") ||
		strings.Contains(s, "[rejected]")
}
