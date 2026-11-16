package reranking

import (
	"context"
	"fmt"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBalancedSourcesReserveKnowledgeAndDiverseDocuments(t *testing.T) {
	var rows []*types.SearchResult
	for i := 0; i < 20; i++ {
		rows = append(rows, &types.SearchResult{ID: fmt.Sprintf("dominant-%d", i), KnowledgeID: "dominant", KnowledgeBaseID: "kb", Content: fmt.Sprintf("EMC design technical item %d", i), Score: 0.99 - float64(i)*0.001})
	}
	for i := 0; i < 8; i++ {
		rows = append(rows, &types.SearchResult{ID: fmt.Sprintf("other-%d", i), KnowledgeID: fmt.Sprintf("other-%d", i), KnowledgeBaseID: "kb", Content: "EMC design technical guide", Score: 0.9 - float64(i)*0.01})
	}
	rows = append(rows, &types.SearchResult{ID: "exact", KnowledgeID: "exact-doc", KnowledgeBaseID: "kb", KnowledgeTitle: "EMC设计.pdf", Content: "EMC design grounded passage", Score: 0.83})
	for i := 0; i < 8; i++ {
		rows = append(rows, &types.SearchResult{ID: fmt.Sprintf("web-%d", i), KnowledgeID: fmt.Sprintf("url-%d", i), ChunkType: "web_search", Content: "web overview", Score: 1})
	}
	picks := SelectBalanced(context.Background(), rows, 10, "emc 设计", true)
	require.Len(t, picks, 10)
	require.Equal(t, "exact", rows[picks[0]].ID)
	docs := map[string]bool{}
	web := 0
	for _, i := range picks {
		if rows[i].ChunkType == "web_search" {
			web++
		} else {
			docs[rows[i].KnowledgeID] = true
		}
	}
	require.Equal(t, 2, web)
	require.Len(t, docs, 8)
	for _, k := range []int{1, 2, 3, 5, 10, 100} {
		p := SelectBalanced(context.Background(), rows, k, "EMC设计", true)
		require.Len(t, p, min(k, len(rows)))
		seen := map[int]bool{}
		for _, i := range p {
			require.False(t, seen[i])
			seen[i] = true
		}
	}
	require.NotEqual(t, "web_search", rows[SelectBalanced(context.Background(), rows, 1, "EMC设计", true)[0]].ChunkType)
}

func TestBalancedSourcesBackfillAndKeepExplicitDocumentPassages(t *testing.T) {
	rows := []*types.SearchResult{{ID: "a", KnowledgeID: "one", Score: 0.9, Content: "layout"}, {ID: "b", KnowledgeID: "one", Score: 0.89, Content: "filter"}, {ID: "c", KnowledgeID: "one", Score: 0.88, Content: "ground"}}
	require.Len(t, SelectBalanced(context.Background(), rows, 10, "EMC设计", false), 3)
	rows = append(rows, &types.SearchResult{ID: "w", ChunkType: "web_search", Content: "web", Score: 1})
	require.Len(t, SelectBalanced(context.Background(), rows, 10, "EMC设计", false), 4)
	require.Len(t, SelectBalanced(context.Background(), rows[3:], 10, "EMC设计", true), 1)
	require.Empty(t, SelectBalanced(context.Background(), rows, 0, "EMC设计", true))
	require.Empty(t, SelectBalanced(context.Background(), nil, 10, "EMC设计", true))
}
