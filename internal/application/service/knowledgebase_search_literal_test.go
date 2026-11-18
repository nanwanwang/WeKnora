package service

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"testing"
)

type recordingLiteralRepo struct {
	interfaces.ChunkRepository
	calls  int
	scopes []types.KnowledgeSearchScope
	params types.SearchParams
}

func (r *recordingLiteralRepo) SearchLiteralChunks(_ context.Context, scopes []types.KnowledgeSearchScope, params types.SearchParams) ([]*types.IndexWithScore, error) {
	r.calls++
	r.scopes = scopes
	r.params = params
	return []*types.IndexWithScore{{ChunkID: "match", MatchType: types.MatchTypeLiteral}}, nil
}
func TestLiteralRecallAuthorizesBeforeReadingBodies(t *testing.T) {
	r := &recordingLiteralRepo{}
	s := &knowledgeBaseService{repo: &titleAuthKBRepo{}, chunkRepo: r, kbShareService: &fakeKBShareForAuth{}}
	_, err := s.HybridSearch(ctxWithTenantForAuth(7), "foreign", types.SearchParams{QueryText: "VF-101-P200-0380-A-T4-E20-N-H-D", MatchCount: 50})
	require.Error(t, err)
	require.Zero(t, r.calls)
}
func TestLiteralRecallPassesDocumentTagAndTenantIntersections(t *testing.T) {
	r := &recordingLiteralRepo{}
	s := &knowledgeBaseService{chunkRepo: r}
	kb := &types.KnowledgeBase{ID: "allowed", TenantID: 7, Type: types.KnowledgeBaseTypeDocument}
	kb.EnsureDefaults()
	p := types.SearchParams{QueryText: "VF-101-P200-0380-A-T4-E20-N-H-D", KnowledgeIDs: []string{"selected"}, TagIDs: []string{"tag"}, MatchCount: 50}
	hits, err := s.recallLiteral(context.Background(), []*types.KnowledgeBase{kb}, p)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, []types.KnowledgeSearchScope{{TenantID: 7, KBID: "allowed"}}, r.scopes)
	require.Equal(t, p, r.params)
	p.DisableKeywordsMatch = true
	hits, err = s.recallLiteral(context.Background(), []*types.KnowledgeBase{kb}, p)
	require.NoError(t, err)
	require.Empty(t, hits)
	require.Equal(t, 1, r.calls)
}
