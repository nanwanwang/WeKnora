package chatpipeline

import (
	"html"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestNamedSummarySuppliesVerifiedCodeOnGenerationFallback(t *testing.T) {
	first := overviewFixture("a", "线材规范.pdf", "规定成品检验方法。", 3)
	first.ChunkType = string(types.ChunkTypeText)
	second := overviewFixture("b", "选型规范.pdf", "文件编号 AB-4923\n选型参数见正文。", 1)
	second.ChunkType = first.ChunkType
	question := "《线材规范》《选型规范》分别规定了什么？"
	request := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: question},
		PipelineState:   types.PipelineState{MergeResult: []*types.SearchResult{first, second}},
	}
	registry := modelcontext.NewRegistry(true)
	registry.RegisterSearchResults(request.MergeResult)
	review := reviewAnswerEvidence(request, registry)
	require.Empty(t, review.pending)
	answer := "《线材规范》规定成品检验方法。《选型规范》规定选型参数。"
	tail := html.UnescapeString(review.supplement(answer))
	require.Contains(t, tail, "《选型规范》 AB-4923")
	require.Contains(t, tail, `chunk_id="b"`)
	require.Empty(t, review.supplement(answer+"文件编号 AB-4923。"))
	require.Empty(t, review.supplement("《线材规范》规定成品检验方法。"))

	second.Content += "\n文件编号 AB-4924"
	require.Empty(t, reviewAnswerEvidence(request, registry).supplement(answer))
}

func TestEvidenceReviewAddsConditionsOnlyForACitedRow(t *testing.T) {
	a := &types.SearchResult{
		ID: "row-a", KnowledgeID: "calibration", KnowledgeBaseID: "engineering",
		KnowledgeTitle: "设备校验指引", ChunkType: string(types.ChunkTypeText),
		Content: `<tr><td>甲级</td><td>Q≤3.5V，T≥12s，P≤70%</td></tr>
<tr><td>乙级</td><td>Q≤4V，T≥18s，P≤80%</td></tr>`,
	}
	request := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: "甲级的电压和时间要求？"},
		PipelineState:   types.PipelineState{MergeResult: []*types.SearchResult{a}},
	}
	registry := modelcontext.NewRegistry(true)
	registry.RegisterSearchResults(request.MergeResult)
	citation := registry.DecodeOutputText(registry.CitationTemplate(registry.ChunkHandle(a.ID)))
	require.NotEmpty(t, citation)
	review := reviewAnswerEvidence(request, registry)
	answer := "甲级 Q **≤ 3.5 V**，时间至少 12 s。 " + citation
	tail := html.UnescapeString(review.supplement(answer))
	require.Contains(t, tail, "P≤70%")
	require.NotContains(t, tail, "80%")
	require.Contains(t, tail, citation)
	require.Empty(t, review.supplement("Q≤3.5 V，T≥12 s。"))
	require.Empty(t, review.supplement("Q≤3.5 V，T≥12 s，P≤70%。 "+citation))

	a.Content += "\n甲级同组另一个工况 Q≤3.5 V，T≥12 s，P≤60%"
	review = reviewAnswerEvidence(request, registry)
	require.Empty(t, review.supplement(answer), "two rows match with different remaining limits")
}

func TestEvidenceReviewDoesNotInheritComparatorAfterSlash(t *testing.T) {
	src := reviewSource{document: "runbook", body: strings.Repeat("与该限位无关的前文。\n", 50) +
		"限位≤22m/75ft 分为甲类，＞22m/75ft\n分为乙类。", ref: "<ref id=\"1\"/>"}
	proof := findEvidenceDifference("正好75 ft，甲类还是乙类？", []reviewSource{src})
	require.Len(t, proof.excerpts, 1)
	answer := html.UnescapeString(renderEvidenceDifference(proof, true))
	require.Contains(t, answer, "22m/75ft")
	require.Contains(t, answer, "待确认")
	require.Contains(t, answer, src.ref)
	require.Empty(t, findEvidenceDifference("甲类是否采用 22m？", []reviewSource{src}).excerpts)
	src.body += "\n英制条件明确为≤75ft归甲类。"
	require.Empty(t, findEvidenceDifference("正好75 ft怎么归类？", []reviewSource{src}).excerpts)
}

