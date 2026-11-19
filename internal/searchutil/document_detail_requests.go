package searchutil

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DocumentDetailRequest pairs a normalized title or standard edition with its subquestion.
type DocumentDetailRequest struct {
	Title    string
	Question string
}

var standardTitlePrefix = regexp.MustCompile(`(?i)^(?:gb[ /-]*t|gb|iec|iso)\s*[0-9]+(?:[.-][0-9]+)*[-:][12][0-9]{3}\b`)

// StandardDocumentNumber retains the edition while normalizing GB/T filename spellings.
func StandardDocumentNumber(title string) string {
	code := NormalizeDocumentTitle(standardTitlePrefix.FindString(strings.TrimSpace(title)))
	return strings.Replace(strings.Replace(code, "gb/t", "gbt", 1), "gb-t", "gbt", 1)
}

// MatchesEvidenceDocument compares a normalized request with a stored filename.
func MatchesEvidenceDocument(requestTitle, title string) bool {
	if code := StandardDocumentNumber(requestTitle); code != "" && code == requestTitle {
		return StandardDocumentNumber(title) == code
	}
	return NormalizeDocumentTitle(title) == requestTitle
}

// DocumentDetailRequests recognizes independent subquestions explicitly scoped
// to different documents. A general document comparison takes the usual path.
func DocumentDetailRequests(query string) []DocumentDetailRequest {
	var requests []DocumentDetailRequest
	seen := make(map[string]bool)
	parts := strings.FieldsFunc(query, func(r rune) bool { return strings.ContainsRune("。?!？！；;\n", r) })
	for _, part := range parts {
		part = strings.TrimSpace(part)
		for _, prefix := range []string{"按", "根据", "依据"} {
			value, ok := strings.CutPrefix(part, prefix)
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			title, rest := "", ""
			if quoted := quotedDocumentTitle.FindStringSubmatchIndex(value); quoted != nil && quoted[0] == 0 {
				name := value[quoted[2]:quoted[3]]
				title, rest = NormalizeDocumentTitle(name), value[quoted[1]:]
				if code := standardTitlePrefix.FindString(strings.TrimSpace(name)); code != "" &&
					NormalizeDocumentTitle(code) == title {
					title = StandardDocumentNumber(code)
				}
			} else if standard := standardTitlePrefix.FindStringIndex(value); standard != nil {
				title, rest = StandardDocumentNumber(value[:standard[1]]), value[standard[1]:]
			}
			rest = strings.TrimLeftFunc(rest, func(r rune) bool {
				return unicode.IsSpace(r) || strings.ContainsRune("，,:：", r)
			})
			if title != "" && IsTopicQuery(title) && rest != "" && utf8.RuneCountInString(rest) <= 160 {
				if seen[title] {
					return nil
				}
				seen[title] = true
				requests = append(requests, DocumentDetailRequest{Title: title, Question: rest})
			}
			break
		}
	}
	if len(requests) < 2 || len(requests) > 5 {
		return nil
	}
	return requests
}

// RequestedEvidenceTitles lists the documents named in scoped detail or overview questions.
func RequestedEvidenceTitles(query string) []string {
	details := DocumentDetailRequests(query)
	if len(details) == 0 {
		return NamedDocumentOverviewTitles(query)
	}
	titles := make([]string, 0, len(details))
	for _, detail := range details {
		titles = append(titles, detail.Title)
	}
	return titles
}
