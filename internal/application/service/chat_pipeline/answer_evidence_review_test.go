package chatpipeline

import (
	"html"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

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
