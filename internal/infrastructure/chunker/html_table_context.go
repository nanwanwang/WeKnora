package chunker

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// The OCR normalizer retains merged cells as HTML. Keep their column labels
// in ContextHeader, where they can guide search and generation without
// changing the cited text, its offsets, or the original table markup.
type htmlTableContext struct {
	headerStart int
	bodyStart   int
	bodyEnd     int
	header      string
}

var htmlTableBlock = regexp.MustCompile("(?ims)^[ \t]*<table\\b[^>]*>.*?</table\\s*>")
var htmlTableOpen = regexp.MustCompile("(?i)<table\\b")
var htmlTableRow = regexp.MustCompile("(?is)<tr\\b[^>]*>.*?</tr\\s*>")
var htmlTableHead = regexp.MustCompile("(?is)<thead\\b[^>]*>.*?</thead\\s*>")

func findHTMLTableContexts(text string) []htmlTableContext {
	tables := htmlTableBlock.FindAllStringIndex(text, -1)
	if len(tables) == 0 {
		return nil
	}
	var contexts []htmlTableContext
	var fence markdownFence
	lineStart, scanned, runeOffset := 0, 0, 0
	for line := range strings.SplitAfterSeq(text, "\n") {
		code, _ := fence.consume(strings.TrimSuffix(line, "\n"))
		lineEnd := lineStart + len(line)
		for len(tables) > 0 && tables[0][0] < lineEnd {
			start, end := tables[0][0], tables[0][1]
			tables = tables[1:]
			if code {
				continue
			}
			runeOffset += runeLen(text[scanned:start])
			scanned = start
			if table, ok := readHTMLTableContext(text[start:end], runeOffset); ok {
				contexts = append(contexts, table)
			}
		}
		lineStart = lineEnd
		if len(tables) == 0 {
			break
		}
	}
	return contexts
}

func readHTMLTableContext(block string, offset int) (htmlTableContext, bool) {
	// A nested table has its own column layout. Do not borrow header rows
	// from it or from a subsequent table to repair an incomplete outer one.
	if len(htmlTableOpen.FindAllStringIndex(block, 2)) != 1 {
		return htmlTableContext{}, false
	}
	rows := htmlTableRow.FindAllStringIndex(block, -1)
	if len(rows) < 2 {
		return htmlTableContext{}, false
	}
	first := readHTMLHeaderRow(block[rows[0][0]:rows[0][1]])
	count := 0
	switch head := htmlTableHead.FindStringIndex(block); {
	case head != nil && rows[0][0] >= head[0] && rows[0][1] <= head[1]:
		for count < len(rows) && rows[count][1] <= head[1] {
			count++
		}
	case first.columnHeaders:
		count = 1
		for count < len(rows) && readHTMLHeaderRow(block[rows[count][0]:rows[count][1]]).columnHeaders {
			count++
		}
	case first.groups && len(rows) >= 3:
		// OCR uses <td> even for headers. Accept the common two-level
		// layout only when distinct grouped labels cover exactly the
		// unmerged label columns in the following row.
		second := readHTMLHeaderRow(block[rows[1][0]:rows[1][1]])
		if second.flat && first.columns == second.columns && first.looksLikeLabels && second.looksLikeLabels {
			count = 2
		}
	}
	qualifiedHeader := ""
	if count == 0 || first.columnHeaders {
		parsed := make([]htmlHeaderRow, 0, min(len(rows), 9))
		for _, row := range rows[:min(len(rows), 9)] {
			parsed = append(parsed, readHTMLHeaderRow(block[row[0]:row[1]]))
		}
		if labels, body := readHTMLColumnPaths(parsed); body > 0 && (count == 0 || count == body) {
			qualifiedHeader = tableColumnPathsPrefix + strings.Join(labels, " | ")
			if count == 0 && runeLen(qualifiedHeader) <= 384 {
				return htmlTableContext{
					headerStart: offset + runeLen(block[:rows[0][0]]),
					bodyStart:   offset + runeLen(block[:rows[body][0]]),
					bodyEnd:     offset + runeLen(block[:rows[len(rows)-1][1]]),
					header:      qualifiedHeader,
				}, true
			}
		}
	}
	if count == 0 || count == len(rows) || count > 8 {
		return htmlTableContext{}, false
	}
	var lines []string
	for _, row := range rows[:count] {
		labels := readHTMLHeaderRow(block[row[0]:row[1]]).labels
		if len(labels) == 0 {
			return htmlTableContext{}, false
		}
		lines = append(lines, strings.Join(labels, " | "))
	}
	header := tableContextPrefix + strings.Join(lines, "\n")
	if qualifiedHeader != "" && runeLen(header+"\n\n"+qualifiedHeader) <= 384 {
		header += "\n\n" + qualifiedHeader
	}
	if runeLen(header) > 384 {
		return htmlTableContext{}, false
	}
	return htmlTableContext{
		headerStart: offset + runeLen(block[:rows[0][0]]),
		bodyStart:   offset + runeLen(block[:rows[count][0]]),
		bodyEnd:     offset + runeLen(block[:rows[len(rows)-1][1]]),
		header:      header,
	}, true
}

