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
	overview := len(params.NamedOverviewTitles) > 0
	detail := ""
	if overview {
		for _, request := range searchutil.DocumentDetailRequests(params.NamedOverviewQuestion) {
			if request.Title == query {
				detail = request.Question
				break
			}
		}
	}
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
		Where("EXISTS (?)", eligible().Select("1").Where("c.knowledge_id = k.id AND c.tenant_id = k.tenant_id AND c.knowledge_base_id = k.knowledge_base_id"))
	titleColumn := titleSQL("k.title")
	if detail != "" && searchutil.StandardDocumentNumber(query) == query {
		aliases := []string{query}
		if suffix, ok := strings.CutPrefix(query, "gbt"); ok {
			aliases = append(aliases, "gb/t"+suffix, "gb-t"+suffix)
		}
		prefixes := make([]string, 0, len(aliases))
		values := make([]interface{}, 0, 2*len(aliases))
		for _, alias := range aliases {
			prefixes = append(prefixes, titleColumn+" LIKE ? ESCAPE ?")
			values = append(values, escapeLikeKeyword(alias)+"%", likeEscapeChar)
		}
		docs = docs.Where("("+strings.Join(prefixes, " OR ")+")", values...)
	} else if overview {
		docs = docs.Where(titleColumn+" IN ?", exactNames)
	} else {
		docs = docs.Where(titleSQL("k.title")+" LIKE ? ESCAPE ?", "%"+escapeLikeKeyword(query)+"%", likeEscapeChar)
	}
	if len(params.KnowledgeIDs) > 0 {
		docs = docs.Where("k.id IN ?", params.KnowledgeIDs)
	}
	docs = docs.Select("k.id, k.tenant_id, k.knowledge_base_id, k.title, CASE WHEN "+
		titleColumn+" IN ? THEN 1 ELSE 0 END AS title_exact", exactNames).
		Order("title_exact DESC, LENGTH(k.title), k.id").Limit(5)
	rankExpr := "CASE WHEN " + titleSQL("c.content") + " LIKE ? ESCAPE ? THEN 0 ELSE 1 END"
	rankArgs := []interface{}{"%" + escapeLikeKeyword(query) + "%", likeEscapeChar}
	candidatesPerDocument := 3
	if overview {
		// Read a bounded prefix of the matched document so body sections can
		// outrank cover pages and TOC lines even when they lack the title phrase.
		rankExpr, rankArgs = "c.chunk_index", nil
		candidatesPerDocument = namedOverviewCandidatesPerDocument
		if detail != "" {
			candidatesPerDocument = namedDetailCandidatesPerDocument
		}
	}
	docJoin := "JOIN (?) AS d ON d.id = c.knowledge_id AND d.tenant_id = c.tenant_id" +
		" AND d.knowledge_base_id = c.knowledge_base_id"
	selectPassages := "c.id, c.knowledge_id, c.knowledge_base_id, c.content, c.tag_id, d.title, d.title_exact," +
		" ROW_NUMBER() OVER (PARTITION BY c.tenant_id,c.knowledge_id" +
		" ORDER BY " + rankExpr + ", c.chunk_index,c.id) AS title_row"
	ranked := eligible().Joins(docJoin, docs).Select(selectPassages, rankArgs...)
	var rows []documentTitlePassage
	err := r.db.WithContext(ctx).Table("(?) AS ranked", ranked).
		Where("title_row <= ?", candidatesPerDocument).Order("title_exact DESC, knowledge_id, title_row").
		Limit(candidatesPerDocument * 5).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if detail != "" {
		// Prefix lookup is only a candidate search. Confirm the complete
		// standard and edition before accepting an authorized document.
		checked := rows[:0]
		for _, row := range rows {
			if searchutil.MatchesEvidenceDocument(query, row.Title) {
				checked = append(checked, row)
			}
		}
		rows = checked
	}
	if overview && len(rows) > 0 {
		first, last := rows[0], rows[len(rows)-1]
		if first.KnowledgeID != last.KnowledgeID || first.KnowledgeBaseID != last.KnowledgeBaseID {
			return nil, nil // An ambiguous exact title falls back to ordinary recall.
		}
		if detail != "" {
			rows = selectDocumentDetailPassages(detail, rows)
		} else {
			rows = selectNamedOverviewPassages(params.NamedOverviewQuestion, rows)
		}
	}
	out := make([]*types.IndexWithScore, 0, len(rows))
	for _, row := range rows {
		score := 0.5
		if row.TitleExact == 1 {
			score = 0.7
		}
		match := types.MatchTypeKeywords
		if overview {
			match = types.MatchTypeDocumentOverview
		}
		out = append(out, &types.IndexWithScore{
			ID: row.ID, ChunkID: row.ID, SourceID: row.ID, SourceType: types.ChunkSourceType,
			KnowledgeID: row.KnowledgeID, KnowledgeBaseID: row.KnowledgeBaseID, TagID: row.TagID,
			Content: row.Content, Score: score, MatchType: match, IsEnabled: true,
		})
	}
	return out, nil
}
