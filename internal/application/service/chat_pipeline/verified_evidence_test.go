package chatpipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

const scratchHTML = `<tr><td rowspan="4">浅划痕</td><td rowspan="4">划伤的痕迹</td><td>A级面</td>
<td colspan="2">L≤10mm，n≤2pcs，DS≥100mm</td><td rowspan="4">目视</td><td rowspan="4">MI</td></tr>
<tr><td>B级面</td><td colspan="2">L≤15mm，n≤3pcs，DS≥100mm</td></tr>
<tr><td>C级面</td><td>L≤30mm，n≤5pcs</td></tr><tr><td>D级面</td><td>可接受</td></tr>
<tr><td>拉模发白</td><td>A级面</td><td>不允许</td><td>MA</td></tr>`

func verifiedPassage(id, title, body string) *types.SearchResult {
	return &types.SearchResult{
		ID: id, KnowledgeID: title + "-doc", KnowledgeBaseID: "test-kb",
		KnowledgeTitle: title + ".pdf", Content: body, ChunkType: string(types.ChunkTypeText),
	}
}

func verifiedRequest(query string, passages ...*types.SearchResult) *types.ChatManage {
	return &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: query, SessionID: "verified-session"},
		PipelineState:   types.PipelineState{UserContent: query, MergeResult: passages},
	}
}

func TestVerifiedScratchUsesTheCompleteCitedRow(t *testing.T) {
	source := verifiedPassage("scratch-html", "塑壳产品检验规范", scratchHTML)
	chat := verifiedRequest("塑壳 A、B 级面的浅划痕允许长度和数量分别是多少？", source)
	answer, ok := verifiedEvidenceReply(chat)
	require.True(t, ok)
	for _, expected := range []string{"A级面", "B级面", "≤ 10 mm", "≤ 15 mm", "≤ 2 条", "≤ 3 条", "**MI**", `chunk_id="scratch-html"`} {
		require.Contains(t, answer, expected)
	}
	require.Equal(t, 2, strings.Count(answer, "DS ≥ 100 mm"))
	require.NotContains(t, answer, "**MA**")

	chat.Query = "塑壳 A 级面有 2 条浅划痕，每条长 9 mm，两条相距 90 mm，能通过吗？"
	answer, ok = verifiedEvidenceReply(chat)
	require.True(t, ok)
	require.Contains(t, answer, "不符合本条允收条件")
	require.Contains(t, answer, "间距 90 mm 小于 100 mm")
	chat.Query = strings.ReplaceAll(chat.Query, "90 mm", "100 mm")
	answer, ok = verifiedEvidenceReply(chat)
	require.True(t, ok)
	require.Contains(t, answer, "均满足")
}

func TestVerifiedScratchWorksWithLegacyTextAndOptionalCitations(t *testing.T) {
	source := verifiedPassage("scratch-text", "塑壳产品检验规范", `浅划痕
划伤的痕迹
A 级面 L≤12mm，n≤3pcs，DS≥110mm
目视 MI
B 级面 L≤16mm，n≤4pcs，DS≥120mm`)
	chat := verifiedRequest("塑壳 B 级面浅划痕的限制条件是什么？允许长度和间距是多少？", source)
	disabled := false
	chat.CitationEnabled = &disabled
	answer, ok := verifiedEvidenceReply(chat)
	require.True(t, ok)
	require.Contains(t, answer, "≤ 16 mm")
	require.Contains(t, answer, "≥ 120 mm")
	require.NotContains(t, answer, "110 mm")
	require.NotContains(t, answer, "<kb")
}

func TestVerifiedReplyUsesCurrentSourceAndNormalStream(t *testing.T) {
	source := verifiedPassage("scratch", "塑壳产品检验规范", scratchHTML)
	chat := verifiedRequest("塑壳 A 级面浅划痕允许长度？", source)
	chat.UserContent = "## Reference materials\n相邻缺陷：深划痕和拉模。\n" + chat.Query
	bus := &syncEventBus{}
	chat.EventBus = bus
	followed := false
	next := func() *PluginError {
		followed = true
		return nil
	}
	require.Nil(t, (&PluginChatCompletionStream{}).OnEvent(context.Background(), types.CHAT_COMPLETION_STREAM, chat, next))
	require.True(t, followed)
	require.Eventually(t, func() bool {
		events := bus.finalAnswerEvents()
		return len(events) == 2 && events[1].Done &&
			strings.Contains(events[0].Content, "DS ≥ 100 mm") &&
			strings.Contains(events[0].Content, `chunk_id="scratch"`)
	}, time.Second, 5*time.Millisecond)
	require.Nil(t, (&PluginChatCompletion{}).OnEvent(context.Background(), types.CHAT_COMPLETION, chat, next))
	require.Contains(t, chat.ChatResponse.Content, "DS ≥ 100 mm")

	original := chat.Query
	chat.Query = "为检验员写一段开场白。"
	_, ok := verifiedEvidenceReply(chat)
	require.False(t, ok, "context text does not select a deterministic reply")
	chat.Query = original
	source.ContentRewritten = true
	_, ok = verifiedEvidenceReply(chat)
	require.False(t, ok, "expanded content with no verified citation source is ineligible")
	source.ContentRewritten = false
	another := *source
	another.ID, another.KnowledgeID = "other", "other-doc"
	chat.MergeResult = append(chat.MergeResult, &another)
	_, ok = verifiedEvidenceReply(chat)
	require.False(t, ok, "a reply must not silently combine document versions")
}

