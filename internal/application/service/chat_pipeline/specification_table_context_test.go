package chatpipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

const recommendedFrequency = `<tr><td colspan="2">Oscillator Frequency</td><td>fosc</td>` +
	`<td>--</td><td>100</td><td>500</td><td>kHz</td></tr>`

const initialFrequency = `<tr><td colspan="7">Oscillator Section</td></tr>
<tr><td>Initial Accurcy</td><td>f</td><td>Ta=25°C</td><td>49</td><td>52</td><td>55</td><td>kHz</td></tr>`

const voltageStability = `<tr><td>Voltage stability</td><td>∆f/∆VCC</td><td>VCC=12V~25V</td>` +
	`<td>--</td><td>0.2</td><td>1</td><td>%</td></tr>`

const mergedOscillatorTables = `RECOMMENDED OPERATING CONDITION
<table><tbody>
` + `<tr><td colspan="2">Characteristics</td><td>Symbol</td>` +
	`<td>Min.</td><td>Typ.</td><td>Max.</td><td>Unit</td></tr>` + `
` + recommendedFrequency + `
</tbody></table>

ELECTRICAL CHARACTERISTICS
<table><tbody>
` + `<tr><td>Characteristics</td><td>Symbol</td><td>Test condition</td>` +
	`<td>Min.</td><td>Typ.</td><td>Max.</td><td>Unit</td></tr>` + `
` + initialFrequency + "\n" + voltageStability

func TestSplitSpecificationRowsReachModelWithTheirOwnTableContexts(t *testing.T) {
	const document = "455ee6c2-5d84-4fbf-b919-d4012f82339c"
	const library = "4bb3dee1-638f-4b1c-81e7-6848a7e0d976"
	candidate := func(id, text string, index int) *types.Chunk {
		return &types.Chunk{
			ID: id, KnowledgeID: document, KnowledgeBaseID: library, ParentChunkID: "parent",
			IsEnabled: true, ChunkType: types.ChunkTypeText, Content: text, ChunkIndex: index,
		}
	}
	recommended := candidate("331da8b6-2825-4ed2-9aa9-72628055486f", recommendedFrequency, 1)
	accuracy := candidate("98420a87-7950-4a29-aa32-46b68e03912e", initialFrequency, 2)
	stability := candidate("279016fe-ab28-44f8-ae34-43315801687c", voltageStability, 3)
	repo := &expandChunkRepo{
		chunks:   map[string]*types.Chunk{recommended.ID: recommended},
		children: map[string][]*types.Chunk{"parent": {recommended, accuracy, stability}},
	}
	merger := &PluginMerge{chunkRepo: repo}
	merged := &types.SearchResult{
		ID: recommended.ID, KnowledgeID: document, KnowledgeBaseID: library,
		ContentRewritten: true, Content: mergedOscillatorTables,
	}
	merger.attachCitationSources(context.Background(), []*types.SearchResult{merged})
	evidence := expandCitationSources([]*types.SearchResult{merged})
	require.Len(t, evidence, 3)
	for i, expected := range []struct {
		chunk   *types.Chunk
		section string
		row     string
	}{
		{recommended, "RECOMMENDED OPERATING CONDITION", "<td>--</td><td>100</td><td>500</td><td>kHz</td>"},
		{accuracy, "ELECTRICAL CHARACTERISTICS", "<td>Ta=25°C</td><td>49</td><td>52</td><td>55</td><td>kHz</td>"},
		{stability, "ELECTRICAL CHARACTERISTICS", "<td>VCC=12V~25V</td><td>--</td><td>0.2</td><td>1</td><td>%</td>"},
	} {
		require.Equal(t, expected.chunk.ID, evidence[i].ID)
		require.Equal(t, expected.chunk.Content, evidence[i].Content)
		require.Contains(t, evidence[i].Content, expected.row)
		require.Contains(t, evidence[i].ContextHeader, "原表章节："+expected.section+"\n")
		require.Contains(t, evidence[i].ContextHeader, "Min. | Typ. | Max. | Unit")
		require.Empty(t, expected.chunk.ContextHeader)
	}
	const query = "振荡器初始精度的范围和典型值是多少？"
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: query, SummaryConfig: types.SummaryConfig{Prompt: "system"}},
		PipelineState:   types.PipelineState{UserContent: query, MergeResult: []*types.SearchResult{merged}},
	}
	messages, _ := prepareMessagesWithModelContext(context.Background(), manage)
	require.NotEmpty(t, messages)
	require.Contains(t, messages[0].Content, "先核对表节、完整参数名和测试条件")
	require.Contains(t, messages[0].Content, "其次采用明确覆盖该行的本表条件")
	prompt := messages[len(messages)-1].Content
	require.Contains(t, prompt, "RECOMMENDED OPERATING CONDITION")
	require.Contains(t, prompt, "ELECTRICAL CHARACTERISTICS")
	require.Contains(t, prompt, "Initial Accurcy")
	require.Contains(t, prompt, "Voltage stability")
}

func TestSpecificationTableContextDoesNotBorrowUnrelatedHeaders(t *testing.T) {
	require.Empty(t, chunker.SpecificationTableContext(mergedOscillatorTables+"\n"+initialFrequency, initialFrequency))
	require.Empty(t, chunker.SpecificationTableContext(
		strings.ReplaceAll(mergedOscillatorTables, "<td>Min.</td>", "<td>Label</td>"), initialFrequency))
	require.Empty(t, chunker.SpecificationTableContext(
		mergedOscillatorTables+"</tbody></table>\n"+recommendedFrequency,
		recommendedFrequency+"\n<not-in-source>"))
	require.Empty(t, chunker.SpecificationTableContext(
		"<table><tr><th>Min</th><th>Typ</th><th>Max</th></tr></table>\n"+voltageStability, voltageStability))
}
