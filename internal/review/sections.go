package review

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"

	"github.com/maorbril/clauder/internal/store"
)

var headingRe = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+?)\s*$`)

// ParseSections walks the markdown plan and returns one ReviewSection per ATX
// heading, with byte offsets covering each section's span (heading line through
// the line before the next equal-or-higher heading).
//
// IDs are stable slugs derived from heading text; ambiguous duplicate slugs
// receive a numeric suffix so anchors can address them deterministically.
func ParseSections(plan string) []store.ReviewSection {
	matches := headingRe.FindAllStringSubmatchIndex(plan, -1)
	if len(matches) == 0 {
		return nil
	}

	sections := make([]store.ReviewSection, 0, len(matches))
	slugCounts := make(map[string]int)

	for i, m := range matches {
		hashes := plan[m[2]:m[3]]
		title := strings.TrimSpace(plan[m[4]:m[5]])
		level := len(hashes)

		baseSlug := slugify(title)
		if baseSlug == "" {
			baseSlug = "section"
		}
		slug := baseSlug
		if n := slugCounts[baseSlug]; n > 0 {
			slug = baseSlug + "-" + strconv.Itoa(n+1)
		}
		slugCounts[baseSlug]++

		start := m[0]
		end := len(plan)
		// Find next heading at equal-or-higher level
		for j := i + 1; j < len(matches); j++ {
			nextHashes := plan[matches[j][2]:matches[j][3]]
			if len(nextHashes) <= level {
				end = matches[j][0]
				break
			}
		}

		sections = append(sections, store.ReviewSection{
			ID:          slug,
			Title:       title,
			Level:       level,
			StartOffset: start,
			EndOffset:   end,
		})
	}
	return sections
}

// FindSectionByOffset returns the deepest section whose span contains the offset,
// or nil if no section matches.
func FindSectionByOffset(sections []store.ReviewSection, offset int) *store.ReviewSection {
	var best *store.ReviewSection
	for i := range sections {
		s := &sections[i]
		if offset >= s.StartOffset && offset < s.EndOffset {
			if best == nil || s.Level > best.Level {
				best = s
			}
		}
	}
	return best
}

// Fingerprint returns a short hash of the normalized text for fuzzy reanchoring.
func Fingerprint(text string) string {
	norm := strings.ToLower(strings.Join(strings.Fields(text), " "))
	if norm == "" {
		return ""
	}
	sum := sha1.Sum([]byte(norm))
	return hex.EncodeToString(sum[:8])
}

var slugReplace = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(s)
	s = slugReplace.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

