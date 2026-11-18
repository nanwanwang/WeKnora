package chatpipeline

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLiteralLookupSkipsRerankAndFirstFileAnalysis(t *testing.T) {
	query := "VF-101-P200-0380-A-T4-E20-N-H-D"
	a := &types.SearchResult{ID: "a", KnowledgeID: "file-a", Content: "| 型号 | 故障 |\n| " + query + " | 上电异常 |", KnowledgeFilename: "a.xlsx"}
	b := &types.SearchResult{ID: "b", KnowledgeID: "file-b", Content: "维修手册型号 " + query, KnowledgeFilename: "manual.pdf"}
	other := &types.SearchResult{ID: "other", Content: query + "-REV"}
	m := &types.ChatManage{}
	m.Query = query
	m.RewriteQuery = query
	m.KnowledgeBaseIDs = []string{"kb"}
	m.SearchResult = []*types.SearchResult{a, other, b}
	m.RerankModelID = "must-not-be-called"
	nextCalls := 0
	next := func() *PluginError { nextCalls++; return nil }
	require.Nil(t, (&PluginRerank{}).OnEvent(context.Background(), types.CHUNK_RERANK, m, next))
	require.Equal(t, []*types.SearchResult{a, b}, m.RerankResult)
	m.MergeResult = m.RerankResult
	// All services deliberately nil: this path must not read Excel or call an LLM.
	require.Nil(t, (&PluginDataAnalysis{}).OnEvent(context.Background(), types.DATA_ANALYSIS, m, next))
	require.Equal(t, []*types.SearchResult{a, b}, m.MergeResult)
	require.Equal(t, 2, nextCalls)
}
