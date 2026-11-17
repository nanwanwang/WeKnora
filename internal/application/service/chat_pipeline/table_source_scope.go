package chatpipeline

import (
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/types"
)

var (
	tableMentionRE  = regexp.MustCompile(`(?i)表|\btable\b`)
	tableRelationRE = regexp.MustCompile(`(?i)同一[行排]|同行|同排|并列|排在一起|等价|等效|互换|换算|替代|\b(?:same (?:table )?row|side by side|equivalen\w*|interchang\w*)\b`)
	otherSourceRE   = regexp.MustCompile(`(?i)《|(?:其他|另一个|另一份|不同)(?:文档|表格|规范|标准)|跨(?:文件|文档|标准)|\b(?:other|another|both) (?:documents?|tables?|sources?|standards?)\b`)
)

// Scope a question about relationships in one table to its document if exactly one
// retrieved document has the two requested values in distinct groups of a row.
func scopeParallelTableQuestion(chat *types.ChatManage, results []*types.SearchResult) []*types.SearchResult {
	if len(results) < 2 || chat.WebSearchEnabled || len(chat.Images) != 0 || len(chat.Attachments) != 0 ||
		(chat.ChatModelSupportsVision && len(chat.ContextImages) != 0) || len(chat.KnowledgeIDs) > 1 ||
		!tableMentionRE.MatchString(chat.Query) || !tableRelationRE.MatchString(chat.Query) || otherSourceRE.MatchString(chat.Query) {
		return results
	}
	points := quantitiesIn(chat.Query)
	if len(points) != 2 {
		return results
	}
	var matched *types.SearchResult
	for _, result := range results {
		if result == nil {
			continue
		}
		if isPipelineWebReference(result) || (chat.FAQPriorityEnabled && result.ChunkType == string(types.ChunkTypeFAQ)) {
			return results
		}
		if result.ChunkType != string(types.ChunkTypeText) || result.KnowledgeID == "" || result.KnowledgeBaseID == "" {
			continue
		}
		for _, row := range chunker.InspectTableEvidence(result.Content, result.ContextHeader).ParallelRows {
			if parallelRowContains(row, points) {
				if matched != nil && (matched.KnowledgeID != result.KnowledgeID || matched.KnowledgeBaseID != result.KnowledgeBaseID) {
					return results
				}
				matched = result
				break
			}
		}
	}
	if matched == nil {
		return results
	}
	var scoped []*types.SearchResult
	for _, result := range results {
		if result != nil && result.KnowledgeID == matched.KnowledgeID && result.KnowledgeBaseID == matched.KnowledgeBaseID {
			scoped = append(scoped, result)
		}
	}
	return scoped
}

func parallelRowContains(row chunker.ParallelTableRow, points map[evidenceQuantity]bool) bool {
	matchedGroups := make(map[evidenceQuantity]int, len(points))
	for index, group := range row.Groups {
		for _, raw := range group.Cells {
			point, ok := exactTableQuantity(raw)
			if !ok || !points[point] {
				continue
			}
			if old, ok := matchedGroups[point]; ok && old != index {
				return false
			}
			matchedGroups[point] = index
		}
	}
	if len(matchedGroups) != len(points) {
		return false
	}
	first := -1
	for _, index := range matchedGroups {
		if first >= 0 && index != first {
			return true
		}
		first = index
	}
	return false
}

func exactTableQuantity(raw string) (evidenceQuantity, bool) {
	cell := comparisonText(strings.TrimSpace(raw))
	quantity := quantityRE.FindStringSubmatch(cell)
	if quantity == nil || quantity[0] != cell {
		return evidenceQuantity{}, false
	}
	return parseEvidenceQuantity(quantity[1], quantity[2]), true
}

type retrievedParallelTable struct {
	chunkID string
	rows    []chunker.ParallelTableRow
}

// Repeat exact matches near the question so a lookup does not depend on the
// model scanning every row of a long table. All fields come from one column group.
func buildTableMatchReminder(registry *modelcontext.Registry, tables []retrievedParallelTable, query string) string {
	if len(tables) == 0 {
		return ""
	}
	points := quantitiesIn(query)
	if len(points) == 0 || len(points) > 12 {
		return ""
	}
	seen := make(map[string]bool)
	var lines []string
	for _, table := range tables {
		citation := registry.CitationTemplate(registry.ChunkHandle(table.chunkID))
		for _, row := range table.rows {
			for _, group := range row.Groups {
				var matched bool
				for _, raw := range group.Cells {
					point, ok := exactTableQuantity(raw)
					matched = matched || (ok && points[point])
				}
				if !matched || len(lines) == 12 {
					continue
				}
				fields := make([]string, len(group.Cells))
				for i, cell := range group.Cells {
					fields[i] = evidenceText(group.Labels[i]) + " = " + evidenceText(cell)
				}
				line := "- " + escapeEvidenceQuote(evidenceText(group.Name)+" | "+strings.Join(fields, " | "))
				if len(line) > 540 || seen[line+citation] {
					continue
				}
				seen[line+citation] = true
				lines = append(lines, strings.TrimSpace(line+" "+citation))
			}
		}
	}
	if len(lines) == 0 {
		return ""
	}
	heading := "Exact data cell matches in the retrieved tables, with their original column groups and sources"
	if hasHan(query) {
		heading = "本轮表格中与提问数值精确匹配的字段，请按各自列组和出处逐项核对"
	}
	return "\n\n" + heading + "\n" + strings.Join(lines, "\n")
}
