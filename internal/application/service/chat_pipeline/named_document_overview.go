package chatpipeline

import (
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
)

// A named overview has an explicit evidence scope. Keep the best passage for
// every exact document before filling the budget with supporting passages.
// If any document could not be resolved, use ordinary ranked retrieval.
func namedOverviewSources(query string, results []*types.SearchResult, limit int) []*types.SearchResult {
	titles := searchutil.RequestedEvidenceTitles(query)
	if len(titles) == 0 || (limit > 0 && limit < len(titles)) {
		return nil
	}
	grouped := make(map[string][]*types.SearchResult, len(titles))
	for _, hit := range results {
		if hit == nil || hit.MatchType != types.MatchTypeDocumentOverview ||
			hit.KnowledgeID == "" || hit.Content == "" {
			continue
		}
		for _, title := range titles {
			if !searchutil.MatchesEvidenceDocument(title, hit.KnowledgeTitle) {
				continue
			}
			group := grouped[title]
			if len(group) > 0 && (group[0].KnowledgeID != hit.KnowledgeID ||
				group[0].KnowledgeBaseID != hit.KnowledgeBaseID) {
				return nil
			}
			grouped[title] = append(group, hit)
			break
		}
	}
	ordered := make([]*types.SearchResult, 0, 3*len(titles))
	for _, title := range titles {
		group := grouped[title]
		if len(group) == 0 {
			return nil
		}
		ordered = append(ordered, group[0])
	}
	for rank := 1; rank < 3; rank++ {
		for _, title := range titles {
			if limit > 0 && len(ordered) == limit {
				return ordered
			}
			if group := grouped[title]; len(group) > rank {
				ordered = append(ordered, group[rank])
			}
		}
	}
	return ordered
}
