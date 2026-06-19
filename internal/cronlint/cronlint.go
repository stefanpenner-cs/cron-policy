// Package cronlint is the PREVENTION pillar: a CI-time check that stops new
// unmanaged scheduled workflows from landing in the first place.
//
// Why a lint can't enforce ownership. A scheduled workflow's actor is decided
// at MERGE time by who pushes the cron-syntax change — that identity is not
// knowable from a PR diff. So this lint cannot guarantee a durable owner. What
// it CAN do is force every cron to be registered (owner + cadence on record) or
// explicitly allow-listed, and fail anything else. Durable ownership is then
// enforced merge-side (a li-cron bot-merge), with deadman/rehome as the backstop.
//
// Two policies:
//   - default: a cron-bearing workflow must be in the registry or allow-list.
//   - BanAll:  every cron is rejected unless the file is allow-listed.
package cronlint

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
)

// cronLine matches a YAML "- cron:" entry, capturing the right-hand value.
// A leading "#" comment line never matches (the "-" must come first).
var cronLine = regexp.MustCompile(`^\s*-\s*cron\s*:\s*(.+?)\s*$`)

// CronRef is one cron entry found in a workflow file (1-based line number).
type CronRef struct {
	Line int
	Expr string
}

// WorkflowFile is a workflow file to lint.
type WorkflowFile struct {
	Path    string
	Content string
}

// Config selects the policy.
type Config struct {
	BanAll   bool            // reject every cron (allow-list still exempts)
	Allow    []string        // glob patterns of exempt file paths
	Registry map[string]bool // workflow paths permitted under the default policy
}

// Violation is one failed cron.
type Violation struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Expr    string `json:"expr"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// exprValue strips surrounding quotes and any trailing comment from a cron RHS.
func exprValue(rhs string) string {
	s := strings.TrimSpace(rhs)
	if len(s) > 0 && (s[0] == '\'' || s[0] == '"') {
		if i := strings.IndexByte(s[1:], s[0]); i >= 0 {
			return s[1 : 1+i]
		}
	}
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// ParseCrons returns every cron entry in a workflow file, in document order.
func ParseCrons(content string) []CronRef {
	var out []CronRef
	for i, line := range strings.Split(content, "\n") {
		m := cronLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if expr := exprValue(m[1]); expr != "" {
			out = append(out, CronRef{Line: i + 1, Expr: expr})
		}
	}
	return out
}

// globToRegexp compiles a path glob to an anchored regexp. "*" matches within a
// path segment, "**" matches across segments, "?" matches one non-slash char.
func globToRegexp(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// allowed reports whether path matches any allow-list glob. A pattern with no
// slash is matched against the basename; otherwise against the full path.
func allowed(path string, patterns []string) bool {
	base := filepath.Base(path)
	for _, p := range patterns {
		target := path
		if !strings.Contains(p, "/") {
			target = base
		}
		if globToRegexp(p).MatchString(target) {
			return true
		}
	}
	return false
}

// Lint returns one violation per offending cron, in file then line order.
func Lint(files []WorkflowFile, cfg Config) []Violation {
	var out []Violation
	for _, f := range files {
		crons := ParseCrons(f.Content)
		if len(crons) == 0 || allowed(f.Path, cfg.Allow) {
			continue
		}
		for _, c := range crons {
			switch {
			case cfg.BanAll:
				out = append(out, Violation{
					Path: f.Path, Line: c.Line, Expr: c.Expr,
					Rule:    "no-new-crons",
					Message: "scheduled workflows are not permitted; remove the cron or add the file to the allow-list",
				})
			case !cfg.Registry[f.Path]:
				out = append(out, Violation{
					Path: f.Path, Line: c.Line, Expr: c.Expr,
					Rule:    "unregistered-cron",
					Message: "cron is not in the registry; register an owner and cadence, or add the file to the allow-list",
				})
			}
		}
	}
	return out
}

// Emit prints a human summary and returns the count.
func Emit(violations []Violation, w io.Writer) int {
	if len(violations) == 0 {
		fmt.Fprintln(w, "No cron-policy violations. \u2705")
		return 0
	}
	fmt.Fprintf(w, "%d cron-policy violation(s):\n\n", len(violations))
	for _, v := range violations {
		fmt.Fprintf(w, "%s:%d  [%s]  cron: '%s'\n    %s\n", v.Path, v.Line, v.Rule, v.Expr, v.Message)
	}
	return len(violations)
}
