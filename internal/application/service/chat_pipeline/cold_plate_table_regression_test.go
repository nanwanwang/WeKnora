package chatpipeline

import (
	"html"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestN224TwoTurnPressureTableKeepsQualifiedGapAndCitation(t *testing.T) {
	const (
		first   = "根据《液冷板设计规范》，电力IGBT、SVG液冷板一般按多大的压力等级设计？"
		second  = "那这个等级下板厚6mm的话，摩擦间隙最大不超过多少？"
		answer  = "一般要求约 10 bar（1 MPa）。"
		session = "cold-plate-session"
	)
	// Minimal fixture for three adjacent pressure bands sharing the same thickness.
	const table = `<table><thead>
<tr><th>压力等级</th><th>应用场景</th><th>基板厚度 (mm)</th><th>摩擦间隙上限 (mm)</th></tr>
</thead><tbody>
<tr><td>6 bar</td><td>汽车电池</td><td>6</td><td>57</td></tr>
<tr><td>10 bar</td><td>电力 IGBT、SVG</td><td>6</td><td>42</td></tr>
<tr><td>30 bar</td><td>冷媒</td><td>6</td><td>30</td></tr>
</tbody></table>`

	chunks := chunker.SplitParentChild(table,
		chunker.SplitterConfig{ChunkSize: 1200},
		chunker.SplitterConfig{ChunkSize: 110, Strategy: chunker.StrategyAuto},
	)
	var target *types.SearchResult
	for _, child := range chunks.Children {
		if strings.Contains(child.Content, "<td>10 bar</td>") {
			target = &types.SearchResult{
				ID: "cold-plate-10-bar", KnowledgeID: "cold-plate", KnowledgeBaseID: "manuals",
				KnowledgeTitle: "液冷板设计规范.pdf", Content: child.Content, ContextHeader: child.ContextHeader,
				SourceLocators: types.SourceLocators{{Type: types.SourceLocatorPDF, Page: 9}},
			}
			break
		}
	}
	require.NotNil(t, target)
	require.NotContains(t, target.Content, "<td>57</td>")
	const evidence = "<tr><td>10 bar</td><td>电力 IGBT、SVG</td><td>6</td><td>42</td></tr>"
	const columns = "压力等级 | 应用场景 | 基板厚度 (mm) | 摩擦间隙上限 (mm)"
	require.Contains(t, target.Content, evidence)

	turn := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: first, SessionID: session, MaxRounds: 5},
		PipelineState: types.PipelineState{
			UserContent: first,
			MergeResult: []*types.SearchResult{target},
		},
	}
	plugin := &PluginQueryUnderstand{}
	require.Nil(t, plugin.OnEvent(t.Context(), types.QUERY_UNDERSTAND, turn, func() *PluginError {
		require.Equal(t, first, turn.RewriteQuery)
		return nil
	}))
	messages, registry := prepareMessagesWithModelContext(t.Context(), turn)
	prompt := messages[len(messages)-1].Content
	require.Contains(t, prompt, first)
	require.Contains(t, prompt, columns)
	require.Contains(t, html.UnescapeString(prompt), evidence)
	require.Contains(t, registry.DecodeOutputText(`<ref id="1"/>`), `chunk_id="cold-plate-10-bar"`)

	turn.Query, turn.UserContent = second, second
	turn.History = []*types.History{{UserQuery: first, Query: first, Answer: answer}}
	require.Nil(t, plugin.OnEvent(t.Context(), types.QUERY_UNDERSTAND, turn, func() *PluginError {
		require.Equal(t, second, turn.Query)
		require.Equal(t, second, turn.UserContent)
		require.Equal(t, session, turn.SessionID)
		return nil
	}))
	messages, registry = prepareMessagesWithModelContext(t.Context(), turn)
	prompt = messages[len(messages)-1].Content
	require.Contains(t, prompt, second)
	require.Contains(t, messages[len(messages)-3].Content, first)
	require.Equal(t, answer, messages[len(messages)-2].Content)
	require.Contains(t, prompt, columns)
	require.Contains(t, html.UnescapeString(prompt), evidence)
	require.NotContains(t, html.UnescapeString(prompt), "<td>57</td>")
	require.Contains(t, registry.DecodeOutputText(`<ref id="1"/>`), `chunk_id="cold-plate-10-bar"`)
	require.Equal(t, 9, target.SourceLocators[0].Page)
}
