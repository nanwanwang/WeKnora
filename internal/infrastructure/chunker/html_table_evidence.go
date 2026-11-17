package chunker

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const tableContextPrefix = "原表列标题（从左到右）：\n"

var (
	richTableMarkup = regexp.MustCompile(`(?i)<(?:a|audio|code|figure|iframe|img|object|pre|script|style|svg|table|video)\b`)
	columnGroupSpan = regexp.MustCompile(`^(.+)（跨([0-9]+)列）$`)
)

type tableColumnGroup struct {
	label string
	span  int
}

// PresentHTMLTableEvidence gives the model the relationships declared by the
// table's spans. It leaves the stored text and citation identity intact.
func PresentHTMLTableEvidence(passage, contextHeader string) string {
	if strings.Contains(passage, "```") || strings.Contains(passage, "~~~") || len(passage) > 32<<10 {
		return passage
	}
	indices := htmlTableRow.FindAllStringIndex(passage, -1)
	var output strings.Builder
	cursor := 0
	for start := 0; start < len(indices); {
		end := start + 1
		for end < len(indices) && strings.TrimSpace(passage[indices[end-1][1]:indices[end][0]]) == "" {
			end++
		}
		first, last := indices[start][0], indices[end-1][1]
		rows := passage[first:last]
		header := ""
		if start == 0 && !htmlTableOpen.MatchString(passage[:first]) {
			header = contextHeader
		}
		if view := presentHTMLRows(rows, header); view != "" && len(view) <= 2*len(rows)+256 {
			output.WriteString(passage[cursor:first])
			output.WriteString("\n" + view + "\n")
			cursor = last
		}
		start = end
	}
	if cursor == 0 {
		return passage
	}
	output.WriteString(passage[cursor:])
	return output.String()
}

func presentHTMLRows(body, contextHeader string) string {
	if richTableMarkup.MatchString(body) {
		return ""
	}
	var rows []htmlHeaderRow
	for _, row := range htmlTableRow.FindAllString(body, -1) {
		parsed := readHTMLHeaderRow(row)
		if len(parsed.cells) == 0 || parsed.columns > 64 {
			return ""
		}
		rows = append(rows, parsed)
	}
	var groups []tableColumnGroup
	var labels []string
	if len(rows) > 2 && rows[0].groups && rows[1].flat && rows[0].columns == rows[1].columns &&
		(rows[0].columnHeaders || (rows[0].looksLikeLabels && rows[1].looksLikeLabels)) {
		for _, cell := range rows[0].cells {
			groups = append(groups, tableColumnGroup{label: cell.text, span: cell.colspan})
		}
		labels, rows = rows[1].labels, rows[2:]
	} else {
		groups, labels = inheritedTableGroups(contextHeader)
	}
	if len(groups) > 1 {
		return renderColumnGroups(groups, labels, rows)
	}
	return renderMergedRows(rows)
}

func inheritedTableGroups(contextHeader string) ([]tableColumnGroup, []string) {
	_, header, ok := strings.Cut(contextHeader, tableContextPrefix)
	if !ok {
		return nil, nil
	}
	lines := strings.Split(strings.TrimSpace(header), "\n")
	if len(lines) != 2 {
		return nil, nil
	}
	var groups []tableColumnGroup
	var labels []string
	width := 0
	for _, raw := range strings.Split(lines[0], "|") {
		group := columnGroupSpan.FindStringSubmatch(strings.TrimSpace(raw))
		if group == nil {
			return nil, nil
		}
		span, err := strconv.Atoi(group[2])
		if err != nil || span < 2 || span > 64 || width+span > 64 {
			return nil, nil
		}
		groups = append(groups, tableColumnGroup{label: group[1], span: span})
		width += span
	}
	for _, raw := range strings.Split(lines[1], "|") {
		if label := strings.TrimSpace(raw); label != "" {
			labels = append(labels, label)
		}
	}
	if len(groups) < 2 || len(labels) != width {
		return nil, nil
	}
	return groups, labels
}

func renderColumnGroups(groups []tableColumnGroup, labels []string, rows []htmlHeaderRow) string {
	for _, row := range rows {
		if len(row.cells) != len(labels) {
			return ""
		}
		for _, cell := range row.cells {
			if cell.colspan != 1 || cell.rowspan != 1 {
				return ""
			}
		}
	}
	var output strings.Builder
	offset := 0
	for _, group := range groups {
		fmt.Fprintf(&output, "### 原表列组 %s\n", group.label)
		for _, row := range rows {
			var values []string
			for c := offset; c < offset+group.span; c++ {
				values = append(values, labels[c]+" = "+row.cells[c].text)
			}
			fmt.Fprintf(&output, "- %s\n", strings.Join(values, " | "))
		}
		output.WriteByte('\n')
		offset += group.span
	}
	return strings.TrimSpace(output.String())
}

type carriedTableCell struct {
	text   string
	endRow int
}

func renderMergedRows(rows []htmlHeaderRow) string {
	var output strings.Builder
	var pending [64]carriedTableCell
	hasRowspan := false
	width := 0
	for rowIndex, row := range rows {
		var values []string
		pos := 0
		for _, cell := range row.cells {
			for pos < width && pending[pos].endRow > rowIndex {
				values = append(values, pending[pos].text)
				pos++
			}
			if pos+cell.colspan > len(pending) {
				return ""
			}
			for col := pos; col < pos+cell.colspan; col++ {
				if pending[col].endRow > rowIndex {
					return ""
				}
				pending[col] = carriedTableCell{text: cell.text, endRow: rowIndex + cell.rowspan}
				values = append(values, cell.text)
			}
			hasRowspan = hasRowspan || cell.rowspan > 1
			pos += cell.colspan
		}
		for col := pos; col < width; col++ {
			if pending[col].endRow > rowIndex {
				values = append(values, pending[col].text)
			}
		}
		width = max(width, pos)
		fmt.Fprintf(&output, "- %s\n", strings.Join(values, " | "))
	}
	if !hasRowspan {
		return ""
	}
	return strings.TrimSpace(output.String())
}
