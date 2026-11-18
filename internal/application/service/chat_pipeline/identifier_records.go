package chatpipeline

import (
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
)

// IsQualifiedRecordLookup enables the same-row identifier and fault lookup
// for knowledge-scoped text turns. Open-ended requests retain normal search.
func IsQualifiedRecordLookup(chat *types.ChatManage) bool {
	if chat == nil || len(chat.Images) != 0 || len(chat.QueryImage) != 0 ||
		chat.DisableKeywordsMatch || chat.WebSearchEnabled || len(chat.SearchTargets) == 0 {
		return false
	}
	_, qualifier := searchutil.QualifiedRecordLookup(chat.Query)
	return qualifier != ""
}

func usesRecordLookup(chat *types.ChatManage) bool {
	return chat != nil && len(chat.Images) == 0 && len(chat.QueryImage) == 0 &&
		(searchutil.IsIdentifierLookup(strings.TrimSpace(chat.Query)) || IsQualifiedRecordLookup(chat))
}

func identifierRecords(chat *types.ChatManage) []*types.SearchResult {
	if !usesRecordLookup(chat) {
		return nil
	}
	identifier, qualifier := recordLookupTerms(chat)
	var out []*types.SearchResult
	seen := map[string]bool{}
	for _, hit := range chat.SearchResult {
		if hit != nil && hit.MatchType == types.MatchTypeLiteral && !seen[hit.ID] &&
			searchutil.ContainsRecordMatch(hit.Content, identifier, qualifier) {
			out = append(out, hit)
			seen[hit.ID] = true
		}
	}
	return out
}

func recordLookupTerms(chat *types.ChatManage) (string, string) {
	if IsQualifiedRecordLookup(chat) {
		return searchutil.QualifiedRecordLookup(chat.Query)
	}
	return strings.TrimSpace(chat.Query), ""
}

func hasIdentifierRecords(chat *types.ChatManage) bool { return len(identifierRecords(chat)) != 0 }

type identifierRecord struct {
	number    int
	document  string
	title     string
	reference string
	labels    []string
	cells     []string
	line      string
}

const recordTableThreshold = 10

// Enumeration is rendered from verified source rows. Parent context supplies
// labels when an older index separates the table header from the row.
func renderIdentifierRecords(chat *types.ChatManage, registry *modelcontext.Registry) string {
	if !hasIdentifierRecords(chat) {
		return ""
	}
	identifier, qualifier := recordLookupTerms(chat)
	var records []identifierRecord
	seen := map[string]bool{}
	showTable := false
	for _, hit := range chat.MergeResult {
		if hit == nil || hit.MatchType != types.MatchTypeLiteral {
			continue
		}
		handle := registry.ChunkHandle(hit.ID)
		if handle == "" {
			continue
		}
		body, origin := recordSource(hit)
		labels := recordHeader(hit.ContextHeader)
		if labels == nil && hit.ContentRewritten {
			labels = recordHeader(hit.Content)
		}
		lines := strings.Split(body, "\n")
		for i, line := range lines {
			cells := recordCells(line)
			if i+1 < len(lines) && len(cells) > 0 && recordSeparator(lines[i+1]) {
				labels = cells
				continue
			}
			if !searchutil.ContainsRecordMatch(line, identifier, qualifier) {
				continue
			}
			// Retain identical rows at distinct source positions.
			document := hit.KnowledgeBaseID + "/" + hit.KnowledgeID
			key := fmt.Sprintf("%s/%s/%d/%s", document, origin, i, line)
			if seen[key] {
				continue
			}
			seen[key] = true
			row := identifierRecord{
				number: len(records) + 1, document: document, title: firstPipelineTitle(hit),
				reference: registry.CitationTemplate(handle), line: strings.TrimSpace(line),
			}
			if len(labels) == len(cells) && len(cells) > 0 {
				row.labels, row.cells = labels, cells
			}
			showTable = showTable || len(cells) > recordTableThreshold
			records = append(records, row)
		}
	}
	if len(records) == 0 {
		return ""
	}

	var body string
	if showTable || len(records) > recordTableThreshold {
		body = renderIdentifierTables(records)
	} else {
		entries := make([]string, 0, len(records))
		for _, row := range records {
			fields := make([]string, 0, len(row.cells))
			for i, value := range row.cells {
				if value != "" {
					fields = append(fields, "- **"+escapeEvidenceQuote(recordLabel(row.labels[i], i))+"**："+escapeEvidenceQuote(value))
				}
			}
			if len(fields) == 0 {
				fields = append(fields, escapeEvidenceQuote(row.line))
			}
			entries = append(entries, fmt.Sprintf("### 记录 %d\n\n来源：%s %s\n\n%s", row.number,
				escapeEvidenceQuote(row.title), row.reference, strings.Join(fields, "\n")))
		}
		body = strings.Join(entries, "\n\n")
	}
	return fmt.Sprintf("在所选知识库已解析的内容中，找到与 **%s** 精确匹配的 **%d 条记录**。\n\n%s",
		escapeEvidenceQuote(strings.TrimSpace(chat.Query)), len(records), body)
}

