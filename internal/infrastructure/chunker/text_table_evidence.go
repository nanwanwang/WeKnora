package chunker

import (
	"strings"
	"unicode"
)

func PresentTableEvidence(passage, contextHeader string) string {
	return InspectTableEvidence(passage, contextHeader).Text
}

// InspectTableEvidence also accepts older indexes that contain a flattened
// grouped table. The text form needs two explicit header rows, aligned field
// counts, and a repeated subcolumn label to establish the group widths.
func InspectTableEvidence(passage, contextHeader string) TableEvidence {
	presented := inspectHTMLTableEvidence(passage, contextHeader)
	if presented.Text != passage || htmlTableRow.MatchString(passage) ||
		strings.Contains(passage, "```") || strings.Contains(passage, "~~~") || len(passage) > 32<<10 {
		return presented
	}
	lines := strings.Split(passage, "\n")
	for start := 0; start+3 < len(lines); start++ {
		groups := strings.Fields(lines[start])
		labels := strings.Fields(lines[start+1])
		if !repeatedTextTableLabels(groups, labels) {
			continue
		}
		end := start + 2
		var rows []htmlHeaderRow
		for end < len(lines) {
			fields := strings.Fields(lines[end])
			if len(fields) != len(labels) || !textTableData(fields) {
				break
			}
			row := htmlHeaderRow{}
			for _, field := range fields {
				row.cells = append(row.cells, htmlTableCell{text: field, colspan: 1, rowspan: 1})
			}
			rows = append(rows, row)
			end++
		}
		if len(rows) < 2 {
			continue
		}
		var layout []tableColumnGroup
		for _, group := range groups {
			layout = append(layout, tableColumnGroup{label: group, span: len(labels) / len(groups)})
		}
		view := renderColumnGroups(layout, labels, rows)
		original := strings.Join(lines[start:end], "\n")
		if view.Text != "" && len(view.Text) <= 2*len(original)+256 {
			first := strings.Join(lines[:start], "\n")
			last := strings.Join(lines[end:], "\n")
			view.Text = strings.TrimPrefix(first+"\n"+view.Text+"\n"+last, "\n")
			return view
		}
	}
	return presented
}

func repeatedTextTableLabels(groups, labels []string) bool {
	if len(groups) < 2 || len(groups) > 8 || len(labels) > 32 ||
		len(labels) < 2*len(groups) || len(labels)%len(groups) != 0 {
		return false
	}
	width := len(labels) / len(groups)
	repeat := labels[width-1]
	usedGroups, usedKeys := map[string]bool{}, map[string]bool{}
	if strings.IndexFunc(repeat, unicode.IsLetter) < 0 {
		return false
	}
	for i, group := range groups {
		key := labels[i*width]
		if usedGroups[group] || usedKeys[key] || labels[(i+1)*width-1] != repeat ||
			strings.IndexFunc(group, unicode.IsLetter) < 0 || strings.IndexFunc(key, unicode.IsLetter) < 0 {
			return false
		}
		usedGroups[group], usedKeys[key] = true, true
	}
	return true
}

func textTableData(fields []string) bool {
	for _, field := range fields {
		if strings.IndexFunc(field, unicode.IsDigit) >= 0 {
			return true
		}
	}
	return false
}
