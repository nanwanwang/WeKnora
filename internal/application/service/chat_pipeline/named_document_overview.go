package chatpipeline

import (
	"slices"

	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
)

// A named overview has an explicit evidence scope. Prefer the purpose and
// identification text for each exact document, keeping the user's title order.
// If any document could not be resolved, use ordinary ranked retrieval.
func namedOverviewSources(query string, results []*types.SearchResult, limit int) []*types.SearchResult {
	titles := searchutil.NamedDocumentOverviewTitles(query)
	if len(titles) == 0 || (limit > 0 && limit < len(titles)) {
		return nil
	}
	grouped := make(map[string][]*types.SearchResult, len(titles))
	for _, hit := range results {
		if hit == nil || hit.MatchType != types.MatchTypeDocumentOverview ||
			hit.KnowledgeID == "" || hit.Content == "" {
			continue
		}
		title := searchutil.NormalizeDocumentTitle(hit.KnowledgeTitle)
		if !slices.Contains(titles, title) {
			continue
		}
		group := grouped[title]
		if len(group) > 0 && (group[0].KnowledgeID != hit.KnowledgeID ||
			group[0].KnowledgeBaseID != hit.KnowledgeBaseID) {
			return nil
		}
		grouped[title] = append(group, hit)
	}
	ordered := make([]*types.SearchResult, 0, 2*len(titles))
	for _, title := range titles {
		group := grouped[title]
		if len(group) == 0 {
			return nil
		}
		ordered = append(ordered, group[0])
	}
	for _, title := range titles {
		if limit > 0 && len(ordered) == limit {
			break
		}
		if group := grouped[title]; len(group) > 1 {
			ordered = append(ordered, group[1])
		}
	}
	return ordered
}
