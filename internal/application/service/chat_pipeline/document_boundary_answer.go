package chatpipeline

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	heatRatedRange   = regexp.MustCompile(`(?i)480V?(?:~|至|到|-|–|—)1140V`)
	heatDimension    = regexp.MustCompile(`(?:高\+宽|高加宽|高度加宽度|高宽合计)(?:之和|合计)?(?:正好|恰好|等于|为|是|=|:)*(` + evidenceNumber + `)mm`)
	heatTable        = regexp.MustCompile(`(?i)最小方铜排的高\+宽(<=|<)(` + evidenceNumber + `)mm热缩套管:(` + evidenceNumber + `)V\(套两层\).{0,160}?最小方铜排的高\+宽(>=|>)(` + evidenceNumber + `)mm热缩套管:(` + evidenceNumber + `)kV`)
	heatDiagram      = regexp.MustCompile(`(?i)热缩管选型母排额定电压:480V~1140V.{0,40}?当最小方铜排的高\+宽(<=|<)(` + evidenceNumber + `)mm时:当最小方铜排的高\+宽(>=|>)(` + evidenceNumber + `)mm时:热缩套管:(` + evidenceNumber + `)V\(套两层\).{0,130}?热缩套管:(` + evidenceNumber + `)kV`)
	evidenceOperator = strings.NewReplacer("<=", "≤", ">=", "≥")

	coatingMark       = regexp.MustCompile(`(?i)Fe/Zn[0-9]+/HC`)
	coatingTable      = regexp.MustCompile(`(?i)(` + evidenceNumber + `)μm以上(?:GB/T9799-)?(Fe/Zn[0-9]+/HC)`)
	coatingLabelNote  = regexp.MustCompile(`(?i)(Fe/Zn[0-9]+/HC)为镀环保彩锌,最小厚度要求为(` + evidenceNumber + `)μm`)
	coatingProduction = regexp.MustCompile(`钢铁镀锌[^。]{0,100}镀环保彩锌,厚度为(` + evidenceNumber + `)μm以上`)
)

func heatShrinkBoundaryReply(query string, sources []verifiedSource) (string, bool) {
	// The same document has other voltage bands and a different sizing rule.
	// Check only the band and measured dimension that the two passages describe.
	dimension := heatDimension.FindStringSubmatch(query)
	if dimension == nil || !heatRatedRange.MatchString(query) || containsAny(query, "缩前内径", "壁厚") {
		return "", false
	}
	point, err := strconv.ParseFloat(dimension[1], 64)
	if err != nil {
		return "", false
	}
	var table, diagram []string
	var tableCitation, diagramCitation string
	for _, source := range sources {
		if m := heatTable.FindStringSubmatch(source.text); m != nil {
			low, lowErr := strconv.ParseFloat(m[2], 64)
			high, highErr := strconv.ParseFloat(m[5], 64)
			if lowErr == nil && highErr == nil && low == point && high == point {
				if table != nil && strings.Join(m[1:], "|") != strings.Join(table[1:], "|") {
					return "", false
				}
				table, tableCitation = m, source.citation
			}
		}
		if m := heatDiagram.FindStringSubmatch(source.text); m != nil {
			low, lowErr := strconv.ParseFloat(m[2], 64)
			high, highErr := strconv.ParseFloat(m[4], 64)
			if lowErr == nil && highErr == nil && low == point && high == point {
				if diagram != nil && strings.Join(m[1:], "|") != strings.Join(diagram[1:], "|") {
					return "", false
				}
				diagram, diagramCitation = m, source.citation
			}
		}
	}
	if table == nil {
		return "", false
	}
	var answer strings.Builder
	answer.WriteString("《热缩套管选型规范》对 480–1140 V 母排按最小方铜排的高加宽选型。\n\n")
	fmt.Fprintf(&answer, "- **表格**　%s %s mm 对应 %s V 套两层，%s %s mm 对应 %s kV。%s\n",
		evidenceOperator.Replace(table[1]), table[2], table[3],
		evidenceOperator.Replace(table[4]), table[5], table[6], tableCitation)
	if diagram == nil {
		fmt.Fprintf(&answer, "- **思维导图**　本轮片段缺少可以逐项对照的边界文字。\n\n**恰好 %s mm 的选型待确认**，请文件负责人核对图示和表格。", dimension[1])
		return answer.String(), true
	}
	fmt.Fprintf(&answer, "- **思维导图**　%s %s mm 对应 %s V 套两层，%s %s mm 对应 %s kV。%s\n",
		evidenceOperator.Replace(diagram[1]), diagram[2], diagram[5],
		evidenceOperator.Replace(diagram[3]), diagram[4], diagram[6], diagramCitation)
	if table[1] == diagram[1] && table[4] == diagram[3] && table[3] == diagram[5] && table[6] == diagram[6] {
		switch {
		case table[1] == "<" && table[4] == ">=":
			fmt.Fprintf(&answer, "\n图表一致，恰好 %s mm 落在 **%s kV** 条件。", dimension[1], table[6])
			return answer.String(), true
		case table[1] == "<=" && table[4] == ">":
			fmt.Fprintf(&answer, "\n图表一致，恰好 %s mm 落在 **%s V 套两层**条件。", dimension[1], table[3])
			return answer.String(), true
		}
	}
	fmt.Fprintf(&answer, "\n**恰好 %s mm 的选型待确认**，请文件负责人统一该边界。", dimension[1])
	return answer.String(), true
}

func coatingThicknessReply(query string, sources []verifiedSource) (string, bool) {
	marks := coatingMark.FindAllString(query, 2)
	if len(marks) != 1 {
		return "", false
	}
	mark := marks[0]
	var tableValue, noteValue, productionValue string
	var tableCitation, noteCitation, productionCitation string
	for _, source := range sources {
		for _, m := range coatingTable.FindAllStringSubmatch(source.text, -1) {
			if strings.EqualFold(m[2], mark) {
				if tableValue != "" && tableValue != m[1] {
					return "", false
				}
				tableValue, tableCitation = m[1], source.citation
				mark = m[2]
			}
		}
		for _, m := range coatingLabelNote.FindAllStringSubmatch(source.text, -1) {
			if strings.EqualFold(m[1], mark) {
				if noteValue != "" && noteValue != m[2] {
					return "", false
				}
				noteValue, noteCitation = m[2], source.citation
			}
		}
		if m := coatingProduction.FindStringSubmatch(source.text); m != nil {
			if productionValue != "" && productionValue != m[1] {
				return "", false
			}
			productionValue, productionCitation = m[1], source.citation
		}
	}
	if noteValue == "" || tableValue == "" || noteValue == tableValue {
		return "", false
	}
	var answer strings.Builder
	fmt.Fprintf(&answer, "《电镀工艺技术标准》中的 **%s** 环保彩锌厚度存在不同写法。\n\n", mark)
	fmt.Fprintf(&answer, "- **设计要求表格**　%s μm 以上。%s\n", tableValue, tableCitation)
	if productionValue != "" {
		fmt.Fprintf(&answer, "- **生产参考要求**　钢铁镀环保彩锌的厚度为 %s μm 以上。%s\n", productionValue, productionCitation)
	}
	fmt.Fprintf(&answer, "- **标识说明示例**　最小厚度为 %s μm。%s\n", noteValue, noteCitation)
	answer.WriteString("\n**验收厚度待确认**，请文件负责人统一表格、生产要求和标识说明。")
	return answer.String(), true
}
