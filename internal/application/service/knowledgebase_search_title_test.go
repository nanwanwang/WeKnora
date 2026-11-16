package service

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"testing"
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
	calls  int
	scopes []types.KnowledgeSearchScope
	params types.SearchParams
}

func (r *recordingTitleChunkRepo) SearchDocumentTitleChunks(_ context.Context, scopes []types.KnowledgeSearchScope, params types.SearchParams) ([]*types.IndexWithScore, error) {
	r.calls++
	r.scopes = scopes
	r.params = params
	return nil, nil
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
