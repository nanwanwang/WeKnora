package chatpipeline

import (
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	evidenceNumberPattern = `[+-]?[0-9]+(?:\.[0-9]+)?`
	// Parse unit labels without converting between units or comparing across them.
	evidenceUnitPattern = `(?:[A-Za-zµμΩ°%‰][A-Za-zµμΩ°%‰0-9^]*|毫米|厘米|毫秒|分钟|小时|件|条|个|处|片|台|次|秒|米|天)`
)

var (
	quantityRE       = regexp.MustCompile(`(` + evidenceNumberPattern + `)[ \t]*(` + evidenceUnitPattern + `)`)
	comparisonRE     = regexp.MustCompile(`([<>]=?)[ \t]*(` + evidenceNumberPattern + `)[ \t]*(` + evidenceUnitPattern + `)?`)
	compoundRE       = regexp.MustCompile(`([<>]=?)[ \t]*(` + evidenceNumberPattern + `)[ \t]*(` + evidenceUnitPattern + `)[ \t]*/[ \t]*(` + evidenceNumberPattern + `)[ \t]*(` + evidenceUnitPattern + `)`)
	markupRE         = regexp.MustCompile(`(?i)</?(?:table|tbody|thead|tfoot|tr|td|th|p|div|span|br|b|strong|em|sup|sub)\b[^>]*>`)
	citationRE       = regexp.MustCompile(`(?i)<(?:kb|ref|web)\b[^>]*>`)
	evidenceNotation = strings.NewReplacer(
		"≤", "<=", "≥", ">=", "＜", "<", "＞", ">", "−", "-", "﹣", "-",
		`\leqslant`, "<=", `\geqslant`, ">=", `\leq`, "<=", `\geq`, ">=",
		`\le`, "<=", `\ge`, ">=", `\text`, "", `\mathrm`, "", `\,`, " ",
		"小于等于", "<=", "大于等于", ">=", "不超过", "<=", "不大于", "<=",
		"不少于", ">=", "不小于", ">=", "至少", ">=", "至多", "<=",
		"**", "", "`", "", "$", "", "{", "", "}", "",
	)
)

type evidenceQuantity struct {
	number string
	unit   string
}

type evidenceComparison struct {
	quantity evidenceQuantity
	operator string
	index    int
}

func comparisonText(text string) string {
	return evidenceNotation.Replace(norm.NFKC.String(text))
}

func parseEvidenceQuantity(number, unit string) evidenceQuantity {
	number = strings.TrimPrefix(number, "+")
	whole, fraction, _ := strings.Cut(number, ".")
	negative := strings.HasPrefix(whole, "-")
	whole = strings.TrimLeft(strings.TrimPrefix(whole, "-"), "0")
	if whole == "" {
		whole = "0"
	}
	fraction = strings.TrimRight(fraction, "0")
	number = whole
	if fraction != "" {
		number += "." + fraction
	}
	if negative && number != "0" {
		number = "-" + number
	}
	// The terms below are count labels; there is no numerical conversion.
	switch unit {
	case "pcs", "pc", "条", "个", "处", "片", "件":
		unit = "count"
	}
	return evidenceQuantity{number: number, unit: unit}
}

func quantitiesIn(text string) map[evidenceQuantity]bool {
	quantities := make(map[evidenceQuantity]bool)
	for _, m := range quantityRE.FindAllStringSubmatch(comparisonText(text), -1) {
		quantities[parseEvidenceQuantity(m[1], m[2])] = true
	}
	return quantities
}

func comparisonsIn(normalized string) []evidenceComparison {
	var comparisons []evidenceComparison
	for _, match := range comparisonRE.FindAllStringSubmatchIndex(normalized, -1) {
		part := func(group int) string {
			start, end := match[2*group], match[2*group+1]
			if start < 0 {
				return ""
			}
			return normalized[start:end]
		}
		comparisons = append(comparisons, evidenceComparison{
			quantity: parseEvidenceQuantity(part(2), part(3)),
			operator: part(1), index: match[0],
		})
	}
	return comparisons
}

func evidenceText(text string) string {
	return strings.Join(strings.Fields(html.UnescapeString(markupRE.ReplaceAllString(text, " "))), " ")
}

func hasHan(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func withinRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit])
}
