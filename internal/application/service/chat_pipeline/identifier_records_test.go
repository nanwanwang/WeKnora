package chatpipeline

import (
	"context"
	"fmt"
	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestIdentifierRecordsKeepEveryRowAcrossDocumentsAndTopK(t *testing.T) {
	chat := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "VF-101-P200"}}
	chat.RerankTopK = 1
	for i := 0; i < 23; i++ {
		hit := &types.SearchResult{ID: fmt.Sprint(i), KnowledgeID: fmt.Sprint(i % 2), KnowledgeBaseID: "kb", KnowledgeTitle: "维修.xlsx", ChunkType: "text", MatchType: types.MatchTypeLiteral,
			Content: fmt.Sprintf("| 型号 | 故障 | 措施 |\n| --- | --- | --- |\n| VF-101-P200 | 故障%d | A\\|B |\n| VF-101-P200-REV | 不应出现 | 不应出现 |", i)}
		chat.SearchResult = append(chat.SearchResult, hit)
	}
	chat.RerankResult = chat.SearchResult
	require.Nil(t, (&PluginMerge{}).OnEvent(context.Background(), types.CHUNK_MERGE, chat, func() *PluginError { return nil }))
	require.Nil(t, (&PluginFilterTopK{}).OnEvent(context.Background(), types.FILTER_TOP_K, chat, func() *PluginError { return nil }))
	require.Len(t, chat.MergeResult, 23)
	reg := modelcontext.NewRegistry(true)
	reg.RegisterSearchResults(chat.MergeResult)
	answer := reg.DecodeOutputText(reviewAnswerEvidence(chat, reg).pending)
	require.Contains(t, answer, "23 条记录")
	require.NotContains(t, answer, "不应出现")
	require.Contains(t, answer, "| 序号 | 型号 | 故障 | 措施 | 来源 |")
	require.Contains(t, answer, "A&#124;B |")
	for i := 0; i < 23; i++ {
		require.Contains(t, answer, fmt.Sprintf("故障%d", i))
	}
	require.Equal(t, 23, strings.Count(answer, "chunk_id="))
	chat.Query = "这个型号有多少条维修记录？"
	require.Empty(t, renderIdentifierRecords(chat, reg))
}

func TestIdentifierRecordsParentDuplicatesAndHostileCells(t *testing.T) {
	body := "| 型号 | 故障 |\n| --- | --- |\n| VF-101 | <script>alert(1)</script> |\n| VF-101 | <script>alert(1)</script> |"
	a := &types.SearchResult{ID: "a", ParentChunkID: "p", KnowledgeID: "d", KnowledgeBaseID: "kb", MatchType: types.MatchTypeLiteral, Content: body}
	b := *a
	b.ID = "b"
	chat := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "VF-101"}, PipelineState: types.PipelineState{SearchResult: []*types.SearchResult{a, &b}, MergeResult: []*types.SearchResult{a, &b}}}
	reg := modelcontext.NewRegistry(true)
	reg.RegisterSearchResults(chat.MergeResult)
	answer := renderIdentifierRecords(chat, reg)
	require.Contains(t, answer, "2 条记录")
	require.NotContains(t, answer, "<script>")
	require.Contains(t, answer, "&lt;script&gt;")
}

func TestIdentifierRecordsPreserveVerifiedCitationsAfterParentExpansion(t *testing.T) {
	child := &types.Chunk{ID: "child", ParentChunkID: "parent", KnowledgeID: "doc", KnowledgeBaseID: "kb", Content: "| VF-101 | 更换插座 |", ChunkType: types.ChunkTypeText, IsEnabled: true}
	parent := &types.Chunk{ID: "parent", KnowledgeID: "doc", KnowledgeBaseID: "kb", Content: "| 型号 | 措施 |\n| --- | --- |\n" + child.Content, ChunkType: types.ChunkTypeParentText, IsEnabled: true}
	repo := &expandChunkRepo{chunks: map[string]*types.Chunk{"child": child, "parent": parent}, children: map[string][]*types.Chunk{"parent": {child}}}
	hit := &types.SearchResult{ID: child.ID, ParentChunkID: child.ParentChunkID, KnowledgeID: child.KnowledgeID, KnowledgeBaseID: child.KnowledgeBaseID, Content: child.Content, ChunkType: "text", MatchType: types.MatchTypeLiteral}
	chat := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "VF-101", SummaryConfig: types.SummaryConfig{Prompt: "system"}}, PipelineState: types.PipelineState{SearchResult: []*types.SearchResult{hit}, RerankResult: []*types.SearchResult{hit}, UserContent: "VF-101"}}
	require.Nil(t, (&PluginMerge{chunkRepo: repo}).OnEvent(context.Background(), types.CHUNK_MERGE, chat, func() *PluginError { return nil }))
	_, reg := prepareMessagesWithModelContext(context.Background(), chat)
	answer := reg.DecodeOutputText(reviewAnswerEvidence(chat, reg).pending)
	require.Contains(t, answer, "1 条记录")
	require.Contains(t, answer, "**措施**：更换插座")
	require.Contains(t, answer, `chunk_id="child"`)
}

