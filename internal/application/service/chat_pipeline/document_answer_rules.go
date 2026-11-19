package chatpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
)

type documentAnswerRule struct {
	KnowledgeID   string   `json:"knowledge_id"`
	FileHash      string   `json:"file_hash"`
	EvidenceTerms []string `json:"evidence_terms"`
	Instruction   string   `json:"instruction"`
}

type documentAnswerRulesConfig struct {
	AgentID string               `json:"agent_id"`
	Rules   []documentAnswerRule `json:"rules"`
}

type documentVersionLookup func(context.Context, uint64, []string) ([]*types.Knowledge, error)

type documentAnswerRuleScope struct {
	rules  []documentAnswerRule
	lookup documentVersionLookup
}

type documentAnswerRuleKey struct{}

var configuredDocumentAnswerRules = sync.OnceValues(func() (documentAnswerRulesConfig, error) {
	return loadDocumentAnswerRules(os.Getenv("WEKNORA_DOCUMENT_ANSWER_RULES_FILE"))
})

func loadDocumentAnswerRules(path string) (documentAnswerRulesConfig, error) {
	var cfg documentAnswerRulesConfig
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	seen := make(map[string]bool, len(cfg.Rules))
	for i, rule := range cfg.Rules {
		if strings.TrimSpace(cfg.AgentID) == "" || strings.TrimSpace(rule.KnowledgeID) == "" ||
			strings.TrimSpace(rule.FileHash) == "" || strings.TrimSpace(rule.Instruction) == "" ||
			len(rule.EvidenceTerms) == 0 || seen[rule.KnowledgeID] {
			return cfg, fmt.Errorf("invalid or duplicate document answer rule at index %d", i)
		}
		for _, term := range rule.EvidenceTerms {
			if strings.TrimSpace(term) == "" {
				return cfg, fmt.Errorf("empty evidence term in document answer rule at index %d", i)
			}
		}
		seen[rule.KnowledgeID] = true
	}
	return cfg, nil
}

// WithDocumentAnswerRules enables operator-managed rules for the selected knowledge QA expert.
func WithDocumentAnswerRules(ctx context.Context, agentID string, lookup documentVersionLookup) context.Context {
	cfg, err := configuredDocumentAnswerRules()
	if err != nil {
		pipelineWarn(ctx, "DocumentRules", "config_unavailable", map[string]interface{}{"error": err.Error()})
		return ctx
	}
	return cfg.withScope(ctx, agentID, lookup)
}

func (cfg documentAnswerRulesConfig) withScope(
	ctx context.Context, agentID string, lookup documentVersionLookup,
) context.Context {
	if agentID == "" || cfg.AgentID != agentID || len(cfg.Rules) == 0 || lookup == nil {
		return ctx
	}
	return context.WithValue(ctx, documentAnswerRuleKey{}, documentAnswerRuleScope{rules: cfg.Rules, lookup: lookup})
}

func documentAnswerInstructions(ctx context.Context, cm *types.ChatManage, evidence []*types.SearchResult) string {
	scope, ok := ctx.Value(documentAnswerRuleKey{}).(documentAnswerRuleScope)
	if !ok {
		return ""
	}
	var candidates []documentAnswerRule
	var ids []string
	for _, rule := range scope.rules {
		if matchesDocumentAnswerEvidence(rule, evidence) {
			candidates = append(candidates, rule)
			ids = append(ids, rule.KnowledgeID)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	docs, err := scope.lookup(ctx, cm.TenantID, ids)
	if err != nil {
		pipelineWarn(ctx, "DocumentRules", "version_unavailable", map[string]interface{}{"session_id": cm.SessionID})
		return ""
	}
	versions := make(map[string]string, len(docs))
	for _, doc := range docs {
		if doc != nil {
			versions[doc.ID] = doc.FileHash
		}
	}
	var instructions strings.Builder
	var applied []string
	for _, rule := range candidates {
		if versions[rule.KnowledgeID] != rule.FileHash {
			continue
		}
		instructions.WriteString("\n\n")
		instructions.WriteString(rule.Instruction)
		applied = append(applied, rule.KnowledgeID)
	}
	if len(applied) > 0 {
		pipelineInfo(ctx, "DocumentRules", "applied", map[string]interface{}{
			"session_id": cm.SessionID,
			"documents":  applied,
		})
	}
	return instructions.String()
}

func matchesDocumentAnswerEvidence(rule documentAnswerRule, evidence []*types.SearchResult) bool {
	var body strings.Builder
	for _, result := range evidence {
		if result != nil && result.KnowledgeID == rule.KnowledgeID {
			body.WriteString(result.Content)
			body.WriteByte('\n')
		}
	}
	text := strings.ToLower(body.String())
	for _, term := range rule.EvidenceTerms {
		if !strings.Contains(text, strings.ToLower(term)) {
			return false
		}
	}
	return true
}
