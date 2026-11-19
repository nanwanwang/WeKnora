package chatpipeline

import (
	"html"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

const namedOverviewQuestion = "《成品线材检验标准》《线缆及端子描述规范》《热缩套管选择规范》在线材方面分别规定了什么？"

func overviewFixture(id, title, content string, page int) *types.SearchResult {
	return &types.SearchResult{
		ID: id, KnowledgeID: id, KnowledgeBaseID: "standards", KnowledgeTitle: title,
		MatchType: types.MatchTypeDocumentOverview, Content: content,
		SourceLocators: types.SourceLocators{{Type: types.SourceLocatorPDF, Page: page}},
	}
}

func TestN212NamedDocumentOverviewKeepsThreeIndependentCitations(t *testing.T) {
	inspection := overviewFixture("inspection", "成品线材检验标准.pdf", "目的：规定成品线材的检验要求。", 3)
	description := overviewFixture("description", "线缆及端子描述规范.pdf", "目的：规范物料规格描述，避免重复新增。", 2)
	sleeve := overviewFixture("sleeve", "热缩套管选择规范.pdf", "文件编号 STD-148。目的：整理热缩套管的耐压、壁厚、内径与使用温度范围。", 3)
	duplicate := overviewFixture("sleeve-copy", "热缩套管选择规范(1).pdf", "供应商参照附录加工。", 8)
	unrelated := overviewFixture("unrelated", "通用线材说明.pdf", "无关的线材信息。", 2)
	unrelated.MatchType = types.MatchTypeKeywords
	unrelated.Score = 0.99

	turn := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: namedOverviewQuestion},
		PipelineState: types.PipelineState{
			UserContent:  namedOverviewQuestion,
			SearchResult: []*types.SearchResult{unrelated, duplicate, sleeve, inspection, description},
		},
	}
	turn.RerankTopK = 3
	require.Nil(t, (&PluginRerank{}).OnEvent(t.Context(), types.CHUNK_RERANK, turn, func() *PluginError {
		require.Equal(t, []*types.SearchResult{inspection, description, sleeve}, turn.RerankResult)
		return nil
	}))
	turn.MergeResult = turn.RerankResult
	messages, registry := prepareMessagesWithModelContext(t.Context(), turn)
	prompt := html.UnescapeString(messages[len(messages)-1].Content)
	for i, file := range []*types.SearchResult{inspection, description, sleeve} {
		require.Contains(t, prompt, file.KnowledgeTitle)
		require.Contains(t, prompt, file.Content)
		tag := []string{`<ref id="1"/>`, `<ref id="2"/>`, `<ref id="3"/>`}[i]
		require.Contains(t, registry.DecodeOutputText(tag), "chunk_id=\""+file.ID+"\"")
	}
	require.NotContains(t, prompt, duplicate.KnowledgeTitle)
	require.NotContains(t, prompt, unrelated.Content)
	require.Equal(t, 3, sleeve.SourceLocators[0].Page)
}

func TestNamedOverviewSourcesFallsBackForIncompleteOrAmbiguousScope(t *testing.T) {
	a := overviewFixture("a", "成品线材检验标准.pdf", "检验", 3)
	b := overviewFixture("b", "线缆及端子描述规范.pdf", "描述", 2)
	c := overviewFixture("c", "热缩套管选择规范.pdf", "选型", 3)
	secondary := overviewFixture("a", "成品线材检验标准.pdf", "封面", 1)
	secondary.ID = "a-cover"
	ambiguous := overviewFixture("another", c.KnowledgeTitle, "不同版本", 1)
	ambiguous.KnowledgeBaseID = "another-library"
	rows := []*types.SearchResult{c, a, secondary, b}
	require.Equal(t, []*types.SearchResult{a, b, c}, namedOverviewSources(namedOverviewQuestion, rows, 3))
	require.Equal(t, []*types.SearchResult{a, b, c, secondary}, namedOverviewSources(namedOverviewQuestion, rows, 4))
	require.Empty(t, namedOverviewSources(namedOverviewQuestion, rows, 2))
	require.Empty(t, namedOverviewSources(namedOverviewQuestion, rows[1:], 10))
	require.Empty(t, namedOverviewSources(namedOverviewQuestion, append(rows, ambiguous), 10))
	require.Empty(t, namedOverviewSources("《成品线材检验标准》《线缆及端子描述规范》《甲》分别规定什么？", rows, 10))
	require.Empty(t, namedOverviewSources("《成品线材检验标准》《线缆及端子描述规范》温度要求相同吗？", rows, 10))
}

