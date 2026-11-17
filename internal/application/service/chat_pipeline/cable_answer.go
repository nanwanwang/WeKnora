package chatpipeline

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	cableCodeRow = regexp.MustCompile(`(?im)^[ \t]*([0-9]+)\s*AWG[ \t]+([0-9A-Z]{1,8})[ \t]+(` + evidenceNumber + `)\s*mm(?:²|2)[ \t]+([0-9A-Z]{1,8})[ \t]*$`)
	cableSpec    = regexp.MustCompile(`(?i)[0-9]+AWG|` + evidenceNumber + `mm(?:²|2)`)
	cableAWGRule = regexp.MustCompile(`(?i)2\.4单根线命名分为电子线与电缆线,规定<=(` + evidenceNumber +
		`)mm²/([0-9]+)AWG的命名为电子线,>(` + evidenceNumber + `)mm²/([0-9]+)AWG的为电缆线`)
	htmlRowEnd = regexp.MustCompile(`(?i)</tr\s*>`)
)

type cableCode struct {
	label, group, code, citation string
}

func cableTableReply(query string, sources []verifiedSource) (string, bool) {
	comparison := containsAny(query, "等价", "互换", "替代", "换算")
	if comparison && !containsAny(query, "表", "同行", "同一行") {
		return "", false
	}
	var requested []string
	seen := map[string]bool{}
	for _, spec := range cableSpec.FindAllString(query, -1) {
		key := normalizeCableSpec(spec)
		if !seen[key] {
			requested = append(requested, key)
			seen[key] = true
		}
	}
	if len(requested) == 0 || len(requested) > 16 {
		return "", false
	}
	codes := make(map[string]cableCode)
	for _, source := range sources {
		header := compactEvidence(source.result.ContextHeader) + source.text
		header = strings.NewReplacer("(跨2列)", "", "|", "", "线径", "线经", "mm2", "mm²").Replace(header)
		if !strings.Contains(header, "美标国标AWG线经描述mm²线经描述") {
			continue
		}
		rows := evidencePlainText(htmlRowEnd.ReplaceAllString(source.result.Content, "\n"))
		for _, m := range cableCodeRow.FindAllStringSubmatch(rows, -1) {
			for _, entry := range []cableCode{
				{label: m[1] + " AWG", group: "美标", code: strings.ToUpper(m[2]), citation: source.citation},
				{label: m[3] + " mm²", group: "国标", code: strings.ToUpper(m[4]), citation: source.citation},
			} {
				key := normalizeCableSpec(entry.label)
				if old, ok := codes[key]; ok && old.code != entry.code {
					return "", false
				}
				if _, ok := codes[key]; !ok {
					codes[key] = entry
				}
			}
		}
	}
	var reply strings.Builder
	reply.WriteString("《线缆及端子规格描述规范》的编码表按各自列组查询线径描述。\n\n")
	reply.WriteString("| 规格 | 列组 | 线径编码 |\n| --- | --- | --- |\n")
	for _, spec := range requested {
		code, ok := codes[spec]
		if !ok {
			return "", false
		}
		fmt.Fprintf(&reply, "| %s | %s | **%s**%s |\n", code.label, code.group, code.code, code.citation)
	}
	if comparison {
		reply.WriteString("\n同行只表示表格的并列排版。换算、等效和替代关系需要可引用的独立依据。")
	}
	return reply.String(), true
}

func normalizeCableSpec(spec string) string {
	return strings.ReplaceAll(strings.ToLower(compactEvidence(spec)), "mm2", "mm²")
}

func awgBoundaryReply(query string, sources []verifiedSource) (string, bool) {
	if containsAny(query, "选型", "端子型号", "标注格式", "转换成", "换算成") {
		return "", false
	}
	for _, source := range sources {
		m := cableAWGRule.FindStringSubmatch(source.text)
		if m == nil || m[1] != m[3] || m[2] != m[4] {
			continue
		}
		for _, requested := range cableSpec.FindAllString(query, -1) {
			if strings.HasSuffix(strings.ToUpper(requested), "AWG") &&
				!strings.EqualFold(requested, m[2]+"AWG") {
				return "", false
			}
		}
		return fmt.Sprintf("按《线缆及端子规格描述规范》第 2.4 条，截面积 **≤ %s mm²** 的单根线命名为电子线，**> %s mm²** 的命名为电缆线。%s\n\n原文在两条条件中都并列写了「/%s AWG」。这两句明确了 mm² 的比较符。**恰好 %s AWG 的归类待确认**，应以文件负责人补充的 AWG 判定规则为准。",
			m[1], m[3], source.citation, m[2], m[2]), true
	}
	return "", false
}
