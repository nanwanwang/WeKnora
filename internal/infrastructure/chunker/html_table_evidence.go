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
	tableSectionGap = regexp.MustCompile(`(?is)^\s*(</?(thead|tbody)\b[^>]*>\s*)*$`)
	columnGroupSpan = regexp.MustCompile(`^(.+)（跨([0-9]+)列）$`)
)

type tableColumnGroup struct {
	label string
	span  int
}

type TableEvidence struct {
	Text             string
	ParallelRows     []ParallelTableRow
	QualifiedColumns bool
}

// ParallelTableRow retains the original row and which cells belong to each group.
type ParallelTableRow struct {
	Groups []ParallelTableGroup
}

type ParallelTableGroup struct {
	Name   string
	Labels []string
	Cells  []string
}

// PresentHTMLTableEvidence gives the model the relationships declared by the
// table's spans. It leaves the stored text and citation identity intact.
func PresentHTMLTableEvidence(passage, contextHeader string) string {
	return inspectHTMLTableEvidence(passage, contextHeader).Text
}

func inspectHTMLTableEvidence(passage, contextHeader string) TableEvidence {
	if strings.Contains(passage, "```") || strings.Contains(passage, "~~~") || len(passage) > 32<<10 {
		return TableEvidence{Text: passage}
	}
	indices := htmlTableRow.FindAllStringIndex(passage, -1)
	var output strings.Builder
	var parallelRows []ParallelTableRow
	qualified := false
	cursor := 0
	for start := 0; start < len(indices); {
		end := start + 1
		for end < len(indices) && tableSectionGap.MatchString(passage[indices[end-1][1]:indices[end][0]]) {
			end++
		}
		first, last := indices[start][0], indices[end-1][1]
		rows := passage[first:last]
		header := ""
		if start == 0 && !htmlTableOpen.MatchString(passage[:first]) {
			header = contextHeader
		}
		view := presentHTMLRows(rows, header)
		limit := 2*len(rows) + 256
		if view.QualifiedColumns {
			// Each row repeats the qualified labels. Bound that expansion
			// separately from the shorter legacy table presentations.
			limit = min(5*len(rows)+256, 32<<10)
		}
		if view.Text != "" && len(view.Text) <= limit {
			output.WriteString(passage[cursor:first])
			output.WriteString("\n" + view.Text + "\n")
			parallelRows = append(parallelRows, view.ParallelRows...)
			qualified = qualified || view.QualifiedColumns
			cursor = last
		}
		start = end
	}
	if cursor == 0 {
		return TableEvidence{Text: passage}
	}
	output.WriteString(passage[cursor:])
	return TableEvidence{Text: output.String(), ParallelRows: parallelRows, QualifiedColumns: qualified}
}

func presentHTMLRows(body, contextHeader string) TableEvidence {
	if richTableMarkup.MatchString(body) {
		return TableEvidence{}
	}
	var rows []htmlHeaderRow
	for _, row := range htmlTableRow.FindAllString(body, -1) {
		parsed := readHTMLHeaderRow(row)
		if len(parsed.cells) == 0 || parsed.columns > 64 {
			return TableEvidence{}
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
	if labels, body := readHTMLColumnPaths(rows); body > 0 {
		if view := renderHTMLColumnPaths(labels, rows[body:]); view != "" {
			return TableEvidence{Text: view, QualifiedColumns: true}
		}
	} else if labels := inheritedHTMLColumnPaths(contextHeader); len(labels) > 0 {
		if view := renderHTMLColumnPaths(labels, rows); view != "" {
			return TableEvidence{Text: view, QualifiedColumns: true}
		}
	}
	return TableEvidence{Text: renderMergedRows(rows)}
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

func renderColumnGroups(groups []tableColumnGroup, labels []string, rows []htmlHeaderRow) TableEvidence {
	for _, row := range rows {
		if len(row.cells) != len(labels) {
			return TableEvidence{}
		}
		for _, cell := range row.cells {
			if cell.colspan != 1 || cell.rowspan != 1 {
				return TableEvidence{}
			}
		}
	}
	var output strings.Builder
	parallelRows := make([]ParallelTableRow, len(rows))
	offset := 0
	for _, group := range groups {
		fmt.Fprintf(&output, "### 原表列组 %s\n", group.label)
		for index, row := range rows {
			cells := make([]string, 0, group.span)
			var values []string
			for c := offset; c < offset+group.span; c++ {
				values = append(values, labels[c]+" = "+row.cells[c].text)
				cells = append(cells, row.cells[c].text)
			}
			parallelRows[index].Groups = append(parallelRows[index].Groups, ParallelTableGroup{
				Name: group.label, Labels: labels[offset : offset+group.span], Cells: cells,
			})
			fmt.Fprintf(&output, "- %s\n", strings.Join(values, " | "))
		}
		output.WriteByte('\n')
		offset += group.span
	}
	return TableEvidence{Text: strings.TrimSpace(output.String()), ParallelRows: parallelRows}
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
