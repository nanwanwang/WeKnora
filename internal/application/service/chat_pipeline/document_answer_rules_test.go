package chatpipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

const testAnswerInstruction = "Use the original numbering from the approved source."

func TestPrepareMessagesWithDocumentAnswerRules(t *testing.T) {
	matches := &types.SearchResult{
		ID: "chunk-1", KnowledgeID: "file-1", KnowledgeTitle: "Manual.pdf", Content: "Original workflow nodes",
	}
	unrelated := &types.SearchResult{
		ID: "chunk-2", KnowledgeID: "file-1", KnowledgeTitle: "Manual.pdf", Content: "Appendix",
	}
	sameName := *matches
	sameName.KnowledgeID = "file-2"
	web := *matches
	web.ChunkType = string(types.ChunkTypeWebSearch)
	cfg := documentAnswerRulesConfig{
		AgentID: "expert-1",
		Rules: []documentAnswerRule{{
			KnowledgeID: "file-1", FileHash: "version-1", EvidenceTerms: []string{"Workflow", "nodes"},
			Instruction: testAnswerInstruction,
		}},
	}
	for _, tc := range []struct {
		name           string
		agent          string
		merged         *types.SearchResult
		currentVersion string
		lookupErr      error
		wantLookup     bool
		wantRule       bool
	}{
		{"matched document and version", "expert-1", matches, "version-1", nil, true, true},
		{"document replaced with same ID", "expert-1", matches, "version-2", nil, true, false},
		{"document outside authorized lookup", "expert-1", matches, "", nil, true, false},
		{"version lookup failed", "expert-1", matches, "version-1", errors.New("unavailable"), true, false},
		{"same title different document", "expert-1", &sameName, "version-1", nil, false, false},
		{"matching document unrelated passage", "expert-1", unrelated, "version-1", nil, false, false},
		{"matching terms only on the web", "expert-1", &web, "version-1", nil, false, false},
		{"matching passage not selected for model", "expert-1", nil, "version-1", nil, false, false},
		{"different expert", "expert-2", matches, "version-1", nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookedUp := false
			lookup := func(_ context.Context, tenant uint64, ids []string) ([]*types.Knowledge, error) {
				lookedUp = true
				if tenant != 7 || len(ids) != 1 || ids[0] != "file-1" {
					t.Fatalf("unexpected version lookup tenant=%d ids=%v", tenant, ids)
				}
				if tc.currentVersion == "" {
					return nil, tc.lookupErr
				}
				return []*types.Knowledge{{ID: "file-1", FileHash: tc.currentVersion}}, tc.lookupErr
			}
			cm := &types.ChatManage{
				PipelineRequest: types.PipelineRequest{
					TenantID: 7, Query: "Which workflow nodes are shown?",
					SummaryConfig: types.SummaryConfig{Prompt: "Keep the general expert prompt."},
				},
				PipelineState: types.PipelineState{
					SearchResult: []*types.SearchResult{matches}, UserContent: "Which workflow nodes are shown?",
				},
			}
			if tc.merged != nil {
				cm.MergeResult = []*types.SearchResult{tc.merged}
			}
			ctx := cfg.withScope(context.Background(), tc.agent, lookup)
			messages, _ := prepareMessagesWithModelContext(ctx, cm)
			if got := strings.Contains(messages[0].Content, testAnswerInstruction); got != tc.wantRule {
				t.Errorf("system prompt includes document rule = %v, want %v", got, tc.wantRule)
			}
			if !strings.Contains(messages[0].Content, cm.SummaryConfig.Prompt) {
				t.Error("general expert prompt was lost")
			}
			if lookedUp != tc.wantLookup {
				t.Errorf("looked up version = %v, want %v", lookedUp, tc.wantLookup)
			}
		})
	}
}

func TestLoadDocumentAnswerRules(t *testing.T) {
	if cfg, err := loadDocumentAnswerRules(""); err != nil || len(cfg.Rules) != 0 {
		t.Fatalf("without a configured path, got rules %+v, error %v", cfg, err)
	}
	rule := `{"knowledge_id":"file-1","file_hash":"version-1",` +
		`"evidence_terms":["workflow"],"instruction":"quote original nodes"}`
	const prefix = `{"agent_id":"expert-1","rules":[`
	for _, tc := range []struct {
		name, body string
		wantError  bool
	}{
		{"valid", prefix + rule + `]}`, false},
		{"duplicate document", prefix + rule + `,` + rule + `]}`, true},
		{"missing expert", `{"rules":[` + rule + `]}`, true},
		{"missing version", prefix + strings.Replace(rule, "version-1", "", 1) + `]}`, true},
		{"blank term", prefix + strings.Replace(rule, "workflow", " ", 1) + `]}`, true},
		{"invalid JSON", `{"rules":[`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rules.json")
			if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := loadDocumentAnswerRules(path)
			if (err != nil) != tc.wantError {
				t.Fatalf("load rules error = %v, want error %v", err, tc.wantError)
			}
			if err == nil && (cfg.AgentID != "expert-1" || len(cfg.Rules) != 1 ||
				cfg.Rules[0].FileHash != "version-1") {
				t.Fatalf("unexpected decoded rules: %+v", cfg)
			}
		})
	}
}
