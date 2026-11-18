package searchutil

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// LiteralLookup accepts pasted values, not questions or aggregate requests.
// It preserves punctuation and whitespace inside the value.
func LiteralLookup(query string) string {
	q := strings.TrimSpace(query)
	if n := utf8.RuneCountInString(q); n < 3 || n > 128 || strings.ContainsAny(q, "\n\r\t?？!！。;；") {
		return ""
	}
	for _, term := range []string{"多少", "统计", "合计", "总共", "总数", "占比", "趋势", "平均", "哪些", "为什么", "怎么", "如何", "查询", "查一下", "列出", "汇总", "对比", "最多", "最少", "所有", "是否", "有没有", " count ", " total ", " how ", " what ", " sum ", " list ", " show ", " find ", " average "} {
		if strings.Contains(" "+strings.ToLower(q)+" ", term) {
			return ""
		}
	}
	return q
}

func identifierByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || strings.ContainsRune("-_./", rune(b))
}

func IsIdentifierLookup(query string) bool {
	if LiteralLookup(query) == "" {
		return false
	}
	digit := false
	for _, b := range []byte(strings.ToLower(query)) {
		if !identifierByte(b) && b != '(' && b != ')' {
			return false
		}
		if b >= '0' && b <= '9' {
			digit = true
		}
	}
	return digit
}

// Numeric identifiers require whole-token matches: WO-123 cannot match WO-1234.
// Pasted prose remains a literal substring, including Chinese cell values.
func ContainsLiteral(content, query string) bool {
	c, q := strings.ToLower(content), strings.ToLower(query)
	if q == "" {
		return false
	}
	code := IsIdentifierLookup(q)
	for offset := 0; offset <= len(c)-len(q); {
		i := strings.Index(c[offset:], q)
		if i < 0 {
			return false
		}
		i += offset
		end := i + len(q)
		if !code || (i == 0 || !identifierByte(c[i-1])) && (end == len(c) || !identifierByte(c[end])) {
			return true
		}
		offset = i + 1
	}
	return false
}

// QualifiedRecordLookup recognizes a pasted identifier followed by a short
// fault phrase. Questions, aggregates, and image queries are handled elsewhere.
func QualifiedRecordLookup(query string) (identifier, qualifier string) {
	query = LiteralLookup(query)
	split := strings.IndexFunc(query, unicode.IsSpace)
	if split < 0 {
		return "", ""
	}
	identifier, qualifier = query[:split], strings.TrimSpace(query[split:])
	if !IsIdentifierLookup(identifier) || !strings.ContainsAny(identifier, "-_./") ||
		qualifier == "" || utf8.RuneCountInString(qualifier) > 32 ||
		strings.IndexFunc(qualifier, unicode.IsSpace) >= 0 ||
		strings.IndexFunc(qualifier, func(r rune) bool { return unicode.In(r, unicode.Han) }) < 0 {
		return "", ""
	}
	for _, marker := range []string{"是什么", "是啥", "请问", "能否", "能不能", "有吗", "行吗", "怎么办", "给我", "能用", "维修方法", "修理方法"} {
		if strings.Contains(qualifier, marker) {
			return "", ""
		}
	}
	return identifier, qualifier
}

// ContainsRecordMatch requires both pasted terms on the same source line so
// that two neighboring table rows cannot satisfy the query together.
func ContainsRecordMatch(content, identifier, qualifier string) bool {
	if qualifier == "" {
		return ContainsLiteral(content, identifier)
	}
	for line := range strings.SplitSeq(content, "\n") {
		if ContainsLiteral(line, identifier) && ContainsLiteral(line, qualifier) {
			return true
		}
	}
	return false
}
