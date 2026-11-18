package repository

import (
	"context"
	"fmt"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
)

func TestLiteralRecallIntersectsScopesAndLiveEvidence(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, sql := range []string{
		`CREATE TABLE knowledge_bases (id TEXT,tenant_id INTEGER,type TEXT,deleted_at TEXT)`,
		`CREATE TABLE knowledges (id TEXT,tenant_id INTEGER,knowledge_base_id TEXT,title TEXT,parse_status TEXT,enable_status TEXT,deleted_at TEXT)`,
		`CREATE TABLE chunks (id TEXT,tenant_id INTEGER,knowledge_base_id TEXT,knowledge_id TEXT,content TEXT,tag_id TEXT,is_enabled BOOLEAN,index_status TEXT,chunk_type TEXT,chunk_index INTEGER,deleted_at TEXT)`,
	} {
		require.NoError(t, db.Exec(sql).Error)
	}
	for _, kb := range []struct {
		id     string
		tenant int
	}{{"allowed", 1}, {"other", 1}, {"shared", 2}, {"forbidden", 2}} {
		require.NoError(t, db.Exec(`INSERT INTO knowledge_bases VALUES (?,?,'document',NULL)`, kb.id, kb.tenant).Error)
	}
	add := func(id, kb, title string, tenant int) {
		require.NoError(t, db.Exec(`INSERT INTO knowledges VALUES (?,?,?,?,'completed','enabled',NULL)`, id, tenant, kb, title).Error)
		for i := 0; i < 5; i++ {
			require.NoError(t, db.Exec(`INSERT INTO chunks VALUES (?,?,?,?,?,'tag-a',true,'ready','text',?,NULL)`, fmt.Sprintf("%s-%d", id, i), tenant, kb, id, "| 型号 | VF-101-P200-0380-A-T4-E20-N-H-D | 故障：上电异常 |", i).Error)
		}
	}
	add("exact", "allowed", "EMC设计.pdf", 1)
	add("partial", "allowed", "PCB--emc设计指南.pdf", 1)
	add("outside", "other", "EMC设计.pdf", 1)
	add("shared-doc", "shared", "EMC设计.pdf", 2)
	add("foreign", "forbidden", "EMC设计.pdf", 2)
	add("similar-code", "allowed", "other.xlsx", 1)
	require.NoError(t, db.Exec(`UPDATE chunks SET content='VF-101-P200-0380-A-T4-E20-N-H-D-REV' WHERE knowledge_id='similar-code'`).Error)
	for _, id := range []string{"disabled", "deleted", "parsing", "unindexed", "disabled-chunks", "deleted-chunks"} {
		add(id, "allowed", "EMC设计.pdf", 1)
	}
	for _, sql := range []string{
		`UPDATE knowledges SET enable_status='disabled' WHERE id='disabled'`,
		`UPDATE knowledges SET deleted_at='2026-01-01' WHERE id='deleted'`,
		`UPDATE knowledges SET parse_status='processing' WHERE id='parsing'`,
		`UPDATE chunks SET index_status='pending' WHERE knowledge_id='unindexed'`,
		`UPDATE chunks SET is_enabled=false WHERE knowledge_id='disabled-chunks'`,
		`UPDATE chunks SET deleted_at='2026-01-01' WHERE knowledge_id='deleted-chunks'`,
	} {
		require.NoError(t, db.Exec(sql).Error)
	}
	repo := &chunkRepository{db: db}
	scope := []types.KnowledgeSearchScope{{TenantID: 1, KBID: "allowed"}}
	for _, query := range []string{"VF-101-P200-0380-A-T4-E20-N-H-D", "vf-101-p200-0380-a-t4-e20-n-h-d", " VF-101-P200-0380-A-T4-E20-N-H-D "} {
		got, err := repo.SearchLiteralChunks(context.Background(), scope, types.SearchParams{MatchCount: 50, QueryText: query})
		require.NoError(t, err)
		require.Len(t, got, 10)
		counts := map[string]int{}
		for _, row := range got {
			counts[row.KnowledgeID]++
			require.Equal(t, "allowed", row.KnowledgeBaseID)
			require.NotEmpty(t, row.Content)
		}
		require.Equal(t, map[string]int{"exact": 5, "partial": 5}, counts)
		require.Equal(t, "exact", got[0].KnowledgeID)
		require.Equal(t, "partial", got[1].KnowledgeID)
	}
	t.Run("complete identifier ignores semantic top k", func(t *testing.T) {
		got, err := repo.SearchLiteralChunks(context.Background(), scope, types.SearchParams{QueryText: "VF-101-P200-0380-A-T4-E20-N-H-D", MatchCount: 1, LiteralAll: true})
		require.NoError(t, err)
		require.Len(t, got, 10)
		got, err = repo.SearchLiteralChunks(context.Background(), scope, types.SearchParams{QueryText: "VF-101-P200-0380-A-T4-E20-N-H-D", MatchCount: 1, LiteralAll: true, KnowledgeIDs: []string{"outside"}})
		require.NoError(t, err)
		require.Empty(t, got)
	})
	cases := []struct {
		name   string
		scope  []types.KnowledgeSearchScope
		params types.SearchParams
		want   string
		count  int
	}{
		{"no scope", nil, types.SearchParams{MatchCount: 50, QueryText: "VF-101-P200-0380-A-T4-E20-N-H-D"}, "", 0},
		{"wrong tenant", []types.KnowledgeSearchScope{{TenantID: 1, KBID: "shared"}}, types.SearchParams{MatchCount: 50, QueryText: "VF-101-P200-0380-A-T4-E20-N-H-D"}, "", 0},
		{"authorized shared", []types.KnowledgeSearchScope{{TenantID: 2, KBID: "shared"}}, types.SearchParams{MatchCount: 50, QueryText: "VF-101-P200-0380-A-T4-E20-N-H-D"}, "shared-doc", 5},
		{"explicit document", scope, types.SearchParams{MatchCount: 50, QueryText: "VF-101-P200-0380-A-T4-E20-N-H-D", KnowledgeIDs: []string{"partial"}}, "partial", 5},
		{"outside document cannot expand kb", scope, types.SearchParams{MatchCount: 50, QueryText: "VF-101-P200-0380-A-T4-E20-N-H-D", KnowledgeIDs: []string{"outside"}}, "", 0},
		{"missing tag", scope, types.SearchParams{MatchCount: 50, QueryText: "VF-101-P200-0380-A-T4-E20-N-H-D", TagIDs: []string{"tag-b"}}, "", 0},
		{"tag and document intersect", scope, types.SearchParams{MatchCount: 50, QueryText: "VF-101-P200-0380-A-T4-E20-N-H-D", TagIDs: []string{"tag-a"}, KnowledgeIDs: []string{"exact"}}, "exact", 5},
		{"unresolved logical tag", scope, types.SearchParams{MatchCount: 50, QueryText: "VF-101-P200-0380-A-T4-E20-N-H-D", ScopeTagIDs: []string{"tag-a"}}, "", 0},
		{"wildcards are literal", scope, types.SearchParams{MatchCount: 50, QueryText: "EMC%"}, "", 0},
		{"injection is literal", scope, types.SearchParams{MatchCount: 50, QueryText: "' OR 1=1 --"}, "", 0},
		{"recommendation gate", scope, types.SearchParams{MatchCount: 50, QueryText: "VF-101-P200-0380-A-T4-E20-N-H-D", OnlyRecommended: true}, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repo.SearchLiteralChunks(context.Background(), tc.scope, tc.params)
			require.NoError(t, err)
			require.Len(t, got, tc.count)
			for _, r := range got {
				require.Equal(t, tc.want, r.KnowledgeID)
			}
		})
	}
}
