package searchutil

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// NormalizeDocumentTitle compares short topic queries with document filenames.
// Punctuation in model numbers and standard numbers remains significant.
func NormalizeDocumentTitle(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch path.Ext(value) {
	case ".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx", ".txt", ".md":
		value = strings.TrimSuffix(value, path.Ext(value))
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}

// IsTopicQuery intentionally excludes long questions and multi-sentence requests.
// Explicit document scopes are checked separately by the caller.
func IsTopicQuery(query string) bool {
	n := utf8.RuneCountInString(NormalizeDocumentTitle(query))
	return n >= 2 && n <= 40 && len(strings.Fields(query)) <= 6 && !strings.ContainsAny(query, "?？!！。;；\n")
}

// DocumentTitleHint uses the title explicitly given in the query, or matches
// a short query to an existing filename. The caller checks eligibility.
func DocumentTitleHint(query string) (title string, topic bool) {
	normalized := NormalizeDocumentTitle(query)
	if strings.ContainsAny(normalized, "《》") {
		if strings.Count(normalized, "《") == 1 && strings.Count(normalized, "》") == 1 {
			_, remainder, _ := strings.Cut(normalized, "《")
			if name, _, ok := strings.Cut(remainder, "》"); ok && name != "" {
				return NormalizeDocumentTitle(name), false
			}
		}
		return "", false
	}
	if IsTopicQuery(query) {
		return normalized, true
	}
	return "", false
}

// IsBroadTopicQuery avoids spreading a concrete interface, part number, standard
// number or filename request across generic documents. It does not affect title
// recall; it only controls the optional document-diversity preference.
func IsBroadTopicQuery(query string) bool {
	if !IsTopicQuery(query) {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(query))
	for _, r := range lower {
		if unicode.IsDigit(r) {
			return false
		}
	}
	if strings.ContainsAny(lower, "/\\_-.") {
		return false
	}
	for _, specific := range []string{"接口", "型号", "条款", "牌号", "interface", "part number"} {
		if strings.Contains(lower, specific) {
			return false
		}
	}
	return true
}
