package chatpipeline

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEvidencePresentationUsesOnlyFinalScopedPassages(t *testing.T) {
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: "EMC", SummaryConfig: types.SummaryConfig{Prompt: "system"}},
		PipelineState: types.PipelineState{
			UserContent:  "EMC",
			SearchResult: []*types.SearchResult{{ID: "dropped-chunk", KnowledgeBaseID: "outside-kb", Content: "outside evidence ![](resource://outside-image)"}},
			MergeResult:  []*types.SearchResult{{ID: "allowed-chunk", KnowledgeID: "allowed-doc", KnowledgeBaseID: "allowed-kb", KnowledgeTitle: "EMC.pdf", Content: "EMC structure ![diagram](resource://abcdefghijklmnopqrstuv)"}},
		},
	}
	messages, registry := prepareMessagesWithModelContext(context.Background(), manage)
	messages = registry.EncodeMessages(messages)
	require.Contains(t, messages[1].Content, "<citation_catalog>")
	require.Contains(t, messages[1].Content, `source=chunk://1 document="EMC.pdf" citation=<ref id="1"/>`)
	require.Contains(t, messages[1].Content, "available_illustration=![diagram](res://0001) source_citation=<ref id=\"1\"/>")
	require.NotContains(t, messages[1].Content, "outside")
	require.Contains(t, messages[1].Content, "本轮可用原图（来源：EMC.pdf）")
	decoded := registry.DecodeOutputText(`![diagram](res://0001) <ref id="1"/>`)
	require.Contains(t, decoded, "![diagram](resource://abcdefghijklmnopqrstuv)")
	require.Contains(t, decoded, `chunk_id="allowed-chunk" kb_id="allowed-kb"`)
	require.Empty(t, registry.DecodeOutputText(`<ref id="2"/>`))
	disabled := false
	manage.CitationEnabled = &disabled
	messages, registry = prepareMessagesWithModelContext(context.Background(), manage)
	require.NotContains(t, messages[1].Content, "<citation_catalog>")
	require.Empty(t, registry.DecodeOutputText(`<ref id="1"/>`))
}

func TestPolicyCheckDoesNotOfferDecorativeImages(t *testing.T) {
	for _, query := range []string{"目前实验室管理制度中是否包含了佩戴工牌", "实验室有没有工牌要求", "EMC设计，纯文本"} {
		t.Run(query, func(t *testing.T) {
			content := "员工上班应佩戴工牌。 ![](resource://abcdefghijklmnopqrstuv)"
			result := &types.SearchResult{ID: "allowed-chunk", KnowledgeID: "allowed-doc", KnowledgeBaseID: "allowed-kb", KnowledgeTitle: "员工手册.pdf", Content: content}
			manage := &types.ChatManage{
				PipelineRequest: types.PipelineRequest{Query: query, SummaryConfig: types.SummaryConfig{Prompt: "system"}},
				PipelineState:   types.PipelineState{UserContent: query, MergeResult: []*types.SearchResult{result}},
			}
			messages, registry := prepareMessagesWithModelContext(context.Background(), manage)
			messages = registry.EncodeMessages(messages)
			require.Contains(t, messages[1].Content, "员工上班应佩戴工牌")
			require.Contains(t, messages[1].Content, "citation=")
			require.NotContains(t, messages[1].Content, "available_illustration")
			require.NotContains(t, messages[1].Content, "本轮可用原图")
			require.NotContains(t, messages[1].Content, "![](")
			require.Equal(t, content, result.Content)
			require.Contains(t, registry.DecodeOutputText(`<ref id="1"/>`), `kb_id="allowed-kb"`)
		})
	}
}

func TestIllustrationPromotionRequiresMatchingTopic(t *testing.T) {
	result := &types.SearchResult{KnowledgeTitle: "员工手册.pdf"}
	require.False(t, offerIllustrations("EMC设计", result))
	result.KnowledgeTitle = "EMC设计.pdf"
	require.True(t, offerIllustrations("EMC设计", result))
	require.False(t, offerIllustrations("", result))
	require.False(t, offerIllustrations("EMC设计", nil))
}

