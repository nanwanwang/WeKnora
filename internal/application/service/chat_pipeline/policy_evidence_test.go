package chatpipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

const labBadgeQuestion = "目前实验室管理制度中是否包含了佩戴工牌"

func TestPolicyReviewPreservesIndependentDocumentCitations(t *testing.T) {
	const visitorClause = "外来人员进入实验室应登记，经负责人批准，佩戴统一标识并由管理人员陪同。"
	const equipmentClause = "仪器设备按操作规程管理，使用完毕后填写仪器记录。"
	const housekeepingClause = "试验期间保持工作区整洁，离开前整理工具并关闭电源。"
	for _, tc := range []struct {
		name          string
		generalClause string
		xianClause    string
	}{
		{"identical clauses", visitorClause, visitorClause},
		{"contained clauses", visitorClause, visitorClause + "离开时归还访客标识。"},
		{"overlap without containment",
			visitorClause + "\n" + equipmentClause + "\n" + housekeepingClause,
			equipmentClause + "\n" + visitorClause + "\n" + housekeepingClause},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			standard := &types.SearchResult{
				ID: "general-clause", KnowledgeID: "general-lab", KnowledgeBaseID: "authorized-kb",
				KnowledgeTitle: "实验室管理规章制度.doc", ChunkType: string(types.ChunkTypeText),
				Score: 0.9, Content: tc.generalClause,
			}
			xian := &types.SearchResult{
				ID: "xian-clause", KnowledgeID: "xian-lab", KnowledgeBaseID: "authorized-kb",
				KnowledgeTitle: "西安实验室管理规章制度.doc", ChunkType: string(types.ChunkTypeText),
				Score: 0.8, Content: tc.xianClause,
			}
			duplicate := *xian
			duplicate.ID = "same-xian-clause"
			duplicate.Score = 0.7

			chat := &types.ChatManage{
				PipelineRequest: types.PipelineRequest{
					Query: labBadgeQuestion, RerankTopK: 10,
					KnowledgeBaseIDs: []string{"authorized-kb"},
					SummaryConfig:    types.SummaryConfig{Prompt: "system"},
				},
				PipelineState: types.PipelineState{
					UserContent: labBadgeQuestion, RewriteQuery: labBadgeQuestion, Intent: types.IntentKBSearch,
					RerankResult: []*types.SearchResult{standard, xian, &duplicate},
					History: []*types.History{{
						KnowledgeReferences: []*types.SearchResult{{
							ID: "outside-history", KnowledgeID: "outside-doc", KnowledgeBaseID: "outside-kb",
							KnowledgeTitle: "先前会话的制度.doc", ChunkType: string(types.ChunkTypeText),
							Score: 1.0, Content: labBadgeQuestion + "。这是以前检索过的文件。",
						}},
					}},
				},
			}
			next := func() *PluginError { return nil }
			require.Nil(t, (&PluginMerge{}).OnEvent(ctx, types.CHUNK_MERGE, chat, next))
			require.Nil(t, (&PluginFilterTopK{}).OnEvent(ctx, types.FILTER_TOP_K, chat, next))
			assertResultIDs(t, chat.MergeResult, "general-clause", "xian-clause")

			messages, registry := prepareMessagesWithModelContext(ctx, chat)
			contextText := messages[len(messages)-1].Content
			require.Contains(t, contextText, "<citation_catalog>")
			require.Contains(t, contextText, labBadgeQuestion)
			require.Empty(t, registry.CitationTemplate(registry.ChunkHandle("outside-history")))
			for _, doc := range []*types.SearchResult{standard, xian} {
				template := registry.CitationTemplate(registry.ChunkHandle(doc.ID))
				require.NotEmpty(t, template)
				require.Contains(t, contextText, `document="`+doc.KnowledgeTitle+`" citation=`+template)
				citation := registry.DecodeOutputText(template)
				require.Contains(t, citation, `doc="`+doc.KnowledgeTitle+`"`)
				require.Contains(t, citation, `chunk_id="`+doc.ID+`"`)
				require.Contains(t, citation, `kb_id="authorized-kb"`)
			}
		})
	}
}

func TestPolicyReviewUsesAvailableSlotsForDistinctDocuments(t *testing.T) {
	const similar = "授权员工应经过登记后进入实验室，访客须佩戴统一标识。"
	ctx := context.Background()
	chat := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: labBadgeQuestion},
		PipelineState:   types.PipelineState{RewriteQuery: labBadgeQuestion},
	}
	results := []*types.SearchResult{
		{ID: "general-1", KnowledgeID: "general", KnowledgeBaseID: "kb", Score: 0.95, Content: similar},
		{ID: "general-2", KnowledgeID: "general", KnowledgeBaseID: "kb", Score: 0.94, Content: strings.Repeat(similar, 2)},
		{ID: "xian", KnowledgeID: "xian", KnowledgeBaseID: "kb", Score: 0.81, Content: similar},
	}
	assertResultIDs(t, selectChatSources(ctx, results, 2, chat), "general-1", "xian")
}
