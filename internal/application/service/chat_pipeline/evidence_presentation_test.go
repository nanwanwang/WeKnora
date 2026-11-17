package chatpipeline

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEvidencePresentationUsesOnlyFinalScopedPassages(t *testing.T) {
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: "EMC", SummaryConfig: types.SummaryConfig{Prompt: "system"}},
		PipelineState: types.PipelineState{
			UserContent:  "EMC",
			SearchResult: []*types.SearchResult{{ID: "dropped-chunk", KnowledgeBaseID: "outside-kb", Content: "outside evidence ![](resource://outside-image)"}},
			MergeResult:  []*types.SearchResult{{ID: "allowed-chunk", KnowledgeID: "allowed-doc", KnowledgeBaseID: "allowed-kb", KnowledgeTitle: "EMC guide", Content: "EMC structure ![diagram](resource://abcdefghijklmnopqrstuv)"}},
		},
	}
	messages, registry := prepareMessagesWithModelContext(context.Background(), manage)
	messages = registry.EncodeMessages(messages)
	require.Contains(t, messages[1].Content, "<citation_catalog>")
	require.Contains(t, messages[1].Content, `source=chunk://1 document="EMC guide" citation=<ref id="1"/>`)
	require.Contains(t, messages[1].Content, "available_illustration=![diagram](res://0001) source_citation=<ref id=\"1\"/>")
	require.NotContains(t, messages[1].Content, "outside")
	require.Contains(t, messages[1].Content, "本轮可用原图（来源：EMC guide）")
	decoded := registry.DecodeOutputText(`![diagram](res://0001) <ref id="1"/>`)
	require.Contains(t, decoded, "![diagram](resource://abcdefghijklmnopqrstuv)")
	require.Contains(t, decoded, `chunk_id="allowed-chunk" kb_id="allowed-kb"`)
	require.Empty(t, registry.DecodeOutputText(`<ref id="2"/>`))
	disabled := false
	manage.CitationEnabled = &disabled
	messages, registry = prepareMessagesWithModelContext(context.Background(), manage)
	require.NotContains(t, messages[1].Content, "<citation_catalog>")
	require.Empty(t, registry.DecodeOutputText(`<ref id="1"/>`))
}
