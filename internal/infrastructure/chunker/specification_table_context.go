package chunker

import (
	"regexp"
	"strings"
)

var specificationTableBoundary = regexp.MustCompile(`(?i)</?table\b[^>]*>`)

// SpecificationTableContext recovers the table title and column headings for
// a legacy body-only fragment that occurs exactly once in its expanded text.
// Only an explicit specification header is used. The fragment stays citable
// under its own identity, and its stored content and offsets are unchanged.
func SpecificationTableContext(expanded, fragment string) string {
	fragment = strings.TrimSpace(fragment)
	row := htmlTableRow.FindStringIndex(fragment)
	if len(expanded) > 32<<10 || row == nil || row[0] != 0 ||
		htmlTableOpen.MatchString(fragment) || strings.Count(expanded, fragment) != 1 {
		return ""
	}
	prefix, _, _ := strings.Cut(expanded, fragment)
	if strings.Contains(prefix, "```") || strings.Contains(prefix, "~~~") {
		return ""
	}
	tags := specificationTableBoundary.FindAllStringIndex(prefix, -1)
	if len(tags) == 0 {
		return ""
	}
	last := tags[len(tags)-1]
	if strings.HasPrefix(prefix[last[0]:last[1]], "</") {
		return ""
	}
	header := readHTMLHeaderRow(htmlTableRow.FindString(prefix[last[1]:]))
	if !hasSpecificationBounds(header) {
		return ""
	}

	columns := "规格表列（从左到右）：" + strings.Join(header.labels, " | ")
	if runeLen(columns) > 240 {
		return ""
	}
	before := strings.TrimSpace(prefix[:last[0]])
	title := strings.TrimSpace(before[strings.LastIndex(before, "\n")+1:])
	if title == "" || strings.HasPrefix(title, "<") || runeLen(title) > 240 {
		return columns
	}
	return "原表章节：" + title + "\n" + columns
}

func hasSpecificationBounds(header htmlHeaderRow) bool {
	// Recognize MIN/TYP/MAX, including abbreviated and localized labels.
	// They must be separate, unmerged columns in that order.
	next := 0
	for _, cell := range header.cells {
		if cell.colspan != 1 || cell.rowspan != 1 {
			continue
		}
		label := strings.Trim(strings.ToLower(cell.text), ".． ")
		switch label {
		case "min", "minimum", "最小", "最小值":
			if next != 0 {
				return false
			}
			next = 1
		case "typ", "typical", "典型", "典型值":
			if next != 1 {
				return false
			}
			next = 2
		case "max", "maximum", "最大", "最大值":
			if next != 2 {
				return false
			}
			next = 3
		}
	}
	return next == 3
}
