package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
)

// SearchLiteralChunks uses persisted bodies, so existing documents do not need
// a new vector payload or reparsing. Scopes must be authorized by the service.
func (r *chunkRepository) SearchLiteralChunks(ctx context.Context, scopes []types.KnowledgeSearchScope, params types.SearchParams) ([]*types.IndexWithScore, error) {
	value := searchutil.LiteralLookup(params.QueryText)
	var qualifier string
	if params.LiteralAll {
		if identifier, phrase := searchutil.QualifiedRecordLookup(params.QueryText); phrase != "" {
			value, qualifier = identifier, phrase
		}
	}
	if value == "" || len(scopes) == 0 || params.OnlyRecommended ||
		(len(params.ScopeTagIDs) > 0 && len(params.KnowledgeIDs) == 0 && len(params.TagIDs) == 0) {
		return nil, nil
	}
	pairs, args := []string{}, []interface{}{}
	for _, scope := range scopes {
		if scope.TenantID != 0 && scope.KBID != "" {
			pairs = append(pairs, "(k.tenant_id = ? AND k.knowledge_base_id = ?)")
			args = append(args, scope.TenantID, scope.KBID)
		}
	}
	if len(pairs) == 0 {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Table("chunks AS c").
		Joins("JOIN knowledges AS k ON k.id = c.knowledge_id AND k.tenant_id = c.tenant_id AND k.knowledge_base_id = c.knowledge_base_id").
		Joins("JOIN knowledge_bases AS kb ON kb.id = k.knowledge_base_id AND kb.tenant_id = k.tenant_id").
		Where("("+strings.Join(pairs, " OR ")+")", args...).
		Where("kb.deleted_at IS NULL AND kb.type = ? AND k.deleted_at IS NULL AND k.enable_status = ? AND k.parse_status = ?", types.KnowledgeBaseTypeDocument, "enabled", "completed").
		Where("c.deleted_at IS NULL AND c.is_enabled = ? AND c.index_status = ? AND c.chunk_type = ?", true, "ready", types.ChunkTypeText).
		Where("LOWER(c.content) LIKE ? ESCAPE ?", "%"+escapeLikeKeyword(strings.ToLower(value))+"%", likeEscapeChar)
	if qualifier != "" {
		q = q.Where("LOWER(c.content) LIKE ? ESCAPE ?", "%"+escapeLikeKeyword(strings.ToLower(qualifier))+"%", likeEscapeChar)
	}
	if len(params.KnowledgeIDs) > 0 {
		q = q.Where("k.id IN ?", params.KnowledgeIDs)
	}
	if len(params.TagIDs) > 0 {
		q = q.Where("c.tag_id IN ?", params.TagIDs)
	}
	if params.LiteralAll && searchutil.IsIdentifierLookup(value) {
		rows, err := q.Select("c.id,c.knowledge_id,c.knowledge_base_id,c.content,c.tag_id").Order("k.id,c.chunk_index,c.id").Rows()
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []*types.IndexWithScore
		scanned, bytes := 0, 0
		for rows.Next() {
			var id, kid, kbid, content string
			var tag *string
			if err := rows.Scan(&id, &kid, &kbid, &content, &tag); err != nil {
				return nil, err
			}
			scanned++
			bytes += len(content)
			if scanned > 50000 || bytes > 64<<20 {
				return nil, fmt.Errorf("编号匹配范围过大，请缩小知识库或文件范围后重试；未返回不完整记录")
			}
			if !searchutil.ContainsRecordMatch(content, value, qualifier) {
				continue
			}
			if len(out) >= 5000 {
				return nil, fmt.Errorf("编号匹配超过5000个片段，请缩小文件范围后重试；未返回不完整记录")
			}
			hit := &types.IndexWithScore{ID: id, ChunkID: id, SourceID: id, SourceType: types.ChunkSourceType, KnowledgeID: kid, KnowledgeBaseID: kbid, Content: content, Score: 1, MatchType: types.MatchTypeLiteral, IsEnabled: true}
			if tag != nil {
				hit.TagID = *tag
			}
			out = append(out, hit)
		}
		return out, rows.Err()
	}
	// Bound work even for common cell values. Round-robin documents before the
	// result cut; this is retrieval evidence, never an exhaustive statistics set.
	ranked := q.Select("c.id, c.knowledge_id, c.knowledge_base_id, c.content, c.tag_id, ROW_NUMBER() OVER (PARTITION BY c.knowledge_id ORDER BY c.chunk_index,c.id) AS literal_row")
	var rows []struct{ ID, KnowledgeID, KnowledgeBaseID, Content, TagID string }
	if err := r.db.WithContext(ctx).Table("(?) AS ranked", ranked).Order("literal_row, knowledge_id, id").Limit(500).Scan(&rows).Error; err != nil {
		return nil, err
	}
	limit := min(100, max(1, params.MatchCount))
	out := make([]*types.IndexWithScore, 0, limit)
	for _, row := range rows {
		if !searchutil.ContainsLiteral(row.Content, value) {
			continue
		}
		out = append(out, &types.IndexWithScore{ID: row.ID, ChunkID: row.ID, SourceID: row.ID, SourceType: types.ChunkSourceType, KnowledgeID: row.KnowledgeID, KnowledgeBaseID: row.KnowledgeBaseID, TagID: row.TagID, Content: row.Content, Score: 1, MatchType: types.MatchTypeLiteral, IsEnabled: true})
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