type htmlHeaderRow struct {
	labels          []string
	cells           []htmlTableCell
	columns         int
	flat            bool
	groups          bool
	columnHeaders   bool
	looksLikeLabels bool
}

type htmlTableCell struct {
	text    string
	colspan int
	rowspan int
}

func readHTMLHeaderRow(row string) htmlHeaderRow {
	result := htmlHeaderRow{flat: true, groups: true, columnHeaders: true, looksLikeLabels: true}
	tokenizer := html.NewTokenizer(strings.NewReader(row))
	var label strings.Builder
	var cell string
	var colspan, rowspan int
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			result.groups = result.groups && len(result.labels) >= 2
			result.columnHeaders = result.columnHeaders && len(result.labels) > 0
			return result
		case html.StartTagToken, html.SelfClosingTagToken:
			tag := tokenizer.Token()
			if cell != "" && tag.Data == "br" {
				label.WriteByte(' ')
			}
			if tag.Data != "th" && tag.Data != "td" {
				continue
			}
			cell = tag.Data
			label.Reset()
			colspan, rowspan = 1, 1
			result.columnHeaders = result.columnHeaders && cell == "th"
			for _, attr := range tag.Attr {
				switch attr.Key {
				case "scope":
					result.columnHeaders = result.columnHeaders && !strings.HasPrefix(strings.ToLower(attr.Val), "row")
				case "colspan", "rowspan":
					value, err := strconv.Atoi(attr.Val)
					if err != nil || value < 1 || value > 64 {
						return htmlHeaderRow{}
					}
					if attr.Key == "colspan" {
						colspan = value
					} else {
						rowspan = value
					}
				}
			}
		case html.TextToken:
			if cell != "" {
				label.Write(tokenizer.Text())
			}
		case html.EndTagToken:
			if cell == "" || tokenizer.Token().Data != cell {
				continue
			}
			name := strings.Join(strings.Fields(label.String()), " ")
			result.cells = append(result.cells, htmlTableCell{text: name, colspan: colspan, rowspan: rowspan})
			result.flat = result.flat && colspan == 1 && rowspan == 1 && name != ""
			result.groups = result.groups && colspan > 1 && rowspan == 1 && name != ""
			result.looksLikeLabels = result.looksLikeLabels && strings.IndexFunc(name, unicode.IsLetter) >= 0 &&
				strings.IndexFunc(name, unicode.IsDigit) < 0
			if colspan > 1 {
				name += fmt.Sprintf("（跨%d列）", colspan)
			}
			if rowspan > 1 {
				name += fmt.Sprintf("（跨%d行）", rowspan)
			}
			result.labels = append(result.labels, name)
			result.columns += colspan
			cell = ""
		}
	}
}

func attachHTMLTableContexts(chunks []Chunk, tables []htmlTableContext, budget int) {
	if len(tables) == 0 {
		return
	}
	for i := range chunks {
		chunk := &chunks[i]
		j := sort.Search(len(tables), func(j int) bool { return tables[j].bodyEnd > chunk.Start })
		if j == len(tables) || chunk.Start <= tables[j].headerStart || chunk.End <= tables[j].bodyStart {
			continue
		}
		header := tables[j].header
		if chunk.ContextHeader != "" {
			header = chunk.ContextHeader + "\n\n" + header
		}
		// Keep explicit token limits and source offsets intact. With a
		// constrained budget, prefer the row values over duplicated labels.
		if budget > 0 && runeLen(header)+2+runeLen(chunk.Content) > budget {
			continue
		}
		chunk.ContextHeader = header
	}
}
