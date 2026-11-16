package repository

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// titleSQL removes the same common whitespace from stored titles on PostgreSQL
// and SQLite. All search values remain bound parameters, including LIKE wildcards.
func titleSQL(column string) string {
	expression := "LOWER(COALESCE(" + column + ", ''))"
	for _, space := range []string{" ", "\t", "\r", "\n", "　"} {
		expression = "REPLACE(" + expression + ", '" + space + "', '')"
	}
	return expression
}

func (r *chunkRepository) SearchDocumentTitleChunks(ctx context.Context, scopes []types.KnowledgeSearchScope, params types.SearchParams) ([]*types.IndexWithScore, error) {
	if len(scopes) == 0 || !searchutil.IsTopicQuery(params.QueryText) || params.OnlyRecommended ||
		(len(params.ScopeTagIDs) > 0 && len(params.KnowledgeIDs) == 0 && len(params.TagIDs) == 0) {
		return nil, nil
	}
	query := searchutil.NormalizeDocumentTitle(params.QueryText)
	exactNames := []string{query}
	for _, ext := range []string{".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx", ".txt", ".md"} {
		exactNames = append(exactNames, query+ext)
	}
	pairs := make([]string, 0, len(scopes))
	args := make([]interface{}, 0, len(scopes)*2)
	for _, scope := range scopes {
		if scope.KBID == "" || scope.TenantID == 0 {
			continue
		}
		pairs = append(pairs, "(k.tenant_id = ? AND k.knowledge_base_id = ?)")
		args = append(args, scope.TenantID, scope.KBID)
	}
	if len(pairs) == 0 {
		return nil, nil
	}
	// Restrict eligible chunks before choosing documents, so disabled or tag-excluded
	// chunks cannot use the small title budget or become model evidence.
	eligible := func() *gorm.DB {
		q := r.db.WithContext(ctx).Table("chunks AS c").Where("c.deleted_at IS NULL AND c.is_enabled = ? AND c.index_status = ? AND c.chunk_type = ?", true, "ready", types.ChunkTypeText)
		if len(params.TagIDs) > 0 {
			q = q.Where("c.tag_id IN ?", params.TagIDs)
		}
		return q
	}
	docs := r.db.WithContext(ctx).Table("knowledges AS k").
		Joins("JOIN knowledge_bases AS kb ON kb.id = k.knowledge_base_id AND kb.tenant_id = k.tenant_id").
		Where("("+strings.Join(pairs, " OR ")+")", args...).
		Where("kb.deleted_at IS NULL AND kb.type = ? AND k.deleted_at IS NULL AND k.enable_status = ? AND k.parse_status = ?", types.KnowledgeBaseTypeDocument, "enabled", "completed").
		Where(titleSQL("k.title")+" LIKE ? ESCAPE ?", "%"+escapeLikeKeyword(query)+"%", likeEscapeChar).
		Where("EXISTS (?)", eligible().Select("1").Where("c.knowledge_id = k.id AND c.tenant_id = k.tenant_id AND c.knowledge_base_id = k.knowledge_base_id"))
	if len(params.KnowledgeIDs) > 0 {
		docs = docs.Where("k.id IN ?", params.KnowledgeIDs)
	}
	docs = docs.Select("k.id, k.tenant_id, k.knowledge_base_id, CASE WHEN "+titleSQL("k.title")+" IN ? THEN 1 ELSE 0 END AS title_exact", exactNames).
		Order("title_exact DESC, LENGTH(k.title), k.id").Limit(5)
	ranked := eligible().Joins("JOIN (?) AS d ON d.id = c.knowledge_id AND d.tenant_id = c.tenant_id AND d.knowledge_base_id = c.knowledge_base_id", docs).
		Select("c.id, c.knowledge_id, c.knowledge_base_id, c.content, c.tag_id, d.title_exact, ROW_NUMBER() OVER (PARTITION BY c.tenant_id,c.knowledge_id ORDER BY CASE WHEN "+titleSQL("c.content")+" LIKE ? ESCAPE ? THEN 0 ELSE 1 END, c.chunk_index,c.id) AS title_row", "%"+escapeLikeKeyword(query)+"%", likeEscapeChar)
	var rows []struct {
		ID, KnowledgeID, KnowledgeBaseID, Content, TagID string
		TitleExact                                       int
		TitleRow                                         int
	}
	err := r.db.WithContext(ctx).Table("(?) AS ranked", ranked).Where("title_row <= 3").Order("title_exact DESC, knowledge_id, title_row").Limit(15).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*types.IndexWithScore, 0, len(rows))
	for _, row := range rows {
		score := 0.5
		if row.TitleExact == 1 {
			score = 0.7
		}
		out = append(out, &types.IndexWithScore{ID: row.ID, ChunkID: row.ID, SourceID: row.ID, SourceType: types.ChunkSourceType, KnowledgeID: row.KnowledgeID, KnowledgeBaseID: row.KnowledgeBaseID, TagID: row.TagID, Content: row.Content, Score: score, MatchType: types.MatchTypeKeywords, IsEnabled: true})
	}
	return out, nil
}
