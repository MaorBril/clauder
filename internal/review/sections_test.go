package review

import (
	"strings"
	"testing"

	"github.com/maorbril/clauder/internal/store"
)

func TestParseSections_basic(t *testing.T) {
	plan := strings.Join([]string{
		"# Overview",
		"intro paragraph",
		"",
		"## Goals",
		"goal one",
		"",
		"## Approach",
		"step one",
		"",
		"### Subsection",
		"detail",
		"",
		"## Risks",
		"risk one",
	}, "\n")

	sections := ParseSections(plan)
	if len(sections) != 5 {
		t.Fatalf("expected 5 sections, got %d", len(sections))
	}
	got := make([]string, 0, len(sections))
	for _, s := range sections {
		got = append(got, s.ID)
	}
	wantSlugs := []string{"overview", "goals", "approach", "subsection", "risks"}
	for i, w := range wantSlugs {
		if got[i] != w {
			t.Errorf("section[%d]: got %q, want %q", i, got[i], w)
		}
	}

	// Approach must span Subsection (since subsection is deeper)
	approach := sections[2]
	if approach.Title != "Approach" || approach.Level != 2 {
		t.Errorf("approach: got %+v", approach)
	}
	if approach.EndOffset <= approach.StartOffset {
		t.Fatalf("approach has empty span")
	}
	// Risks (## level) should end the Approach span
	risks := sections[4]
	if approach.EndOffset != risks.StartOffset {
		t.Errorf("approach.End=%d should equal risks.Start=%d", approach.EndOffset, risks.StartOffset)
	}
}

func TestParseSections_ignoresFencedCode(t *testing.T) {
	plan := "## Real One\n\nintro\n\n```go\n// phantom\n# not a heading\n## also not\n```\n\n## Real Two\nafter\n"
	sections := ParseSections(plan)
	if len(sections) != 2 {
		t.Fatalf("expected 2 real sections, got %d", len(sections))
	}
	if sections[0].ID != "real-one" || sections[1].ID != "real-two" {
		t.Errorf("unexpected slugs: %v", []string{sections[0].ID, sections[1].ID})
	}
}

func TestParseSections_duplicateSlugs(t *testing.T) {
	plan := "## Setup\n\n## Setup\n\n## Setup\n"
	sections := ParseSections(plan)
	if len(sections) != 3 {
		t.Fatalf("want 3 sections, got %d", len(sections))
	}
	if sections[0].ID != "setup" || sections[1].ID != "setup-2" || sections[2].ID != "setup-3" {
		t.Errorf("unexpected slugs: %s %s %s", sections[0].ID, sections[1].ID, sections[2].ID)
	}
}

func TestFindSectionByOffset(t *testing.T) {
	plan := strings.Join([]string{
		"# A",
		"aaa",
		"## B",
		"bbb",
		"### C",
		"ccc",
	}, "\n")
	sections := ParseSections(plan)
	cIdx := strings.Index(plan, "ccc")
	got := FindSectionByOffset(sections, cIdx)
	if got == nil || got.ID != "c" {
		t.Fatalf("expected innermost section 'c', got %+v", got)
	}
}

func TestReanchor_structural(t *testing.T) {
	oldPlan := "## Goals\nold goals\n\n## Approach\nold approach\n"
	newPlan := "## Goals\nrewritten goals\n\n## Approach\ndifferent approach\n"

	oldSections := ParseSections(oldPlan)
	newSections := ParseSections(newPlan)

	// Comment anchored on "Approach" should re-anchor structurally even though
	// the body text changed.
	approach := oldSections[1]
	c := store.ReviewComment{
		ID:                "c1",
		AnchorSectionID:   approach.ID,
		AnchorStartOffset: approach.StartOffset + 4, // somewhere inside the heading line
		AnchorEndOffset:   approach.EndOffset,
		Status:            store.CommentStatusOpen,
	}
	res := Reanchor(oldPlan, []store.ReviewComment{c}, newPlan, newSections)
	if len(res) != 1 {
		t.Fatalf("want 1 result, got %d", len(res))
	}
	if res[0].MigrationStrategy != "structural" {
		t.Errorf("expected structural, got %q", res[0].MigrationStrategy)
	}
	if res[0].NewStatus != store.CommentStatusOpen {
		t.Errorf("expected open, got %q", res[0].NewStatus)
	}
	// The new offsets should point inside the new Approach section.
	if res[0].NewStartOffset < newSections[1].StartOffset || res[0].NewEndOffset > newSections[1].EndOffset {
		t.Errorf("new anchor outside new section: got [%d,%d) section [%d,%d)",
			res[0].NewStartOffset, res[0].NewEndOffset,
			newSections[1].StartOffset, newSections[1].EndOffset)
	}
}

