package chatpipeline

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTopicRerankDoesNotKeepIrrelevantExactTitle(t *testing.T) {
	cm := rerankChatManage(0.3)
	cm.RewriteQuery = "EMC设计"
	cm.SearchResult = []*types.SearchResult{{ID: "unrelated", KnowledgeID: "exact", KnowledgeTitle: "EMC设计.pdf", Content: "office lunch menu", Score: 0.7}, {ID: "relevant", KnowledgeID: "guide", KnowledgeTitle: "EMC指南.pdf", Content: "EMC filtering and shielding", Score: 0.6}}
	plugin := &PluginRerank{modelService: &rerankOnlyModelService{reranker: &fixedReranker{scores: []float64{0.01, 0.9}}}}
	require.Nil(t, plugin.OnEvent(context.Background(), types.CHUNK_RERANK, cm, func() *PluginError { return nil }))
	require.Len(t, cm.RerankResult, 1)
	require.Equal(t, "relevant", cm.RerankResult[0].ID)
}

func TestTopicDiversityRespectsExplicitScopeAndSourceCuts(t *testing.T) {
	cm := rerankChatManage(0.3)
	cm.RewriteQuery = "EMC设计"
	require.True(t, diversifyTopicDocuments(cm))
	cm.SearchTargets = types.SearchTargets{{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb", KnowledgeIDs: []string{"selected"}}}
	require.False(t, diversifyTopicDocuments(cm))
	cm.SearchResult = []*types.SearchResult{{ID: "a", KnowledgeID: "selected", Score: 0.8, Content: "design"}, {ID: "b", KnowledgeID: "selected", Score: 0.7, Content: "layout"}, {ID: "w1", ChunkType: "web_search", Score: 1, Content: "web"}, {ID: "w2", ChunkType: "web_search", Score: 1, Content: "web duplicate"}, {ID: "w3", ChunkType: "web_search", Score: 1, Content: "web third"}}
	cm.RerankTopK = 3
	got := (&PluginMerge{}).selectInputResults(context.Background(), cm)
	require.Len(t, got, 3)
	internal := 0
	for _, r := range got {
		if r.ChunkType != "web_search" {
			internal++
		}
	}
	require.Equal(t, 2, internal)
	cm.MergeResult = cm.SearchResult
	require.Nil(t, (&PluginFilterTopK{}).OnEvent(context.Background(), types.FILTER_TOP_K, cm, func() *PluginError { return nil }))
	internal = 0
	for _, r := range cm.MergeResult {
		if r.ChunkType != "web_search" {
			internal++
		}
	}
	require.Equal(t, 2, internal)
}
