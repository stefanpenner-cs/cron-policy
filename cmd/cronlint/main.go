// Command cronlint is the PREVENTION pillar: a CI-time check that fails when a
// scheduled workflow is added without being registered or allow-listed.
//
// It reads workflow files from positional arguments (e.g. the changed files in a
// PR, via `git diff --name-only`) or, with --dir, by walking a tree for
// .github/workflows/*.y{a,}ml. Paths are matched against the registry and
// allow-list as given (so pass repo-relative paths).
//
// Policies:
//
//	cronlint --registry cron-registry.txt .github/workflows/foo.yml   # default
//	cronlint --ban-all --allow 'vendor/**' --dir .                     # ban all
//
// What this can and cannot do. A cron's actor is set at MERGE time by whoever
// pushes the cron-syntax change — not knowable from a diff. So this lint cannot
// guarantee a durable owner; it forces every cron to be registered or
// allow-listed and fails the rest. Make it a REQUIRED status check, land cron
// changes via a li-cron bot-merge (so the bot is the actor), and keep
// deadman/rehome as the backstop. Use --list-touched to feed that bot-merge the
// set of cron-bearing files in a PR.
//
// Exit codes: 0 = clean, 1 = violations, 2 = usage/IO error.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"fixcron/internal/cronlint"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	banAll := flag.Bool("ban-all", false, "reject every cron (allow-list still exempts)")
	registryPath := flag.String("registry", "", "newline-delimited file of permitted workflow paths (# comments allowed)")
	dir := flag.String("dir", "", "walk this tree for .github/workflows/*.y{a,}ml instead of taking file args")
	jsonOut := flag.String("json-out", "", "write violations as JSON to this path")
	listTouched := flag.Bool("list-touched", false, "print cron-bearing files (one per line) and exit 0; the li-cron bot-merge signal")
	var allow stringList
	flag.Var(&allow, "allow", "glob of exempt paths; repeatable (** spans directories)")
	flag.Parse()

	paths, err := collectPaths(*dir, flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, "collect files:", err)
		os.Exit(2)
	}

	files, err := readFiles(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read files:", err)
		os.Exit(2)
	}

	if *listTouched {
		for _, f := range files {
			if len(cronlint.ParseCrons(f.Content)) > 0 {
				fmt.Println(f.Path)
			}
		}
		return
	}

	registry, err := loadRegistry(*registryPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load registry:", err)
		os.Exit(2)
	}

	v := cronlint.Lint(files, cronlint.Config{BanAll: *banAll, Allow: allow, Registry: registry})
	n := cronlint.Emit(v, os.Stdout)

	if *jsonOut != "" {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "marshal:", err)
			os.Exit(2)
		}
		if err := os.WriteFile(*jsonOut, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write:", err)
			os.Exit(2)
		}
	}

	if n > 0 {
		os.Exit(1)
	}
}

// fileRef pairs a file's on-disk location with the logical path used for
// registry/allow matching (repo-relative).
type fileRef struct {
	disk    string
	logical string
}

// collectPaths returns the workflow files to lint: explicit args, or a walk of
// dir for .github/workflows/*.y{a,}ml (logical paths made relative to dir).
func collectPaths(dir string, args []string) ([]fileRef, error) {
	if dir == "" {
		out := make([]fileRef, 0, len(args))
		for _, a := range args {
			out = append(out, fileRef{disk: a, logical: filepath.ToSlash(a)})
		}
		return out, nil
	}
	var out []fileRef
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(p)
		if ext != ".yml" && ext != ".yaml" {
			return nil
		}
		if !strings.Contains(filepath.ToSlash(p), ".github/workflows/") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out = append(out, fileRef{disk: p, logical: filepath.ToSlash(rel)})
		return nil
	})
	return out, err
}

func readFiles(refs []fileRef) ([]cronlint.WorkflowFile, error) {
	out := make([]cronlint.WorkflowFile, 0, len(refs))
	for _, r := range refs {
		b, err := os.ReadFile(r.disk)
		if err != nil {
			return nil, err
		}
		out = append(out, cronlint.WorkflowFile{Path: r.logical, Content: string(b)})
	}
	return out, nil
}

// loadRegistry reads a newline-delimited set of permitted workflow paths,
// ignoring blank lines and # comments.
func loadRegistry(path string) (map[string]bool, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[filepath.ToSlash(line)] = true
	}
	return out, nil
}
