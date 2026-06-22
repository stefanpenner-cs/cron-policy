// Package cronequiv rewrites a cron to a schedule-equivalent but textually
// different expression — the safe "real edit" that re-attributes a scheduled
// workflow's actor without changing when it runs.
//
// GitHub re-attributes a scheduled workflow's actor only when the cron VALUE
// changes (comment/whitespace edits do not). So to re-home a cron onto a
// durable account without changing its schedule, we make a real but
// schedule-neutral edit.
//
// Strategy (first applicable wins):
//  1. Expand a plain "*" field to its explicit full range
//     (minute->0-59, hour->0-23, month->1-12, dom->1-31, dow->0-6).
//  2. Reorder a comma list ("9,17" -> "17,9"); cron lists are unordered sets.
//  3. Turn the first single numeric field into an N-N range ("4" -> "4-4").
//
// Safety: cron treats day-of-month and day-of-week as a UNION when BOTH are
// restricted. Expanding "*" -> full-range on dom while dow is restricted (or
// vice-versa) would silently widen the schedule (e.g. "Mondays" -> "daily"),
// so those expansions are skipped unless the paired field is also "*".
package cronequiv

import "strings"

// field indexes
const (
	idxMinute = 0
	idxHour   = 1
	idxDOM    = 2
	idxMonth  = 3
	idxDOW    = 4
)

// full-range replacement for a plain "*" per field index.
var fullRange = map[int]string{
	idxMinute: "0-59",
	idxHour:   "0-23",
	idxDOM:    "1-31",
	idxMonth:  "1-12",
	idxDOW:    "0-6",
}

// Try always-safe fields first (minute, hour, month), then the dom/dow pair
// whose expansion is only conditionally safe.
var expandOrder = []int{idxMinute, idxHour, idxMonth, idxDOM, idxDOW}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func expandStar(f []string) bool {
	for _, i := range expandOrder {
		if f[i] != "*" {
			continue
		}
		if i == idxDOM && f[idxDOW] != "*" {
			continue // would OR with a restricted dow -> widens schedule
		}
		if i == idxDOW && f[idxDOM] != "*" {
			continue // would OR with a restricted dom -> widens schedule
		}
		f[i] = fullRange[i]
		return true
	}
	return false
}

func reorderComma(f []string) bool {
	for i, field := range f {
		if strings.Contains(field, ",") {
			parts := strings.Split(field, ",")
			for l, r := 0, len(parts)-1; l < r; l, r = l+1, r-1 {
				parts[l], parts[r] = parts[r], parts[l]
			}
			f[i] = strings.Join(parts, ",")
			return true
		}
	}
	return false
}

func singleValueRange(f []string) bool {
	for i, field := range f {
		if isDigits(field) {
			f[i] = field + "-" + field
			return true
		}
	}
	return false
}

// Rewrite returns a schedule-equivalent but textually different cron, and true.
// It returns ("", false) when the input is not a 5-field cron, or no safe
// neutral edit exists.
func Rewrite(expr string) (string, bool) {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return "", false
	}
	orig := strings.Join(f, " ")
	for _, transform := range []func([]string) bool{expandStar, reorderComma, singleValueRange} {
		if transform(f) {
			if out := strings.Join(f, " "); out != orig {
				return out, true
			}
		}
	}
	return "", false
}