func TestVerifiedCableMappingsUseSeparateColumnGroups(t *testing.T) {
	header := "原表列标题（从左到右）：\n美标（跨2列） | 国标（跨2列）\nAWG | 线经描述 | mm² | 线经描述"
	oldText := "美标 国标\nAWG 线经描述 mm² 线经描述\n24AWG 24A 0.75mm² R75\n20AWG 20A 1.5mm² 1R5"
	newText := "<tr><td>24AWG</td><td>24A</td><td>0.75mm²</td><td>R75</td></tr>\n" +
		"<tr><td>20AWG</td><td>20A</td><td>1.5mm²</td><td>1R5</td></tr>"
	query := "线缆编码表把 24 AWG 和 0.75 mm² 放在一行，表示等价或可替代吗？20 AWG 和 1.5 mm² 各是什么编码？"
	for _, source := range []*types.SearchResult{
		verifiedPassage("old", "线缆及端子规格描述规范", oldText),
		verifiedPassage("new", "线缆及端子规格描述规范", newText),
	} {
		if source.ID == "new" {
			source.ContextHeader = header
		}
		reply, ok := verifiedEvidenceReply(verifiedRequest(query, source))
		require.True(t, ok)
		require.Contains(t, reply, "24 AWG | 美标 | **24A**")
		require.Contains(t, reply, "0.75 mm² | 国标 | **R75**")
		require.Contains(t, reply, "20 AWG | 美标 | **20A**")
		require.Contains(t, reply, "1.5 mm² | 国标 | **1R5**")
		require.Contains(t, reply, "换算、等效和替代关系需要可引用的独立依据")
	}
	withoutHeader := verifiedPassage("current", "线缆及端子规格描述规范", newText)
	_, ok := verifiedEvidenceReply(verifiedRequest(query, withoutHeader))
	require.False(t, ok)
}

func TestVerifiedAWGBoundaryKeepsTheIndependentRulePending(t *testing.T) {
	source := verifiedPassage("wire-clause", "线缆及端子规格描述规范", "2.4 单根线命名分为电子线与电缆线，规定≤6mm²/10AWG 的命名为电子线，＞6mm²/10AWG的为电缆线。")
	answer, ok := verifiedEvidenceReply(verifiedRequest("恰好 10 AWG 的单根线是电子线还是电缆线，如何归类？", source))
	require.True(t, ok)
	require.Contains(t, answer, "≤ 6 mm²")
	require.Contains(t, answer, "> 6 mm²")
	require.Contains(t, answer, "恰好 10 AWG 的归类待确认")
	require.Contains(t, answer, `chunk_id="wire-clause"`)
}

func TestVerifiedHeatShrinkReportsBothConflictingSources(t *testing.T) {
	title := "热缩套管选型规范"
	table := verifiedPassage("table", title, "最小方铜排的高+宽＜18mm 热缩套管：600V（套两层），125℃。最小方铜排的高+宽≥18mm 热缩套管：1kV")
	diagram := verifiedPassage("diagram", title, "热缩管选型 母排额定电压：480V~1140V\n2.母排额定电压：1kV~10kV\n当最小方铜排的高+宽≤18mm 时：\n当最小方铜排的高+宽≥18mm 时：\n热缩套管：600V（套两层），125℃。热缩套管：1kV，125℃")
	query := "母排额定电压为 480～1140 V，最小方铜排的高加宽正好 18 mm，热缩套管选哪种？"
	answer, ok := verifiedEvidenceReply(verifiedRequest(query, table, diagram))
	require.True(t, ok)
	require.Contains(t, answer, "< 18 mm 对应 600 V")
	require.Contains(t, answer, "≤ 18 mm 对应 600 V")
	require.Contains(t, answer, "≥ 18 mm 对应 1 kV")
	require.Contains(t, answer, "恰好 18 mm 的选型待确认")
	require.Contains(t, answer, `chunk_id="table"`)
	require.Contains(t, answer, `chunk_id="diagram"`)
	reply, ok := verifiedEvidenceReply(verifiedRequest(query, table))
	require.True(t, ok)
	require.Contains(t, reply, "恰好 18 mm 的选型待确认")
	require.NotContains(t, reply, "≤ 18 mm 对应 600 V")

	_, ok = verifiedEvidenceReply(verifiedRequest(strings.ReplaceAll(query, "480～1140 V", "1～10 kV"), table, diagram))
	require.False(t, ok)
	diagram.Content = strings.ReplaceAll(diagram.Content, "≤18mm", "＜18mm")
	reply, ok = verifiedEvidenceReply(verifiedRequest(query, table, diagram))
	require.True(t, ok)
	require.Contains(t, reply, "图表一致")
	require.NotContains(t, reply, "待确认")
}

func TestVerifiedCoatingConflictKeepsTheSourceValues(t *testing.T) {
	title := "伟创电气电镀工艺技术标准"
	design := verifiedPassage("design", title, "钢铁 环保彩锌 8μm 以上 GB/T 9799-Fe/Zn8/HC。Fe/Zn8/HC 为镀环保彩锌，最小厚度要求为 5µm。")
	production := verifiedPassage("production", title, "钢铁镀锌，目前公司规定过 72 小时的盐雾中性试验标准，镀环保彩锌，厚度为 8μm 以上。")
	answer, ok := verifiedEvidenceReply(verifiedRequest("Fe/Zn8/HC 的厚度冲突怎么解释？", design, production))
	require.True(t, ok)
	require.Contains(t, answer, "8 μm 以上")
	require.Contains(t, answer, "最小厚度为 5 μm")
	require.Contains(t, answer, "验收厚度待确认")
	require.Contains(t, answer, `chunk_id="design"`)
	require.Contains(t, answer, `chunk_id="production"`)
}