func TestQualifiedRecordLookupKeepsOnlyMatchingSourceRows(t *testing.T) {
	hit := &types.SearchResult{
		ID: "chunk", KnowledgeID: "document", KnowledgeBaseID: "kb",
		KnowledgeTitle: "维修.xlsx", ChunkType: "text", MatchType: types.MatchTypeLiteral,
		Content: "| 产品型号 | 不良描述 |\n| --- | --- |\n| AC10-T3-7R5G-B | 制动管失效 |\n| AC10-T3-7R5G-B | 无显示 |\n| AC10-T3-7R5G-B2 | 制动管失效 |",
	}
	chat := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: "AC10-T3-7R5G-B 制动管失效",
			SearchTargets: []*types.SearchTarget{{KnowledgeBaseID: "kb"}}},
		PipelineState: types.PipelineState{SearchResult: []*types.SearchResult{hit}, MergeResult: []*types.SearchResult{hit}},
	}
	registry := modelcontext.NewRegistry(true)
	registry.RegisterSearchResults(chat.MergeResult)
	answer := registry.DecodeOutputText(renderIdentifierRecords(chat, registry))
	require.Contains(t, answer, "1 条记录")
	require.Contains(t, answer, "制动管失效")
	require.NotContains(t, answer, "无显示")
	require.NotContains(t, answer, "AC10-T3-7R5G-B2")
	chat.Query = "AC10-T3-7R5G-B 制动开关失效"
	require.Empty(t, renderIdentifierRecords(chat, registry))
}

func TestIdentifierTableForWideSourceKeepsOnlyOriginalRow(t *testing.T) {
	header := "| 日期 | 产品型号 | 工单 | 故障描述 | 原因 | 处理措施 | 维修确认 | 其他1 | 其他2 | 其他3 | 其他4 | 其他5 |"
	line := "| 2026-01-01 | AC10-T3-7R5G-B | WO1 | 制动故障 | 元件失效 | 更换元件 | 完成 | a | b | c | d | e |"
	hit := &types.SearchResult{
		ID: "original", KnowledgeBaseID: "kb", KnowledgeID: "document", KnowledgeTitle: "维修.xlsx",
		MatchType: types.MatchTypeLiteral, ContentRewritten: true,
		Content: header + "\n|" + strings.Repeat(" --- |", 12) + "\n" + line + "\n" +
			strings.Replace(line, "WO1", "WO2", 1),
		CitationSources: []*types.SearchResult{{ID: "original", Content: line}},
	}
	chat := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: "AC10-T3-7R5G-B"},
		PipelineState:   types.PipelineState{SearchResult: []*types.SearchResult{hit}, MergeResult: []*types.SearchResult{hit}},
	}
	registry := modelcontext.NewRegistry(true)
	registry.RegisterSearchResults(chat.MergeResult)
	answer := registry.DecodeOutputText(renderIdentifierRecords(chat, registry))
	require.Contains(t, answer, "1 条记录")
	require.Contains(t, answer, "| 序号 | 日期 | 产品型号 | 工单 | 故障描述 | 原因 | 处理措施 | 维修确认 | 来源 |")
	require.Contains(t, answer, "WO1")
	require.NotContains(t, answer, "WO2")
	require.Contains(t, answer, `chunk_id="original"`)
}
