package chatpipeline

import (
	"strings"

	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/searchutil"
)

type verifiedDocumentCode struct {
	title, lookup, code, ref string
}

// A generated overview can omit a code even when the title is covered. Keep
// only a unique code stated in citation-enabled evidence from that exact file.
func verifiedNamedDocumentCodes(
	query string, sources []reviewSource, registry *modelcontext.Registry,
) []verifiedDocumentCode {
	var codes []verifiedDocumentCode
	for _, title := range searchutil.NamedDocumentOverviewTitles(query) {
		var code verifiedDocumentCode
		document, ambiguous := "", false
		for _, source := range sources {
			if source.imageOCR || source.ref == "" || searchutil.NormalizeDocumentTitle(source.title) != title {
				continue
			}
			if document != "" && source.document != document {
				ambiguous = true
				break
			}
			document = source.document
			for _, m := range documentCode.FindAllStringSubmatch(evidenceText(source.body), -1) {
				if code.code != "" && !strings.EqualFold(code.code, m[1]) {
					ambiguous = true
					break
				}
				if code.code == "" {
					code = verifiedDocumentCode{
						title: strings.TrimSuffix(source.title, ".pdf"), lookup: title,
						code: m[1], ref: registry.DecodeOutputText(source.ref),
					}
				}
			}
			if ambiguous {
				break
			}
		}
		if !ambiguous && code.code != "" && code.ref != "" {
			codes = append(codes, code)
		}
	}
	return codes
}

func supplementNamedDocumentCodes(answer string, codes []verifiedDocumentCode) string {
	if len(codes) == 0 {
		return ""
	}
	body := searchutil.NormalizeDocumentTitle(citationRE.ReplaceAllString(answer, ""))
	var lines []string
	for _, code := range codes {
		if !strings.Contains(body, code.lookup) || strings.Contains(body, strings.ToLower(code.code)) {
			continue
		}
		lines = append(lines, "- 《"+escapeEvidenceQuote(code.title)+"》 "+escapeEvidenceQuote(code.code)+" "+code.ref)
	}
	if len(lines) == 0 {
		return ""
	}
	heading := "文件编号"
	if !hasHan(body) {
		heading = "Document numbers"
	}
	return "\n\n**" + heading + "**\n\n" + strings.Join(lines, "\n")
}
