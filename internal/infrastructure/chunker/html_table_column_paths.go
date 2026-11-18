package chunker

import (
	"fmt"
	"strings"
	"unicode"
)

const tableColumnPathsPrefix = "原表列路径（从左到右）：\n"

// readHTMLColumnPaths recognizes grouped OCR tables with a shared row key.
// Every column must have a unique, complete path before it can label values.
func readHTMLColumnPaths(rows []htmlHeaderRow) ([]string, int) {
	if len(rows) < 3 {
		return nil, 0
	}
	width := rows[0].columns
	if width < 3 || width > 64 {
		return nil, 0
	}
	shared, grouped := false, false
	for _, cell := range rows[0].cells {
		shared = shared || cell.rowspan > 1
		grouped = grouped || cell.colspan > 1
	}
	if !shared || !grouped {
		return nil, 0
	}

	var paths [64][]string
	var pending [64]carriedTableCell
	var headerEnd int
	for i, row := range rows[:min(len(rows)-1, 8)] {
		column := 0
		leaves := 0
		for _, cell := range row.cells {
			for column < width && pending[column].endRow > i {
				column++
			}
			if cell.text == "" || strings.Contains(cell.text, "|") || column+cell.colspan > width {
				return nil, 0
			}
			if cell.colspan == 1 && cell.rowspan == 1 &&
				strings.IndexFunc(cell.text, unicode.IsLetter) < 0 && !strings.ContainsAny(cell.text, "℃℉") {
				return nil, 0
			}
			if cell.colspan == 1 && cell.rowspan == 1 {
				leaves++
			}
			for j := column; j < column+cell.colspan; j++ {
				if pending[j].endRow > i {
					return nil, 0
				}
				pending[j].endRow = i + cell.rowspan
				paths[j] = append(paths[j], cell.text)
			}
			column += cell.colspan
			headerEnd = max(headerEnd, i+cell.rowspan)
		}

		for _, cell := range pending[:width] {
			if cell.endRow <= i {
				return nil, 0
			}
		}
		if i == 0 || i+1 < headerEnd || leaves < 2 || rows[i+1].columns != width {
			continue
		}

		labels := make([]string, width)
		seen := make(map[string]bool, width)
		for j, path := range paths[:width] {
			labels[j] = strings.Join(path, " / ")
			if seen[labels[j]] {
				return nil, 0
			}
			seen[labels[j]] = true
		}
		return labels, i + 1
	}
	return nil, 0
}

func inheritedHTMLColumnPaths(contextHeader string) []string {
	_, header, ok := strings.Cut(contextHeader, tableColumnPathsPrefix)
	if !ok {
		return nil
	}
	line, _, _ := strings.Cut(header, "\n")
	return strings.Split(line, " | ")
}

// Keep empty values in their source column and repeat shared row labels only
// over their declared rowspan. A partially aligned table keeps its raw HTML.
func renderHTMLColumnPaths(labels []string, rows []htmlHeaderRow) string {
	var out strings.Builder
	var pending [64]carriedTableCell
	width := len(labels)
	if width < 3 || width > len(pending) {
		return ""
	}
	for i, row := range rows {
		column := 0
		for _, cell := range row.cells {
			for column < width && pending[column].endRow > i {
				column++
			}
			if column >= width || cell.colspan != 1 {
				return ""
			}
			pending[column] = carriedTableCell{text: cell.text, endRow: i + cell.rowspan}
			column++
		}
		out.WriteString("- ")
		separator := ""
		for j, cell := range pending[:width] {
			if cell.endRow <= i {
				return ""
			}
			if cell.text != "" {
				fmt.Fprintf(&out, "%s%s = %s", separator, labels[j], cell.text)
				separator = " | "
			}
		}
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String())
}
