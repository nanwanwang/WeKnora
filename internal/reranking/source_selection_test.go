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

func TestExactTopicKeepsBodyAndIllustrationInsteadOfContents(t *testing.T) {
	rows := []*types.SearchResult{
		{ID: "toc", KnowledgeID: "topic", KnowledgeBaseID: "kb", KnowledgeTitle: "EMC设计.pdf", Score: 0.95, Content: "目录\n概述....1\n传导....2\n屏蔽....3"},
		{ID: "body", KnowledgeID: "topic", KnowledgeBaseID: "kb", KnowledgeTitle: "EMC设计.pdf", Score: 0.9, Content: "EMC design principles"},
		{ID: "diagram", KnowledgeID: "topic", KnowledgeBaseID: "kb", KnowledgeTitle: "EMC设计.pdf", Score: 0.7, Content: "EMC structure ![diagram](resource://abcdefghijklmnopqrstuv)"},
		{ID: "other", KnowledgeID: "other", KnowledgeBaseID: "kb", KnowledgeTitle: "PCB guide", Score: 0.85, Content: "PCB design"},
		{ID: "web", ChunkType: "web_search", Score: 0.9, Content: "external"},
	}
	indices := SelectBalanced(context.Background(), rows, 4, "EMC设计", true)
	var ids []string
	for _, i := range indices {
		ids = append(ids, rows[i].ID)
	}
	require.ElementsMatch(t, []string{"body", "diagram", "other", "web"}, ids)
	// A single slot still chooses substantive evidence; no new candidate is fetched.
	indices = SelectBalanced(context.Background(), rows, 1, "EMC设计", true)
	require.Equal(t, "body", rows[indices[0]].ID)
}

func TestQuotedTitlePrioritizesAnEligibleMatchingBody(t *testing.T) {
	rows := []*types.SearchResult{
		{ID: "neighbor", KnowledgeTitle: "Y 型模块安装说明.pdf", Content: "安装距离及相邻设备要求", Score: 0.94},
		{ID: "toc", KnowledgeTitle: "X 型模块安装说明.pdf", Content: "目录\n总则....1\n安装要求....4\n表单....6", Score: 0.91},
		{ID: "body", KnowledgeTitle: "X 型模块安装说明.pdf", Content: "安装时设备前方预留 75 mm", Score: 0.88},
	}
	ctx := context.Background()
	q := "根据《X 型模块安装说明》，前方需要留出多少距离？"
	require.Equal(t, []int{2}, SelectBalanced(ctx, rows, 1, q, false))
	require.Equal(t, []int{0}, SelectBalanced(ctx, rows[:2], 1, q, false))
	require.Equal(t, []int{0}, SelectBalanced(ctx, rows, 1, "比较《X 型模块安装说明》和《Y 型模块安装说明》的要求", false))
}
