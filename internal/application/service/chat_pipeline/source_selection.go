package chatpipeline

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/reranking"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
)

func sourcePreferenceQuery(chat *types.ChatManage) string {
	if strings.ContainsAny(chat.Query, "《》") {
		return chat.Query
	}
	return chat.RewriteQuery
}

func diversifyTopicDocuments(chat *types.ChatManage) bool {
	if preservePolicyDocuments(chat) {
		return true
	}
	if !searchutil.IsBroadTopicQuery(chat.RewriteQuery) {
		return false
	}
	for _, target := range chat.SearchTargets {
		if target != nil && target.Type == types.SearchTargetTypeKnowledge {
			return false
		}
	}
	return true
}

// The fallback and final cut use the same bounded source selection as rerank.
// Existing image evidence explicitly kept outside top-k still rides along.
func selectChatSources(ctx context.Context, results []*types.SearchResult, k int, chat *types.ChatManage) []*types.SearchResult {
	if k <= 0 {
		return results
	}
	var ranked, kept []*types.SearchResult
	for _, r := range results {
		if searchutil.IsKeptOutsideTopK(r) {
			kept = append(kept, r)
		} else if r != nil {
			ranked = append(ranked, r)
		}
	}
	selected := reranking.SelectBalanced(ctx, ranked, k, sourcePreferenceQuery(chat), diversifyTopicDocuments(chat))
	out := make([]*types.SearchResult, 0, len(selected)+len(kept))
	for _, i := range selected {
		out = append(out, ranked[i])
	}
	return append(out, kept...)
}