func TestExplicitPictureRequestKeepsSourceImage(t *testing.T) {
	content := "![制度原图](resource://abcdefghijklmnopqrstuv)"
	require.Equal(t, content, presentationPassage("请展示制度原图", content))
	require.NotContains(t, presentationPassage("不要图片，纯文本说明制度", content), "resource://")
}

func TestTableHeadersReachTheModelUnderTheOriginalCitation(t *testing.T) {
	query := "塑壳产品浅划痕的长度和数量上限是什么？"
	row := "<tr><td>A级面</td><td>L≤10mm，n≤2pcs，DS≥100mm</td></tr>"
	result := &types.SearchResult{
		ID: "table-row", KnowledgeID: "inspection", KnowledgeBaseID: "allowed-kb",
		KnowledgeTitle: "塑壳产品检验规范.pdf", Content: row,
		ContextHeader: "原表列标题（从左到右）：\n外观等级 | 允收标准",
	}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: query},
		PipelineState:   types.PipelineState{UserContent: query, MergeResult: []*types.SearchResult{result}},
	}
	messages, registry := prepareMessagesWithModelContext(context.Background(), manage)
	require.Contains(t, messages[1].Content, "外观等级 | 允收标准")
	require.Contains(t, messages[1].Content, "DS≥100mm")
	require.Equal(t, row, result.Content)
	require.Contains(t, registry.DecodeOutputText("<ref id=\"1\"/>"), "chunk_id=\"table-row\"")
	require.Empty(t, registry.DecodeOutputText("<ref id=\"2\"/>"))
}

func TestParallelTableQuestionScopesModelEvidence(t *testing.T) {
	table := "机械 定时\n长度 编码 时长 编码\n12mm L12 4s T4\n16mm L16 6s T6"
	entries := &types.SearchResult{
		ID: "matrix", KnowledgeID: "label-guide", KnowledgeBaseID: "manuals",
		ChunkType: string(types.ChunkTypeText), KnowledgeTitle: "Label guide", Content: table,
	}
	definition := &types.SearchResult{
		ID: "guide-intro", KnowledgeID: "label-guide", KnowledgeBaseID: "manuals",
		ChunkType: string(types.ChunkTypeText), Content: "The columns list label codes.",
	}
	other := &types.SearchResult{
		ID: "other", KnowledgeID: "other-guide", KnowledgeBaseID: "manuals",
		ChunkType: string(types.ChunkTypeText), Content: "A different procedure happens to mention 12mm and 4s.",
	}
	competing := *other
	competing.Content = table
	for _, tc := range []struct {
		name, query string
		extra       *types.SearchResult
		wantOther   bool
	}{
		{"one table", "编码表把12 mm和4 s放在同一行，表示等价吗？", other, false},
		{"relationships in table", "编码表里的12 mm和4 s可以互换吗？", other, false},
		{"different rows", "编码表把12 mm和6 s放在同一行吗？", other, true},
		{"explicit comparison", "编码表把12 mm和4 s放在同一行，请与其他标准对照。", other, true},
		{"ambiguous table", "编码表把12 mm和4 s放在同一行，表示等价吗？", &competing, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manage := &types.ChatManage{
				PipelineRequest: types.PipelineRequest{Query: tc.query},
				PipelineState: types.PipelineState{
					UserContent: tc.query, MergeResult: []*types.SearchResult{tc.extra, entries, definition},
				},
			}
			messages, registry := prepareMessagesWithModelContext(context.Background(), manage)
			require.NotEmpty(t, registry.ChunkHandle(entries.ID))
			require.NotEmpty(t, registry.ChunkHandle(definition.ID))
			require.Equal(t, tc.wantOther, registry.ChunkHandle(other.ID) != "")
			require.Contains(t, messages[1].Content, `机械 \| 长度 = 12mm \| 编码 = L12 `+registry.CitationTemplate(registry.ChunkHandle(entries.ID)))
			require.Len(t, manage.MergeResult, 3)
		})
	}
}