func TestEvidenceReviewFindsDifferentInclusivityWithinADocument(t *testing.T) {
	one := reviewSource{document: "manual", body: "流量 <7.3 L 选流程甲\n流量 ≥7.3 L 选流程乙", ref: "<ref id=\"2\"/>"}
	two := reviewSource{document: "manual", body: "当流量 ≤7.3 L 时用甲\n当流量 ≥7.3 L 时用乙", ref: "<ref id=\"3\"/>"}
	proof := findEvidenceDifference("流量等于7.30 L，该用哪个流程？", []reviewSource{one, two})
	require.Len(t, proof.excerpts, 2)
	answer := html.UnescapeString(renderEvidenceDifference(proof, true))
	require.Contains(t, answer, "流量 <7.3 L")
	require.Contains(t, answer, "流量 ≤7.3 L")
	require.Contains(t, answer, "待确认")
	require.Contains(t, answer, two.ref)
	require.Empty(t, findEvidenceDifference("流量等于7.3 L时使用哪个流程？", []reviewSource{one}).excerpts)
	two.document = "another revision"
	require.Empty(t, findEvidenceDifference("流量等于7.3 L时使用哪个流程？", []reviewSource{one, two}).excerpts)
}

func TestEvidenceReviewChecksTableBoundaryAgainstCitedFigureOCR(t *testing.T) {
	table := &types.SearchResult{
		ID: "table", KnowledgeID: "guide", KnowledgeBaseID: "lab", KnowledgeTitle: "测试指南",
		ChunkType: string(types.ChunkTypeText),
		Content:   `<table><tr><td rowspan="2">进液压力 3 bar~9 bar</td><td>流量 &lt;7.3 L</td><td>流程甲</td></tr><tr><td>流量 ≥7.3 L</td><td>流程乙</td></tr></table>`,
	}
	figure := &types.SearchResult{
		ID: "figure", KnowledgeID: table.KnowledgeID, KnowledgeBaseID: table.KnowledgeBaseID,
		KnowledgeTitle: table.KnowledgeTitle, ChunkType: string(types.ChunkTypeImageOCR),
		Content: "进液压力 3 bar~9 bar\n当流量 ≤7.3 L 时进流程甲\n当流量 ≥7.3 L 时进流程乙",
	}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: "进液压力在 3 到 9 bar，流量正好7.30 L时应走哪个流程？"},
		PipelineState:   types.PipelineState{MergeResult: []*types.SearchResult{table, figure}},
	}
	registry := modelcontext.NewRegistry(true)
	registry.RegisterSearchResults([]*types.SearchResult{table})
	require.Empty(t, reviewAnswerEvidence(manage, registry).pending, "ignore a figure without a registered citation")
	registry.RegisterSearchResults(manage.MergeResult)
	answer := html.UnescapeString(reviewAnswerEvidence(manage, registry).pending)
	require.Contains(t, answer, "请核对原图")
	require.Contains(t, answer, "流量 <7.3 L")
	require.Contains(t, answer, "当流量 ≤7.3 L")
	for _, src := range manage.MergeResult {
		require.Contains(t, answer, registry.CitationTemplate(registry.ChunkHandle(src.ID)))
	}
	query := manage.Query
	manage.Query = "进液压力在 1 bar 到 2 bar，流量正好7.30 L时应走哪个流程？"
	require.Empty(t, reviewAnswerEvidence(manage, registry).pending, "unrelated table group")
	manage.Query = "进液压力 1 到 3 bar，排液压力 5 到 9 bar，流量正好7.30 L时应走哪个流程？"
	require.Empty(t, reviewAnswerEvidence(manage, registry).pending, "do not join endpoints across ranges")
	manage.Query = query
	original := figure.Content
	figure.Content = strings.Replace(figure.Content, "3 bar~9 bar", "1 bar~2 bar", 1)
	require.Empty(t, reviewAnswerEvidence(manage, registry).pending, "unrelated figure condition")
	figure.Content = original
	figure.KnowledgeID = "different guide"
	figure.KnowledgeTitle = "另一份指南.pdf"
	require.Empty(t, reviewAnswerEvidence(manage, registry).pending, "do not pair unrelated files")
	figure.KnowledgeTitle = "测试指南(2).pdf"
	answer = html.UnescapeString(reviewAnswerEvidence(manage, registry).pending)
	require.Contains(t, answer, "两份资料")
	require.Contains(t, answer, registry.CitationTemplate(registry.ChunkHandle(table.ID)))
	require.Contains(t, answer, registry.CitationTemplate(registry.ChunkHandle(figure.ID)))
	figure.Content = strings.Split(figure.Content, "\n当流量 ≥")[0]
	require.Empty(t, reviewAnswerEvidence(manage, registry).pending, "a single recognized comparison does not span files")
	figure.Content = original
	figure.KnowledgeBaseID = "different lab"
	require.Empty(t, reviewAnswerEvidence(manage, registry).pending, "do not pair files across libraries")
	figure.KnowledgeBaseID = table.KnowledgeBaseID
	figure.KnowledgeID = table.KnowledgeID
	manage.MergeResult = []*types.SearchResult{figure}
	figure.Content += "\n另一处 OCR 给出流量 <7.3 L 时进流程甲"
	require.Empty(t, reviewAnswerEvidence(manage, registry).pending, "OCR alone is insufficient")
}

