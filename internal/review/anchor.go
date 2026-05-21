package review

import (
	"strings"

	"github.com/maorbril/clauder/internal/store"
)

// ReanchorResult captures what happened when a comment was re-anchored against
// a new revision.
type ReanchorResult struct {
	CommentID         string
	NewSectionID      string
	NewFingerprint    string
	NewStartOffset    int
	NewEndOffset      int
	NewStatus         string // open or orphaned
	MigrationStrategy string // "structural" | "fuzzy" | "orphaned"
}

// Reanchor walks each comment and attempts to map it onto the new revision.
// Strategy:
//  1. Match by section ID (stable slug). The comment now points at the *start*
//     of the matching section in the new plan, since fine-grained offsets become
//     meaningless after substantive edits.
//  2. If the section is gone, look up the fingerprint in the new plan via fuzzy
//     text search; if found, anchor there.
//  3. Otherwise, mark orphaned. The UI surfaces these for manual re-anchoring.
func Reanchor(
	oldPlan string,
	comments []store.ReviewComment,
	newPlan string,
	newSections []store.ReviewSection,
) []ReanchorResult {
	sectionByID := make(map[string]*store.ReviewSection, len(newSections))
	for i := range newSections {
		sectionByID[newSections[i].ID] = &newSections[i]
	}

	results := make([]ReanchorResult, 0, len(comments))
	for _, c := range comments {
		if c.Status == store.CommentStatusResolved {
			// Resolved comments don't migrate — they stay attached to their
			// historical revision for the audit log.
			continue
		}

		if sec, ok := sectionByID[c.AnchorSectionID]; ok && c.AnchorSectionID != "" {
			results = append(results, ReanchorResult{
				CommentID:         c.ID,
				NewSectionID:      sec.ID,
				NewFingerprint:    c.AnchorTextFingerprint,
				NewStartOffset:    sec.StartOffset,
				NewEndOffset:      sec.EndOffset,
				NewStatus:         store.CommentStatusOpen,
				MigrationStrategy: "structural",
			})
			continue
		}

		// Fuzzy: find the highlighted snippet from the old plan inside the new plan.
		snippet := snippetForComment(oldPlan, c)
		if snippet != "" {
			if start, end, ok := findFuzzy(newPlan, snippet); ok {
				sec := FindSectionByOffset(newSections, start)
				newSectionID := ""
				if sec != nil {
					newSectionID = sec.ID
				}
				results = append(results, ReanchorResult{
					CommentID:         c.ID,
					NewSectionID:      newSectionID,
					NewFingerprint:    c.AnchorTextFingerprint,
					NewStartOffset:    start,
					NewEndOffset:      end,
					NewStatus:         store.CommentStatusOpen,
					MigrationStrategy: "fuzzy",
				})
				continue
			}
		}

		results = append(results, ReanchorResult{
			CommentID:         c.ID,
			NewSectionID:      c.AnchorSectionID,
			NewFingerprint:    c.AnchorTextFingerprint,
			NewStartOffset:    0,
			NewEndOffset:      0,
			NewStatus:         store.CommentStatusOrphan,
			MigrationStrategy: "orphaned",
		})
	}
	return results
}

func snippetForComment(plan string, c store.ReviewComment) string {
	if c.AnchorEndOffset <= c.AnchorStartOffset || c.AnchorEndOffset > len(plan) {
		return ""
	}
	return plan[c.AnchorStartOffset:c.AnchorEndOffset]
}

// findFuzzy looks for snippet inside hay. It first tries exact, then tries a
// whitespace-normalized exact match by walking forward over the hay.
// Returns the inclusive-exclusive offsets in hay.
func findFuzzy(hay, snippet string) (int, int, bool) {
	if snippet == "" {
		return 0, 0, false
	}
	if idx := strings.Index(hay, snippet); idx >= 0 {
		return idx, idx + len(snippet), true
	}
	// Normalize whitespace in snippet and scan over hay character-by-character.
	target := strings.Join(strings.Fields(snippet), " ")
	if target == "" {
		return 0, 0, false
	}
	// Build a normalized view of hay and remember original offsets.
	var norm strings.Builder
	offsets := make([]int, 0, len(hay))
	prevSpace := true
	for i := 0; i < len(hay); i++ {
		ch := hay[i]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if !prevSpace {
				norm.WriteByte(' ')
				offsets = append(offsets, i)
				prevSpace = true
			}
			continue
		}
		norm.WriteByte(ch)
		offsets = append(offsets, i)
		prevSpace = false
	}
	normStr := norm.String()
	idx := strings.Index(normStr, target)
	if idx < 0 {
		return 0, 0, false
	}
	startOriginal := offsets[idx]
	endIdx := idx + len(target) - 1
	if endIdx >= len(offsets) {
		return 0, 0, false
	}
	endOriginal := offsets[endIdx] + 1
	return startOriginal, endOriginal, true
}