func TestNamedOverviewSourcesBudgetsSupportingPassagesAcrossDocuments(t *testing.T) {
	const query = "《材料标准》《质量标准》对供应商选材和质量确认分别规定了什么？"
	a := overviewFixture("a", "材料标准.pdf", "材料标准目的", 3)
	a2 := overviewFixture("a", a.KnowledgeTitle, "材料选择", 4)
	a2.ID = "a-selection"
	a3 := overviewFixture("a", a.KnowledgeTitle, "抽样检验", 5)
	a3.ID = "a-inspection"
	b := overviewFixture("b", "质量标准.pdf", "质量标准目的", 3)
	b2 := overviewFixture("b", b.KnowledgeTitle, "不同场景选材", 4)
	b2.ID = "b-selection"
	b3 := overviewFixture("b", b.KnowledgeTitle, "首样检验", 5)
	b3.ID = "b-inspection"
	rows := []*types.SearchResult{a, a2, a3, b, b2, b3}
	want := []*types.SearchResult{a, b, a2, b2, a3, b3}
	require.Equal(t, want, namedOverviewSources(query, rows, 6))
	require.Equal(t, want[:4], namedOverviewSources(query, rows, 4))
}

func TestN291WorkflowDiagramKeepsAllFourNodesAndPageCitation(t *testing.T) {
	const question = "《样品承认流程规范》流程图中从供应商交付到归档有哪几个节点？"
	nodes := []string{"2. 供应商提交承认书和样品", "3. 结构核实、签样", "4. 接受承认书和样品", "5. 归档"}
	diagram := &types.SearchResult{
		ID: "workflow-diagram", KnowledgeID: "workflow", KnowledgeBaseID: "standards",
		KnowledgeTitle: "样品承认流程规范.pdf", ChunkType: types.ChunkTypeImageOCR,
		Content:        strings.Join(append([]string{"样品承认流程图", "1. 发起送样需求"}, nodes...), "\n"),
		SourceLocators: types.SourceLocators{{Type: types.SourceLocatorPDF, Page: 4}},
	}
	explanation := &types.SearchResult{
		ID: "workflow-explanation", KnowledgeID: "workflow", KnowledgeBaseID: "standards",
		KnowledgeTitle: diagram.KnowledgeTitle, Content: "流程说明：核实、接受和归档的责任人。",
		SourceLocators: types.SourceLocators{{Type: types.SourceLocatorPDF, Page: 5}},
	}
	turn := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: question},
		PipelineState: types.PipelineState{
			UserContent: question,
			MergeResult: []*types.SearchResult{diagram, explanation},
		},
	}
	messages, registry := prepareMessagesWithModelContext(t.Context(), turn)
	prompt := html.UnescapeString(messages[len(messages)-1].Content)
	last := -1
	for _, node := range nodes {
		position := strings.Index(prompt, node)
		require.Greater(t, position, last, node)
		last = position
	}
	tag := registry.DecodeOutputText(`<ref id="1"/>`)
	require.Contains(t, tag, "chunk_id=\"workflow-diagram\"")
	require.NotContains(t, tag, "workflow-explanation")
	require.Equal(t, 4, diagram.SourceLocators[0].Page)
}
