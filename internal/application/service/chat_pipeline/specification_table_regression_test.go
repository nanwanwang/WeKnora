package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestN273FollowupReceivesQualifiedRowAndOriginalCitation(t *testing.T) {
	const question = "那要是换成直流供电呢，同样25℃下能过多少安？"
	table := `<table>
<tr><td rowspan="2">铜排尺寸</td><td colspan="2">交流 (A)</td><td colspan="2">直流 (A)</td></tr>
<tr><td>25℃</td><td>35℃</td><td>25℃</td><td>35℃</td></tr>
<tr><td>50×6</td><td>955</td><td>840</td><td>960</td><td>845</td></tr>
<tr><td>63×6</td><td>1125</td><td>990</td><td>1145</td><td>1010</td></tr>
</table>`
	result := &types.SearchResult{ID: "busbar-row", KnowledgeID: "busbar", KnowledgeBaseID: "manuals",
		KnowledgeTitle: "busbar.pdf", Content: table}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: "单铜排 50×6，25℃ 直流载流量"},
		PipelineState: types.PipelineState{
			UserContent: question,
			History:     []*types.History{{Query: "50×6 的单铜排，25℃ 交流能过多少安？", Answer: "955 A"}},
			MergeResult: []*types.SearchResult{result},
		},
	}
	messages, registry := prepareMessagesWithModelContext(context.Background(), manage)
	prompt := messages[len(messages)-1].Content
	require.Contains(t, prompt, question)
	require.Equal(t, "955 A", messages[len(messages)-2].Content)
	require.Contains(t, prompt, "铜排尺寸 = 50×6 | 交流 (A) / 25℃ = 955 | 交流 (A) / 35℃ = 840 | 直流 (A) / 25℃ = 960")
	require.Contains(t, prompt, "直流 (A) / 35℃ = 1010")
	require.Contains(t, messages[0].Content, "在同一证据行内按完整列路径读数")
	require.Contains(t, registry.DecodeOutputText(`<ref id="1"/>`), `chunk_id="busbar-row"`)
	require.Equal(t, table, result.Content)
	require.NotContains(t, prompt, "直流 (A) / 25℃ = 1010")
}
