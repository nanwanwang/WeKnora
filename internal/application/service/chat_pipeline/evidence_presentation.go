package chatpipeline

import (
	"fmt"
	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/types"
	"regexp"
	"strings"
)

const evidencePresentationPrompt = `

## 本轮资料的回答与展示
- 按问题组织答案；概述类问题先介绍概念和主要措施，再给具体例子。优先使用标题直接匹配且正文相关的资料，其他文档作为补充。特定接口、器件或电源方案的参数不可泛化为所有产品的通用要求。
- 来源引用开启时，在关键结论后复制本轮 citation_catalog 提供的 citation 原文；每条只支持对应 source 片段。不要自行编写编号、引用标签或只在末尾笼统列出来源。可在句中自然提及实际采用的文档标题。
- 当前资料包含与解释直接相关的原理图、结构图或流程图时，择优展示 1–2 张（citation_catalog 的 available_illustration 给出本轮可用的原图及其 source_citation，可直接复制；不要只描述“如下图”却遗漏图片），在相关说明旁用 Markdown 图片语法复制原图地址，并注明该图的来源。优先主题文档中的解释性示意图；不要为配图加入无关图片、封面、标志或水印。
- 只能展示本轮提供的图片地址，不得猜测或修改地址，也不能将历史图片当成本轮证据。没有相关原图时正常用文字回答。用户要求纯文本或严格输出格式时遵循用户要求。
`

// Build the catalog solely from final ranked passages, after normal authorization
// and filtering. No document lookup, expanded scope, or historical source is used.
func buildCitationCatalog(registry *modelcontext.Registry, results []*types.SearchResult) string {
	var b strings.Builder
	seen := make(map[string]bool)
	imageCount := 0
	seenImages := make(map[string]bool)
	for _, result := range results {
		if result == nil || strings.TrimSpace(result.Content) == "" {
			continue
		}
		handle := registry.ChunkHandle(result.ID)
		if isPipelineWebReference(result) {
			handle = registry.RegisterWeb(result.ID, firstPipelineTitle(result))
		}
		if handle == "" || seen[handle] {
			continue
		}
		template := registry.CitationTemplate(handle)
		if template == "" {
			continue
		}
		seen[handle] = true
		fmt.Fprintf(&b, "- source=%s document=%q citation=%s\n", handle, firstPipelineTitle(result), template)
		// Repeat the bounded illustrations beside their exact citation template.
		// Long source bodies can otherwise bury image addresses among prose.
		for _, match := range presentationImageRE.FindAllStringSubmatch(result.Content, -1) {
			if imageCount >= 2 || seenImages[match[2]] {
				continue
			}
			caption := strings.TrimSpace(match[1])
			if strings.Contains(caption, "封面") || strings.Contains(caption, "水印") || strings.Contains(strings.ToLower(caption), "logo") {
				continue
			}
			seenImages[match[2]] = true
			imageCount++
			fmt.Fprintf(&b, "  available_illustration=%s source_citation=%s\n", match[0], template)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "<citation_catalog>\n" + b.String() + "</citation_catalog>"
}

var presentationImageRE = regexp.MustCompile(`!\[([^\]\r\n]*)\]\((resource://[A-Za-z0-9_-]{22})\)`)

// Keep a small presentation reminder next to the current question, after long
// retrieved bodies. This is sourced only from this call's citable passages.
func buildIllustrationReminder(registry *modelcontext.Registry, results []*types.SearchResult, query string) string {
	candidates := append([]*types.SearchResult(nil), results...)
	normalize := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		for _, ext := range []string{".pdf", ".docx", ".doc", ".md", ".txt"} {
			s = strings.TrimSuffix(s, ext)
		}
		return strings.Join(strings.Fields(s), "")
	}
	for i, r := range candidates {
		if r != nil && normalize(firstPipelineTitle(r)) == normalize(query) && presentationImageRE.MatchString(r.Content) {
			candidates[0], candidates[i] = candidates[i], candidates[0]
			break
		}
	}
	for _, r := range candidates {
		if r == nil {
			continue
		}
		template := registry.CitationTemplate(registry.ChunkHandle(r.ID))
		if template == "" {
			continue
		}
		for _, m := range presentationImageRE.FindAllStringSubmatch(r.Content, -1) {
			caption := strings.ToLower(m[1])
			if strings.Contains(caption, "封面") || strings.Contains(caption, "水印") || strings.Contains(caption, "logo") {
				continue
			}
			return "\n\n本轮可用原图（来源：" + firstPipelineTitle(r) + "）：\n" + m[0] + " " + template +
				"\n这张图来自本轮已检索正文。若直接支持当前主题，请在对应段落展示上面的原图及来源，不要只用文字说‘如下图’。不要猜测图中未提供的细节；若图片不相关或用户要求纯文本/固定格式，则省略。"
		}
	}
	return ""
}
