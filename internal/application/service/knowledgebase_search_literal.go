package service

import (
	"context"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
	"time"
)

// Optional interface keeps third-party repository implementations compatible.
type literalChunkReader interface {
	SearchLiteralChunks(context.Context, []types.KnowledgeSearchScope, types.SearchParams) ([]*types.IndexWithScore, error)
}

func (s *knowledgeBaseService) recallLiteral(ctx context.Context, kbs []*types.KnowledgeBase, params types.SearchParams) ([]*types.IndexWithScore, error) {
	reader, ok := s.chunkRepo.(literalChunkReader)
	if !ok || params.DisableKeywordsMatch || len(params.QueryEmbedding) > 0 || searchutil.LiteralLookup(params.QueryText) == "" {
		return nil, nil
	}
	scopes := []types.KnowledgeSearchScope{}
	for _, kb := range kbs {
		if kb.Type == types.KnowledgeBaseTypeDocument && (kb.IsKeywordEnabled() || kb.IsVectorEnabled()) {
			scopes = append(scopes, types.KnowledgeSearchScope{TenantID: kb.TenantID, KBID: kb.ID})
		}
	}
	timeout := 2 * time.Second
	if params.LiteralAll {
		timeout = 10 * time.Second
	}
	lookupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	hits, err := reader.SearchLiteralChunks(lookupCtx, scopes, params)
	if err != nil {
		if params.LiteralAll {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		logger.Warnf(ctx, "Literal recall unavailable; continuing normal retrieval: %v", err)
		return nil, nil
	}
	logger.Infof(ctx, "Literal recall: %d chunks in %d authorized scopes (bounded evidence, not a full-table count)", len(hits), len(scopes))
	return hits, nil
}
