package service

import (
	"context"
	"slices"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
)

// Called only after hybridSearchCandidates authorizes every KB for the caller.
// No tenant-wide document lookup or fallback scope is allowed here.
func (s *knowledgeBaseService) recallDocumentTitles(ctx context.Context, kbs []*types.KnowledgeBase, params types.SearchParams) []*types.IndexWithScore {
	titles := params.NamedOverviewTitles
	if !params.EnableTitleMatch || params.DisableKeywordsMatch || s.chunkRepo == nil ||
		(len(titles) == 0 && !searchutil.IsTopicQuery(params.QueryText)) {
		return nil
	}
	scopes := make([]types.KnowledgeSearchScope, 0, len(kbs))
	for _, kb := range kbs {
		if kb.Type == types.KnowledgeBaseTypeDocument && (kb.IsVectorEnabled() || kb.IsKeywordEnabled()) {
			scopes = append(scopes, types.KnowledgeSearchScope{TenantID: kb.TenantID, KBID: kb.ID})
		}
	}
	if len(scopes) == 0 {
		return nil
	}
	titleCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if len(titles) > 0 {
		results := make([]*types.IndexWithScore, 0, 2*len(titles))
		for _, title := range titles {
			scoped := params
			scoped.QueryText = title
			hits, err := s.chunkRepo.SearchDocumentTitleChunks(titleCtx, scopes, scoped)
			if err != nil {
				logger.Warnf(ctx, "Named document overview recall unavailable: %v", err)
				continue
			}
			// An exact filename can refer to more than one eligible file. In that
			// case let ordinary retrieval handle it instead of choosing by ID.
			if len(hits) == 0 || slices.ContainsFunc(hits[1:], func(hit *types.IndexWithScore) bool {
				return hit.KnowledgeID != hits[0].KnowledgeID || hit.KnowledgeBaseID != hits[0].KnowledgeBaseID
			}) {
				continue
			}
			results = append(results, hits[:min(2, len(hits))]...)
		}
		return results
	}
	results, err := s.chunkRepo.SearchDocumentTitleChunks(titleCtx, scopes, params)
	if err != nil {
		logger.Warnf(ctx, "Document title recall unavailable; keeping body recall: %v", err)
		return nil
	}
	logger.Infof(ctx, "Document title recall: %d chunks within %d authorized KB scopes", len(results), len(scopes))
	return results
}

// Reserve a small share of the candidate pool for titles before the ordinary
// top-k cut. Duplicates retain the stronger original body hit and its metadata.
func mergeDocumentTitleRecall(body, titles []*types.IndexWithScore, limit int) []*types.IndexWithScore {
	if len(titles) == 0 {
		return body
	}
	byID := make(map[string]*types.IndexWithScore, len(body))
	for _, r := range body {
		if r != nil {
			byID[r.ChunkID] = r
		}
	}
	ordered := make([]*types.IndexWithScore, 0, len(titles))
	seenDoc := map[string]bool{}
	for _, r := range titles {
		if !seenDoc[r.KnowledgeID] {
			ordered = append(ordered, r)
			seenDoc[r.KnowledgeID] = true
		}
	}
	ordered = append(ordered, titles...)
	seen := map[string]bool{}
	out := make([]*types.IndexWithScore, 0, len(body)+len(titles))
	add := func(r *types.IndexWithScore) {
		if r != nil && !seen[r.ChunkID] {
			seen[r.ChunkID] = true
			out = append(out, r)
		}
	}
	budget := min(10, max(1, limit/5))
	if ordered[0].MatchType == types.MatchTypeDocumentOverview {
		budget = len(titles)
	}
	for _, r := range ordered {
		if len(out) >= budget {
			break
		}
		if old := byID[r.ChunkID]; old != nil && old.Score > r.Score {
			if r.MatchType == types.MatchTypeDocumentOverview {
				kept := *old
				kept.MatchType = r.MatchType
				r = &kept
			} else {
				r = old
			}
		}
		add(r)
	}
	for _, r := range body {
		add(r)
	}
	for _, r := range titles {
		add(r)
	}
	return out
}
