package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDocumentTitleRecallIntersectsScopesAndLiveEvidence(t *testing.T) {
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
			require.NoError(t, db.Exec(`INSERT INTO chunks VALUES (?,?,?,?,?,'tag-a',true,'ready','text',?,NULL)`, fmt.Sprintf("%s-%d", id, i), tenant, kb, id, "EMC 设计正文", i).Error)
		}
	}
	add("exact", "allowed", "EMC设计.pdf", 1)
	add("partial", "allowed", "PCB--emc设计指南.pdf", 1)
	add("outside", "other", "EMC设计.pdf", 1)
	add("shared-doc", "shared", "EMC设计.pdf", 2)
	add("foreign", "forbidden", "EMC设计.pdf", 2)
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
	repo := NewChunkRepository(db)
	scope := []types.KnowledgeSearchScope{{TenantID: 1, KBID: "allowed"}}
	t.Run("scoped clauses use the exact standard edition and the relevant body", func(t *testing.T) {
		add("standard", "allowed", "GBT 1000.2-2024（试验方法）.pdf", 1)
		add("old-standard", "allowed", "GB/T 1000.2-2020.pdf", 1)
		add("prefix-only", "allowed", "GB-T 1000.2-20240.pdf", 1)
		add("outside-standard", "other", "GB-T 1000.2-2024.pdf", 1)
		add("storage", "allowed", "试剂规范.pdf", 1)
		add("storage-copy", "allowed", "试剂规范(1).pdf", 1)
		for _, id := range []string{"standard", "old-standard", "prefix-only", "outside-standard"} {
			require.NoError(t, db.Exec("UPDATE chunks SET content=? WHERE id=?",
				"恢复方法与时间由相关规范规定。", id+"-4").Error)
		}
		for _, id := range []string{"storage", "storage-copy"} {
			require.NoError(t, db.Exec("UPDATE chunks SET content=? WHERE id=?",
				"储存稳定性：35 ± 1 ℃，48 ± 2 h，试样无裂纹。", id+"-3").Error)
		}
		require.NoError(t, db.Exec("UPDATE chunks SET content=? WHERE id=?",
			"储存稳定性 …… 12", "storage-0").Error)
		p := types.SearchParams{
			QueryText:           "gbt1000.2-2024",
			NamedOverviewTitles: []string{"gbt1000.2-2024", "试剂规范"},
			NamedOverviewQuestion: "按 GB/T 1000.2-2024，恢复方法与时间由谁规定？" +
				"按《试剂规范》，储存稳定性如何判定？",
		}
		hits, err := repo.SearchDocumentTitleChunks(t.Context(), scope, p)
		require.NoError(t, err)
		require.Len(t, hits, 1)
		require.Equal(t, "standard-4", hits[0].ChunkID)
		require.Equal(t, types.MatchTypeDocumentOverview, hits[0].MatchType)

		p.QueryText = "试剂规范"
		hits, err = repo.SearchDocumentTitleChunks(t.Context(), scope, p)
		require.NoError(t, err)
		require.Len(t, hits, 1)
		require.Equal(t, "storage-3", hits[0].ChunkID)
		p.KnowledgeIDs = []string{"storage-copy"}
		hits, err = repo.SearchDocumentTitleChunks(t.Context(), scope, p)
		require.NoError(t, err)
		require.Empty(t, hits)
	})
	for _, query := range []string{"EMC设计", "emc 设计", " EMC设计.PDF "} {
		got, err := repo.SearchDocumentTitleChunks(context.Background(), scope, types.SearchParams{QueryText: query})
		require.NoError(t, err)
		require.Len(t, got, 6)
		counts := map[string]int{}
		for _, row := range got {
			counts[row.KnowledgeID]++
			require.Equal(t, "allowed", row.KnowledgeBaseID)
			require.NotEmpty(t, row.Content)
		}
		require.Equal(t, map[string]int{"exact": 3, "partial": 3}, counts)
		require.Equal(t, "exact", got[0].KnowledgeID)
	}
	t.Run("named overview uses exact authorized title and prefers purpose", func(t *testing.T) {
		add("copy", "allowed", "EMC设计(1).pdf", 1)
		require.NoError(t, db.Exec("UPDATE chunks SET content=? WHERE id='exact-0'", "目录\n1.目的 …… 3").Error)
		require.NoError(t, db.Exec("UPDATE chunks SET content=? WHERE id='exact-1'", "文件编号 XY-1").Error)
		require.NoError(t, db.Exec("UPDATE chunks SET content=? WHERE id='exact-4'", "## 1.目的\n规范设计与验收。").Error)
		params := types.SearchParams{
			QueryText: "EMC设计", NamedOverviewTitles: []string{"emc设计", "试验规范"}, TagIDs: []string{"tag-a"},
		}
		got, err := repo.SearchDocumentTitleChunks(t.Context(), scope, params)
		require.NoError(t, err)
		require.Len(t, got, 3)
		require.Equal(t, "exact-4", got[0].ChunkID)
		require.Equal(t, "exact-1", got[1].ChunkID)
		for _, row := range got {
			require.Equal(t, "exact", row.KnowledgeID)
			require.Equal(t, types.MatchTypeDocumentOverview, row.MatchType)
		}
		params.KnowledgeIDs = []string{"outside"}
		got, err = repo.SearchDocumentTitleChunks(t.Context(), scope, params)
		require.NoError(t, err)
		require.Empty(t, got)
		// The exact-title path must also respect the caller's document allowlist.
		params.KnowledgeIDs = []string{"copy"}
		got, err = repo.SearchDocumentTitleChunks(t.Context(), scope, params)
		require.NoError(t, err)
		require.Empty(t, got)
		require.NoError(t, db.Exec("UPDATE chunks SET is_enabled=false WHERE knowledge_id='copy'").Error)
	})
	cases := []struct {
		name   string
		scope  []types.KnowledgeSearchScope
		params types.SearchParams
		want   string
		count  int
	}{
		{"no scope", nil, types.SearchParams{QueryText: "EMC设计"}, "", 0},
		{"wrong tenant", []types.KnowledgeSearchScope{{TenantID: 1, KBID: "shared"}}, types.SearchParams{QueryText: "EMC设计"}, "", 0},
		{"authorized shared", []types.KnowledgeSearchScope{{TenantID: 2, KBID: "shared"}}, types.SearchParams{QueryText: "EMC设计"}, "shared-doc", 3},
		{"explicit document", scope, types.SearchParams{QueryText: "EMC设计", KnowledgeIDs: []string{"partial"}}, "partial", 3},
		{"outside document cannot expand kb", scope, types.SearchParams{QueryText: "EMC设计", KnowledgeIDs: []string{"outside"}}, "", 0},
		{"missing tag", scope, types.SearchParams{QueryText: "EMC设计", TagIDs: []string{"tag-b"}}, "", 0},
		{"tag and document intersect", scope, types.SearchParams{QueryText: "EMC设计", TagIDs: []string{"tag-a"}, KnowledgeIDs: []string{"exact"}}, "exact", 3},
		{"unresolved logical tag", scope, types.SearchParams{QueryText: "EMC设计", ScopeTagIDs: []string{"tag-a"}}, "", 0},
		{"wildcards are literal", scope, types.SearchParams{QueryText: "EMC%"}, "", 0},
		{"injection is literal", scope, types.SearchParams{QueryText: "' OR 1=1 --"}, "", 0},
		{"recommendation gate", scope, types.SearchParams{QueryText: "EMC设计", OnlyRecommended: true}, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repo.SearchDocumentTitleChunks(context.Background(), tc.scope, tc.params)
			require.NoError(t, err)
			require.Len(t, got, tc.count)
			for _, r := range got {
				require.Equal(t, tc.want, r.KnowledgeID)
			}
		})
	}
}