func TestEvidenceReviewKeepsNamedLimitsFromTheSameField(t *testing.T) {
	row := reviewSource{document: "manual", body: "压环 44N 以上编号 DK/R9/N。STD12-DK/R9/N 为大号压环，最小载荷为 44N。后附说明 DK/R9/N 为大号压环，最小载荷要求为 29N。", ref: "<ref id=\"4\"/>"}
	production := reviewSource{document: "manual", body: "生产注记约定大号压环，载荷为44N以\n上。", ref: "<ref id=\"5\"/>"}
	proof := findEvidenceDifference("对照 DK/R9/N 的载荷要求，是否有冲突？", []reviewSource{row, production})
	require.GreaterOrEqual(t, len(proof.excerpts), 2)
	answer := html.UnescapeString(renderEvidenceDifference(proof, true))
	require.Contains(t, answer, "44N")
	require.Contains(t, answer, "29N")
	require.Contains(t, answer, "待确认")
	require.Contains(t, answer, production.ref)
	require.Empty(t, findEvidenceDifference("对照另一件 DT/B4/Z 的载荷要求", []reviewSource{row, production}).excerpts)
	row.body = strings.ReplaceAll(row.body, "最小载荷要求为 29N", "最大载荷要求为 29N")
	require.Empty(t, findEvidenceDifference("对照 DK/R9/N 的载荷要求", []reviewSource{row, production}).excerpts)
}

func TestEvidenceReviewUsesTheRegisteredCurrentSource(t *testing.T) {
	other := &types.SearchResult{
		ID: "source", KnowledgeID: "within", KnowledgeBaseID: "allowed", ChunkType: string(types.ChunkTypeText),
		KnowledgeTitle: "流程分类指引", Content: "≤22m/75ft 分入甲类，＞22m/75ft分入乙类。",
	}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: "75 ft 归类待核对，按哪项？"},
		PipelineState:   types.PipelineState{MergeResult: []*types.SearchResult{other}},
	}
	registry := modelcontext.NewRegistry(true)
	require.Empty(t, reviewAnswerEvidence(manage, registry).pending)
	registry.RegisterSearchResults(manage.MergeResult)
	require.NotEmpty(t, reviewAnswerEvidence(manage, registry).pending)
	manage.Query += " 严格使用 JSON"
	require.Empty(t, reviewAnswerEvidence(manage, registry).pending)
}
