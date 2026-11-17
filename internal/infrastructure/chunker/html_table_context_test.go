package chunker

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const groupedTableHead = "<table><tbody>\n" +
	"<tr><td colspan=\"2\">美标</td><td colspan=\"2\">国标</td></tr>\n" +
	"<tr><td>AWG</td><td>编码</td><td>mm²</td><td>编码</td></tr>\n"

func TestGroupedHTMLTableHeadersStayWithRowsAcrossParentBoundaries(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("# 技术规范\n\n## 图表 5.1\n\n")
	doc.WriteString(groupedTableHead)
	for i := 1; i <= 15; i++ {
		fmt.Fprintf(&doc, "<tr><td>%dAWG</td><td>%dA</td><td>%dmm²</td><td>GB%02d</td></tr>\n", i, i, i, i)
	}
	doc.WriteString("</tbody></table>\n\n## 下一节\n\n后续章节没有表格。\n")
	source := doc.String()
	runes := []rune(source)
	check := func(t *testing.T, chunks []Chunk) {
		t.Helper()
		found := false
		for _, c := range chunks {
			require.Equal(t, string(runes[c.Start:c.End]), c.Content)
			if strings.Contains(c.Content, ">GB14<") {
				found = true
				require.Contains(t, c.ContextHeader, "美标（跨2列） | 国标（跨2列）")
				require.Contains(t, c.EmbeddingContent(), "AWG | 编码 | mm² | 编码")
				require.NotContains(t, c.Content, "原表列标题")
			}
			if strings.Contains(c.Content, "后续章节") && !strings.Contains(c.Content, "<tr>") {
				require.NotContains(t, c.ContextHeader, "国标")
			}
		}
		require.True(t, found, "must include late table row")
	}
	for _, strategy := range []string{StrategyLegacy, StrategyHeading, StrategyAuto, StrategyHeuristic} {
		t.Run(strategy, func(t *testing.T) {
			parent := SplitterConfig{ChunkSize: 310, Strategy: strategy}
			child := SplitterConfig{ChunkSize: 155, Strategy: strategy}
			check(t, Split(source, parent))
			parts, _ := SplitWithDiagnostics(source, parent)
			check(t, parts)
			result := SplitParentChild(source, parent, child)
			require.Greater(t, len(result.Parents), 1)
			children := make([]Chunk, 0, len(result.Children))
			for _, c := range result.Children {
				children = append(children, c.Chunk)
			}
			check(t, children)
		})
	}
}

func TestHTMLTableHeadersAreLocalAndDoNotTreatBodyValuesAsLabels(t *testing.T) {
	fenced := "~~~html\n" + groupedTableHead + "<tr><td>tmpA</td></tr></tbody></table>\n~~~\n\n"
	plain := "<table>\n<tr><td>产品甲</td><td>产品乙</td></tr>\n<tr><td>100</td><td>200</td></tr>\n</table>\n\n"
	explicit := "<table>\n<thead>\n<tr><th rowspan=\"2\">等级</th><th colspan=\"2\">试验要求</th></tr>\n" +
		"<tr><th>长度</th><th>数量</th></tr></thead>\n<tbody>\n<tr><td>合格</td><td>10</td><td>2</td></tr>\n</tbody></table>\n\n"
	source := fenced + plain + explicit + "表外说明"
	contexts := findHTMLTableContexts(source)
	require.Len(t, contexts, 1)
	require.Contains(t, contexts[0].header, "等级（跨2行） | 试验要求（跨2列）")
	runes := []rune(source)
	require.Contains(t, string(runes[contexts[0].bodyStart:contexts[0].bodyEnd]), "<td>合格</td>")
	require.NotContains(t, contexts[0].header, "产品甲")
	c := Chunk{Content: "<tr><td>合格</td><td>10</td><td>2</td></tr>", Start: contexts[0].bodyStart, End: contexts[0].bodyEnd}
	chunks := []Chunk{c}
	attachHTMLTableContexts(chunks, contexts, runeLen(c.Content))
	require.Equal(t, c, chunks[0], "do not exceed an explicit token budget")
}