func recordSource(hit *types.SearchResult) (body, origin string) {
	if hit.ContentRewritten {
		for _, source := range hit.CitationSources {
			if source != nil && source.ID == hit.ID {
				return source.Content, hit.ID
			}
		}
		// Never present an expanded neighbor under the matched chunk's ID.
		return "", hit.ID
	}
	if hit.ParentChunkID != "" {
		return hit.Content, hit.ParentChunkID
	}
	return hit.Content, hit.ID
}

func recordHeader(text string) []string {
	lines := strings.Split(text, "\n")
	for i := 1; i < len(lines); i++ {
		if recordSeparator(lines[i]) {
			if labels := recordCells(lines[i-1]); len(labels) > 0 {
				return labels
			}
		}
	}
	return nil
}

func recordLabel(label string, index int) string {
	if label != "" {
		return label
	}
	return fmt.Sprintf("第%d列", index+1)
}

// Group by source and actual headers, since workbook versions can have different
// layouts. The preview keeps one row per match and links to the original fields.
func renderIdentifierTables(records []identifierRecord) string {
	type group struct {
		title string
		rows  []identifierRecord
	}
	var groups []group
	indices := map[string]int{}
	for _, row := range records {
		key := row.document + "\x00" + strings.Join(row.labels, "\x00")
		index, ok := indices[key]
		if !ok {
			index = len(groups)
			indices[key] = index
			groups = append(groups, group{title: row.title})
		}
		groups[index].rows = append(groups[index].rows, row)
	}
	var out strings.Builder
	for _, group := range groups {
		labels := group.rows[0].labels
		columns := recordPreviewColumns(group.rows)
		headers := []string{"序号"}
		if len(columns) == 0 {
			headers = append(headers, "记录")
		}
		for _, column := range columns {
			headers = append(headers, recordTableCell(recordLabel(labels[column], column)))
		}
		headers = append(headers, "来源")
		fmt.Fprintf(&out, "#### %s\n\n", escapeEvidenceQuote(group.title))
		writeRecordTableRow(&out, headers)
		out.WriteString("|" + strings.Repeat(" --- |", len(headers)) + "\n")
		for _, row := range group.rows {
			cells := []string{fmt.Sprint(row.number)}
			if len(columns) == 0 {
				cells = append(cells, recordTableCell(row.line))
			}
			for _, column := range columns {
				cells = append(cells, recordTableCell(row.cells[column]))
			}
			writeRecordTableRow(&out, append(cells, row.reference))
		}
		out.WriteString("\n")
	}
	return "表格展示主要字段，完整字段可通过每行来源查看。\n\n" + strings.TrimSpace(out.String())
}

func writeRecordTableRow(out *strings.Builder, cells []string) {
	out.WriteString("| " + strings.Join(cells, " | ") + " |\n")
}

// The platform's table renderer splits on pipes before rendering inline
// Markdown. Encode literal pipes as entities to keep cells and citations aligned.
func recordTableCell(value string) string {
	return strings.ReplaceAll(escapeEvidenceQuote(value), `\|`, "&#124;")
}

func recordPreviewColumns(rows []identifierRecord) []int {
	const maxFields = 8 // Up to 10 displayed columns including index and source.
	labels := rows[0].labels
	var populated []int
	for i := range labels {
		for _, row := range rows {
			if row.cells[i] != "" {
				populated = append(populated, i)
				break
			}
		}
	}
	if len(populated) <= maxFields {
		return populated
	}
	// These aliases prioritize familiar display fields only. Match conditions
	// are still applied to the entire original row, regardless of column names.
	priorities := [][]string{
		{"报修时间", "创建时间", "日期", "维修日期", "维修时间", "故障日期", "date"},
		{"产品型号", "型号", "model"},
		{"产品条码", "条码", "机器编码", "序列号", "serial number"},
		{"工单号", "工单", "work order"},
		{"不良描述", "故障描述", "故障现象", "NG项目", "缺陷名称"},
		{"不良原因", "原因", "故障原因"},
		{"问题处理措施", "处理措施", "维修措施"},
		{"维修结果", "维修确认", "结果确认"},
	}
	selected := make([]int, 0, maxFields)
	for _, aliases := range priorities {
		if column := firstRecordColumn(labels, populated, aliases); column >= 0 {
			selected = append(selected, column)
		}
	}
	if len(selected) > 0 {
		return selected
	}
	return populated[:maxFields]
}

func firstRecordColumn(labels []string, populated []int, aliases []string) int {
	for _, alias := range aliases {
		for _, column := range populated {
			if strings.EqualFold(labels[column], alias) {
				return column
			}
		}
	}
	return -1
}

// Markdown's escaped pipe belongs to a cell, not a new column.
func recordCells(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil
	}
	line = line[1 : len(line)-1]
	var cells []string
	var cell strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' && i+1 < len(line) && (line[i+1] == '|' || line[i+1] == '\\') {
			i++
			cell.WriteByte(line[i])
			continue
		}
		if line[i] == '|' {
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
		} else {
			cell.WriteByte(line[i])
		}
	}
	return append(cells, strings.TrimSpace(cell.String()))
}

func recordSeparator(line string) bool {
	cells := recordCells(line)
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		c = strings.Trim(c, ":")
		if len(c) < 3 || strings.Trim(c, "-") != "" {
			return false
		}
	}
	return true
}
