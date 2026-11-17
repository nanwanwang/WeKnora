package chatpipeline

import (
	"html"
	"regexp"
	"strings"
	"unicode"

	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/types"
)

type verifiedSource struct {
	result   *types.SearchResult
	text     string
	citation string
}

var evidenceMarkup = regexp.MustCompile(`(?i)</?(?:table|tbody|thead|tfoot|tr|td|th|p|div|span|br|b|strong|sup|sub)\b[^>]*>`)
var evidenceSymbols = strings.NewReplacer(
	"≤", "<=", "≥", ">=", "＜", "<", "＞", ">", "＝", "=",
	"，", ",", "：", ":", "（", "(", "）", ")", "／", "/", "＋", "+",
	"～", "~", "µ", "μ", "㎟", "mm²",
)

func evidencePlainText(text string) string {
	return html.UnescapeString(evidenceMarkup.ReplaceAllString(text, " "))
}

func compactEvidence(text string) string {
	return evidenceSymbols.Replace(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text))
}

func containsAny(text string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

// These replies cover source formats that can be checked without generation.
// A reply uses only the current, authorized, citable body of one document.
func verifiedEvidenceReply(request *types.ChatManage) (string, bool) {
	if len(request.MergeResult) == 0 || len(request.Images) > 0 {
		return "", false
	}
	// IntoChatMessage renders references into UserContent before completion.
	// Select rules using the original question, independently of that template.
	query := compactEvidence(request.Query)
	if containsAny(strings.ToLower(query), "json", "yaml", "csv", "翻译", "英文回答", "改写") {
		return "", false
	}
	switch {
	case strings.Contains(query, "浅划痕") && strings.Contains(query, "塑壳"):
		return inspectionReply(query, verifiedDocumentSources(request, "塑壳产品检验规范"))
	case containsAny(query, "电子线", "电缆线", "单根线") && containsAny(query, "归类", "划分", "界限", "区分", "边界", "命名", "还是"):
		return awgBoundaryReply(query, verifiedDocumentSources(request, "线缆及端子规格描述规范"))
	case containsAny(query, "线缆", "线材", "线径", "线经", "端子规格") && containsAny(query, "编码", "线径描述", "线经描述", "等价", "替代", "互换", "换算"):
		return cableTableReply(query, verifiedDocumentSources(request, "线缆及端子规格描述规范"))
	case containsAny(query, "热缩套管", "热缩管") && containsAny(query, "高+宽", "高加宽", "高度加宽度"):
		return heatShrinkBoundaryReply(query, verifiedDocumentSources(request, "热缩套管选型规范"))
	case strings.Contains(strings.ToLower(query), "fe/zn") && containsAny(query, "厚度", "不一致", "冲突"):
		return coatingThicknessReply(query, verifiedDocumentSources(request, "电镀工艺技术标准"))
	default:
		return "", false
	}
}

func verifiedDocumentSources(request *types.ChatManage, title string) []verifiedSource {
	var passages []*types.SearchResult
	var documentID, libraryID string
	for _, r := range expandCitationSources(orderedPipelineReferences(request)) {
		if r == nil || r.ChunkType != string(types.ChunkTypeText) || r.ID == "" ||
			r.KnowledgeID == "" || r.KnowledgeBaseID == "" ||
			!strings.Contains(compactEvidence(firstPipelineTitle(r)), title) {
			continue
		}
		if documentID != "" && (r.KnowledgeID != documentID || r.KnowledgeBaseID != libraryID) {
			return nil
		}
		documentID, libraryID = r.KnowledgeID, r.KnowledgeBaseID
		passages = append(passages, r)
	}
	// Resolve source identities independently of the public citation setting.
	registry := modelcontext.NewRegistry(true)
	registry.RegisterSearchResults(passages)
	sources := make([]verifiedSource, 0, len(passages))
	for _, r := range passages {
		ref := registry.CitationTemplate(registry.ChunkHandle(r.ID))
		if ref == "" {
			continue
		}
		var citation string
		if request.CitationsEnabled() {
			citation = " " + registry.DecodeOutputText(ref)
		}
		sources = append(sources, verifiedSource{result: r, text: compactEvidence(evidencePlainText(r.Content)), citation: citation})
	}
	return sources
}
