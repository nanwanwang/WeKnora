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
- 检验、验收和选型按当前对象、等级、版本及原表表头核对。查到的同一条规则包含共同生效的条件时，一起给出这些条件，即使问题只问其中一项。例如划痕的长度、数量和间距共同决定是否允收。缺少当前产品的适用条款时，说明本轮证据不足，并标注其他产品规范的适用对象。
- 按表头分组读取多级表格，每组内将规格与本组的编码配对。若只看到两个独立列组，同一行表示排版位置 (例如美标线径→美标编码、国标线径→国标编码)。判断跨组换算、等效、同一档位或替代关系时，需要原文明确给出该关系。问题问到跨组关系而原文只给出组内编码时，分别列出两组编码，说明该表无法推出跨组关系。
- 按原文分别核对各单位的比较符和边界。遇到“≤某值mm²/某值AWG”这类并列表述时，保留原文写法，明确可判定的截面积规则。仅凭该写法无法把 mm² 的不等号、等号或等价关系套用于 AWG；若没有独立的 AWG 判定规则，标明 AWG 边界待确认。
- 图示、表格或正文的数值、区间或比较符有冲突时，逐项列出原文和出处，并标明边界或限值待确认。用户要求对照图示和表格时，分别核对二者的证据；没有提取到图中文字时，说明目前只核对了已取得的文本，图示边界仍待核对。
- 制度或条款核查要区分“本轮片段未检索到”和“完整制度不存在”。未完整核查相关制度时不得断言所有制度均无此条款；先简洁回答证据范围，再说明实际检索到的要求。公司通用规定与实验室专属条款应分开说明，不能据后者未提及而推断员工在实验室无需遵守通用规定。来访标识、门禁卡不等同于员工工牌，除非用户需要辨析，不必展开无关规定。
- 核查多份制度时，逐份说明本轮命中的相关文件及其证据；即使结论相同也应注明各份文件。启用引用时分别使用各文件自己的引用，结论相同可以合写并列出各自引用。
- 不要在正文输出 weknora://doc/ 等内部地址；用文档标题和本轮 citation 模板标明来源。
- 来源引用开启时，在关键结论后复制本轮 citation_catalog 提供的 citation 原文；每条只支持对应 source 片段。不要自行编写编号、引用标签或只在末尾笼统列出来源。可在句中自然提及实际采用的文档标题。
- 当前资料包含与解释直接相关的原理图、结构图或流程图时，择优展示 1–2 张（citation_catalog 的 available_illustration 给出本轮可用的原图及其 source_citation，可直接复制；不要只描述“如下图”却遗漏图片），在相关说明旁用 Markdown 图片语法复制原图地址，并注明该图的来源。优先主题文档中的解释性示意图；不要为配图加入无关图片、封面、标志或水印。
- 只能展示本轮提供的图片地址，不得猜测或修改地址，也不能将历史图片当成本轮证据。没有相关原图时正常用文字回答。用户要求纯文本或严格输出格式时遵循用户要求。
`

// Build the catalog solely from final ranked passages, after normal authorization
// and filtering. No document lookup, expanded scope, or historical source is used.
func buildCitationCatalog(registry *modelcontext.Registry, results []*types.SearchResult, query string) string {
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
		if !offerIllustrations(query, result) {
			continue
		}
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
	for _, r := range results {
		if !offerIllustrations(query, r) {
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

// Clause checks need textual evidence. Keep the original retrieved passages and
// permissions intact; omit embedded resource images only from the model view.
func textEvidenceQuery(query string) bool {
	q := strings.ToLower(query)
	for _, marker := range []string{"纯文本", "不要图片", "不需要图片", "text only"} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	for _, marker := range []string{"展示图片", "显示图片", "查看图片", "原图", "截图", "配图", "show image"} {
		if strings.Contains(q, marker) {
			return false
		}
	}
	for _, marker := range []string{"是否", "有没有", "有无", "包含", "规定", "条款", "制度"} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	return false
}

func presentationPassage(query, content string) string {
	if !textEvidenceQuery(query) {
		return content
	}
	return presentationImageRE.ReplaceAllString(content, "$1")
}

func normalizePresentationTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, ext := range []string{".pdf", ".docx", ".doc", ".md", ".txt"} {
		s = strings.TrimSuffix(s, ext)
	}
	return strings.Join(strings.Fields(s), "")
}

// Never fall back to the first image from another document. An exact topic
// match is a candidate, not proof of relevance; the prompt still requires a
// directly explanatory figure and rejects decorative artwork.
func offerIllustrations(query string, result *types.SearchResult) bool {
	return result != nil && !textEvidenceQuery(query) &&
		normalizePresentationTitle(query) != "" &&
		normalizePresentationTitle(query) == normalizePresentationTitle(firstPipelineTitle(result))
}