func TestReanchor_fuzzy(t *testing.T) {
	oldPlan := "## Setup\nrun the migrations before launch.\n"
	// Section renamed (slug changes) but the highlighted snippet survives.
	newPlan := "## Setup procedure\nyou should run the migrations before launch carefully.\n"

	oldSections := ParseSections(oldPlan)
	newSections := ParseSections(newPlan)

	snippet := "run the migrations"
	start := strings.Index(oldPlan, snippet)
	if start < 0 {
		t.Fatalf("snippet not found in oldPlan")
	}
	c := store.ReviewComment{
		ID:                "c1",
		AnchorSectionID:   oldSections[0].ID,
		AnchorStartOffset: start,
		AnchorEndOffset:   start + len(snippet),
		Status:            store.CommentStatusOpen,
	}

	res := Reanchor(oldPlan, []store.ReviewComment{c}, newPlan, newSections)
	if res[0].MigrationStrategy != "fuzzy" {
		t.Errorf("expected fuzzy, got %q", res[0].MigrationStrategy)
	}
	if res[0].NewStatus != store.CommentStatusOpen {
		t.Errorf("expected open, got %q", res[0].NewStatus)
	}
}

func TestReanchor_fuzzy_utf8(t *testing.T) {
	// Plan contains multi-byte runes (em-dash and emoji). Whitespace normalization
	// must not split runes, and the returned offsets must land on byte boundaries
	// inside newPlan that decode back to a valid UTF-8 substring.
	oldPlan := "## Setup\nrun migrations — ✅ before launch.\n"
	newPlan := "## Setup procedure\nyou should run migrations — ✅ before launch carefully.\n"

	oldSections := ParseSections(oldPlan)
	newSections := ParseSections(newPlan)

	snippet := "run migrations — ✅"
	start := strings.Index(oldPlan, snippet)
	if start < 0 {
		t.Fatalf("snippet not found in oldPlan")
	}
	c := store.ReviewComment{
		ID:                "c1",
		AnchorSectionID:   oldSections[0].ID,
		AnchorStartOffset: start,
		AnchorEndOffset:   start + len(snippet),
		Status:            store.CommentStatusOpen,
	}
	res := Reanchor(oldPlan, []store.ReviewComment{c}, newPlan, newSections)
	if res[0].MigrationStrategy != "fuzzy" {
		t.Fatalf("expected fuzzy, got %q", res[0].MigrationStrategy)
	}
	// The mapped substring should decode back to the original snippet.
	got := newPlan[res[0].NewStartOffset:res[0].NewEndOffset]
	if got != snippet {
		t.Errorf("expected newPlan[%d:%d] == %q, got %q",
			res[0].NewStartOffset, res[0].NewEndOffset, snippet, got)
	}
}

func TestReanchor_orphan(t *testing.T) {
	oldPlan := "## Setup\noriginal text here.\n"
	newPlan := "## Goals\ncompletely different content.\n"

	oldSections := ParseSections(oldPlan)
	newSections := ParseSections(newPlan)

	start := strings.Index(oldPlan, "original")
	c := store.ReviewComment{
		ID:                "c1",
		AnchorSectionID:   oldSections[0].ID,
		AnchorStartOffset: start,
		AnchorEndOffset:   start + len("original"),
		Status:            store.CommentStatusOpen,
	}
	res := Reanchor(oldPlan, []store.ReviewComment{c}, newPlan, newSections)
	if res[0].MigrationStrategy != "orphaned" {
		t.Errorf("expected orphaned, got %q", res[0].MigrationStrategy)
	}
	if res[0].NewStatus != store.CommentStatusOrphan {
		t.Errorf("expected orphan status, got %q", res[0].NewStatus)
	}
}
