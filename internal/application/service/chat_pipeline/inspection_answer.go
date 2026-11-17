package chatpipeline

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const evidenceNumber = `[0-9]+(?:\.[0-9]+)?`

var (
	inspectionGrade     = regexp.MustCompile(`(?i)([A-D])(?:级)?面`)
	inspectionGradePair = regexp.MustCompile(`(?i)([A-D])(?:级面)?(?:[、,/]|和|与|及)([A-D])(?:级)?面`)
	scratchConditions   = `L<=(` + evidenceNumber + `)mm,n<=([0-9]+)(?:pcs|条),DS>=(` + evidenceNumber + `)mm`
	scratchTable        = regexp.MustCompile(`(?i)浅划痕(?:划伤的痕迹)?A级面` + scratchConditions + `目视(MI|MA|CR)B级面` + scratchConditions)
	scratchLength       = regexp.MustCompile(`(?i)(?:每条(?:长(?:度)?)?|长度(?:均)?|长|L=)(?:为|是|[:=])?(` + evidenceNumber + `)mm`)
	scratchCount        = regexp.MustCompile(`(?i)(?:([0-9]+)条(?:浅划痕|划痕)|(?:共|数量[为=:]?|n=)([0-9]+)(?:条|pcs)?)`)
	scratchSpacing      = regexp.MustCompile(`(?i)(?:间距|相距|间隔|DS=)(?:为|是|[:=])?(` + evidenceNumber + `)mm`)
)

type scratchLimit struct {
	length, count, spacing string
}

func inspectionReply(query string, sources []verifiedSource) (string, bool) {
	if !containsAny(query, "长度", "数量", "间距", "允收", "判定", "合格", "通过", "验收", "允许", "缺陷等级", "最多", "上限", "几条") ||
		containsAny(query, "深划痕", "压铸", "拉模", "缺胶", "色差", "修复", "怎么处理") {
		return "", false
	}
	grades := make(map[string]bool)
	for _, m := range inspectionGrade.FindAllStringSubmatch(query, -1) {
		grades[strings.ToUpper(m[1])] = true
	}
	for _, m := range inspectionGradePair.FindAllStringSubmatch(query, -1) {
		grades[strings.ToUpper(m[1])] = true
		grades[strings.ToUpper(m[2])] = true
	}
	if len(grades) == 0 || grades["C"] || grades["D"] {
		return "", false
	}
	var match []string
	var citation string
	for _, source := range sources {
		row := scratchTable.FindStringSubmatch(source.text)
		if row == nil {
			continue
		}
		if match != nil && strings.ToUpper(strings.Join(row[1:], "|")) != strings.ToUpper(strings.Join(match[1:], "|")) {
			return "", false
		}
		if match == nil {
			match, citation = row, source.citation
		}
	}
	if match == nil {
		return "", false
	}
	limits := map[string]scratchLimit{
		"A": {length: match[1], count: match[2], spacing: match[3]},
		"B": {length: match[5], count: match[6], spacing: match[7]},
	}
	var reply strings.Builder
	reply.WriteString("《塑壳产品检验规范》的浅划痕允收条件如下。\n\n")
	for _, grade := range []string{"A", "B"} {
		if !grades[grade] {
			continue
		}
		limit := limits[grade]
		fmt.Fprintf(&reply, "- **%s级面**　长度 L ≤ %s mm，数量 n ≤ %s 条，间距 DS ≥ %s mm。%s\n",
			grade, limit.length, limit.count, limit.spacing, citation)
		if len(grades) == 1 && containsAny(query, "合格", "通过", "判定", "允收吗", "接受吗") {
			reply.WriteString(scratchDecision(query, limit))
		}
	}
	fmt.Fprintf(&reply, "\n缺陷等级为 **%s**。%s", strings.ToUpper(match[4]), citation)
	return reply.String(), true
}

func scratchDecision(query string, limit scratchLimit) string {
	lengths := scratchLength.FindAllStringSubmatch(query, 2)
	counts := scratchCount.FindAllStringSubmatch(query, 2)
	distances := scratchSpacing.FindAllStringSubmatch(query, 2)
	if len(lengths) != 1 || len(counts) != 1 || len(distances) != 1 || containsAny(query, "分别", "另一", "最短", "或者", "或有", "总长") {
		return ""
	}
	length, count, spacing := lengths[0][1], counts[0][1]+counts[0][2], distances[0][1]
	l, lErr := strconv.ParseFloat(length, 64)
	n, nErr := strconv.Atoi(count)
	ds, dsErr := strconv.ParseFloat(spacing, 64)
	maxLength, maxLErr := strconv.ParseFloat(limit.length, 64)
	maxCount, maxNErr := strconv.Atoi(limit.count)
	minSpacing, minDSErr := strconv.ParseFloat(limit.spacing, 64)
	if lErr != nil || nErr != nil || dsErr != nil || maxLErr != nil || maxNErr != nil || minDSErr != nil || n < 2 {
		return ""
	}
	var reasons []string
	if l > maxLength {
		reasons = append(reasons, fmt.Sprintf("长度 %s mm 超过 %s mm", length, limit.length))
	}
	if n > maxCount {
		reasons = append(reasons, fmt.Sprintf("数量 %s 条超过 %s 条", count, limit.count))
	}
	if ds < minSpacing {
		reasons = append(reasons, fmt.Sprintf("间距 %s mm 小于 %s mm", spacing, limit.spacing))
	}
	if len(reasons) > 0 {
		return "\n所述浅划痕**不符合本条允收条件**。" + strings.Join(reasons, "，") + "。\n"
	}
	return "\n所述长度、数量和间距均满足**该浅划痕条款**。\n"
}
