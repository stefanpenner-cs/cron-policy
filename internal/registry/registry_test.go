package registry

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingIsEmpty(t *testing.T) {
	r, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(r.Entries) != 0 {
		t.Fatalf("want empty, got %#v", r.Entries)
	}
}

func sample() Entry {
	return Entry{
		Repo: "linkedin-actions/foo", Path: ".github/workflows/nightly.yml",
		Expr: "0 9 * * *", OwnerTeam: "ci-cd-platform-reviewers",
		Request: "https://github.com/o/r/issues/1",
	}
}

func TestUpsertAddThenUpdate(t *testing.T) {
	r := &Registry{}
	if added := r.Upsert(sample()); !added {
		t.Fatal("first upsert should add")
	}
	e := sample()
	e.Expr = "0 10 * * *"
	if added := r.Upsert(e); added {
		t.Fatal("second upsert of same key should update, not add")
	}
	if len(r.Entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(r.Entries))
	}
	if r.Entries[0].Expr != "0 10 * * *" {
		t.Fatalf("update did not take: %#v", r.Entries[0])
	}
}

func TestUpsertSorted(t *testing.T) {
	r := &Registry{}
	b := sample()
	b.Repo = "linkedin-actions/zzz"
	r.Upsert(b)
	r.Upsert(sample()) // linkedin-actions/foo sorts first
	if r.Entries[0].Repo != "linkedin-actions/foo" {
		t.Fatalf("entries not sorted by key: %#v", r.Entries)
	}
}

func TestValidate(t *testing.T) {
	good := &Registry{Entries: []Entry{sample()}}
	if errs := good.Validate(); len(errs) != 0 {
		t.Fatalf("valid entry should pass, got %v", errs)
	}
	bad := sample()
	bad.OwnerTeam = ""
	bad.Expr = "0 9 * *" // 4 fields
	r := &Registry{Entries: []Entry{bad}}
	if errs := r.Validate(); len(errs) != 2 {
		t.Fatalf("want 2 errors (missing field + bad expr), got %v", errs)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "registry.json")
	r := &Registry{}
	r.Upsert(sample())
	if err := r.Save(p); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Entries) != 1 || got.Entries[0] != sample() {
		t.Fatalf("round-trip mismatch: %#v", got.Entries)
	}
}
