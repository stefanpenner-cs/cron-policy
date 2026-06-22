package intake

import "testing"

const sampleBody = `### Target repository

linkedin-actions/foo

### Workflow path

.github/workflows/nightly.yml

### Cron expression

0 9 * * *

### Justification

nightly backup job
`

func TestParse(t *testing.T) {
	got := Parse(sampleBody)
	want := CronRequest{
		Repo:          "linkedin-actions/foo",
		Path:          ".github/workflows/nightly.yml",
		Expr:          "0 9 * * *",
		Justification: "nightly backup job",
	}
	if got != want {
		t.Fatalf("Parse:\n got %#v\nwant %#v", got, want)
	}
}

func TestParseNoResponseIsEmpty(t *testing.T) {
	body := "### Justification\n\n_No response_\n"
	if got := Parse(body); got.Justification != "" {
		t.Fatalf("_No response_ should be empty, got %q", got.Justification)
	}
}

func TestValidateGood(t *testing.T) {
	if errs := Parse(sampleBody).Validate(); len(errs) != 0 {
		t.Fatalf("valid request should pass, got %v", errs)
	}
}

func TestValidateBad(t *testing.T) {
	req := CronRequest{
		Repo: "not-a-repo",              // no slash
		Path: ".github/workflows/x.txt", // wrong ext
		Expr: "0 9 * *",                 // 4 fields
	}
	if errs := req.Validate(); len(errs) != 3 {
		t.Fatalf("want 3 errors, got %d: %v", len(errs), errs)
	}
}
