package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

func TestMergeDocumentTitleRecallReservesCandidatesWithoutReplacingBetterHits(t *testing.T) {
	a := &types.IndexWithScore{ChunkID: "a", KnowledgeID: "body", Score: 0.95}
	b := &types.IndexWithScore{ChunkID: "b", KnowledgeID: "exact", Score: 0.9, VectorScore: 0.8}
	titleB := &types.IndexWithScore{ChunkID: "b", KnowledgeID: "exact", Score: 0.7}
	c := &types.IndexWithScore{ChunkID: "c", KnowledgeID: "other", Score: 0.5}
	got := mergeDocumentTitleRecall([]*types.IndexWithScore{a, b}, []*types.IndexWithScore{titleB, c}, 10)
	require.Equal(t, []*types.IndexWithScore{b, c, a}, got)
	require.Equal(t, 0.8, got[0].VectorScore)
	require.Equal(t, []*types.IndexWithScore{a, b}, mergeDocumentTitleRecall([]*types.IndexWithScore{a, b}, nil, 10))
}

type titleAuthKBRepo struct {
	interfaces.KnowledgeBaseRepository
}

func (*titleAuthKBRepo) GetKnowledgeBaseByIDs(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return []*types.KnowledgeBase{{ID: "foreign", TenantID: 99, Type: types.KnowledgeBaseTypeDocument}}, nil
}

type recordingTitleChunkRepo struct {
	interfaces.ChunkRepository
	calls    int
	scopes   []types.KnowledgeSearchScope
	params   types.SearchParams
	requests []types.SearchParams
	hits     map[string][]*types.IndexWithScore
}

func (r *recordingTitleChunkRepo) SearchDocumentTitleChunks(_ context.Context, scopes []types.KnowledgeSearchScope, params types.SearchParams) ([]*types.IndexWithScore, error) {
	r.calls++
	r.scopes = scopes
	r.params = params
	r.requests = append(r.requests, params)
	return r.hits[params.QueryText], nil
}
func TestTitleRecallCannotRunBeforeKnowledgeBaseAuthorization(t *testing.T) {
	chunks := &recordingTitleChunkRepo{}
	s := &knowledgeBaseService{repo: &titleAuthKBRepo{}, chunkRepo: chunks, kbShareService: &fakeKBShareForAuth{}}
	_, err := s.HybridSearch(ctxWithTenantForAuth(7), "foreign", types.SearchParams{QueryText: "EMC设计", EnableTitleMatch: true, MatchCount: 50})
	require.Error(t, err)
	require.Zero(t, chunks.calls)
}
func TestTitleRecallPreservesExplicitDocumentAndTagFilters(t *testing.T) {
	chunks := &recordingTitleChunkRepo{}
	s := &knowledgeBaseService{chunkRepo: chunks}
	kb := &types.KnowledgeBase{ID: "allowed", TenantID: 7, Type: types.KnowledgeBaseTypeDocument, IndexingStrategy: types.IndexingStrategy{KeywordEnabled: true}}
	params := types.SearchParams{QueryText: "EMC设计", EnableTitleMatch: true, KnowledgeIDs: []string{"selected"}, TagIDs: []string{"physical"}, ScopeTagIDs: []string{"logical"}}
	s.recallDocumentTitles(context.Background(), []*types.KnowledgeBase{kb}, params)
	require.Equal(t, 1, chunks.calls)
	require.Equal(t, []types.KnowledgeSearchScope{{TenantID: 7, KBID: "allowed"}}, chunks.scopes)
	require.Equal(t, params, chunks.params)
	params.DisableKeywordsMatch = true
	s.recallDocumentTitles(context.Background(), []*types.KnowledgeBase{kb}, params)
	require.Equal(t, 1, chunks.calls)
}

func TestNamedDocumentTitleRecallRetainsScopeAndSkipsAmbiguousTitle(t *testing.T) {
	target := &types.IndexWithScore{
		ChunkID: "purpose", KnowledgeID: "first", KnowledgeBaseID: "allowed",
		MatchType: types.MatchTypeDocumentOverview,
	}
	repo := &recordingTitleChunkRepo{hits: map[string][]*types.IndexWithScore{
		"检验标准": {target},
		"选型规范": {{KnowledgeID: "design-1"}, {KnowledgeID: "design-2"}},
	}}
	s := &knowledgeBaseService{chunkRepo: repo}
	kb := &types.KnowledgeBase{
		ID: "allowed", TenantID: 7, Type: types.KnowledgeBaseTypeDocument,
		IndexingStrategy: types.IndexingStrategy{KeywordEnabled: true},
	}
	params := types.SearchParams{
		QueryText: "改写后没有文档标题的查询", EnableTitleMatch: true,
		NamedOverviewTitles: []string{"检验标准", "选型规范"}, KnowledgeIDs: []string{"first", "design-1", "design-2"},
		TagIDs: []string{"allowed-tag"}, ScopeTagIDs: []string{"scope-tag"},
	}
	got := s.recallDocumentTitles(t.Context(), []*types.KnowledgeBase{kb}, params)
	require.Equal(t, []*types.IndexWithScore{target}, got)
	require.Len(t, repo.requests, 2)
	for i, got := range repo.requests {
		want := params
		want.QueryText = params.NamedOverviewTitles[i]
		require.Equal(t, want, got)
	}
	require.Equal(t, []types.KnowledgeSearchScope{{TenantID: 7, KBID: "allowed"}}, repo.scopes)
}
