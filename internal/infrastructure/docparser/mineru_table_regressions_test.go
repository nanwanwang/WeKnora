package docparser

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestN280MinerUCorrectionRecoversSolderingMaximum(t *testing.T) {
	pdf := []byte("synthetic soldering limit PDF")
	const broken = "<table><tr><td>C°</td><td>003</td><td>s 0 1 , gn ir edloS</td><td>er utar ep met daeL</td></tr></table>"
	const peak = "Package peak temperature: 260 °C"
	const corrected = `<table><thead>
<tr><th>Parameter</th><th>Conditions</th><th>MIN</th><th>MAX</th><th>UNIT</th></tr>
</thead><tbody>
<tr><td>Lead temperature</td><td>Soldering, 10 s</td><td></td><td>300</td><td>°C</td></tr>
</tbody></table>`
	box := []float64{0.1, 0.2, 0.9, 0.5}
	writeMinerUTableCorrections(t, pdf, minerUTableCorrection{
		Page: 4, MatchText: []string{"erutarepmetdael"}, HTML: corrected, BBox: box,
	})

	layout := marshalCorrectionFixture(t, []map[string]any{
		{"type": "table", "table_body": broken, "page_idx": 3},
		{"type": "text", "text": peak, "page_idx": 13},
	})
	result, err := finishMinerURead(&types.ReadRequest{FileType: "pdf", FileContent: pdf}, broken+"\n\n"+peak, nil, layout)
	require.NoError(t, err)
	require.NotContains(t, result.MarkdownContent, "003")
	require.Contains(t, result.MarkdownContent, peak)
	row := correctedPDFTable(t, result, 4, "mineru:table-correction:4:1", box)
	require.Contains(t, row, "<th>MIN</th><th>MAX</th><th>UNIT</th>")
	require.Contains(t, row, "<td>Lead temperature</td><td>Soldering, 10 s</td><td></td><td>300</td><td>°C</td>")
	require.NotContains(t, row, "260")
}

func TestN101MinerUCorrectionKeepsBothPEColumns(t *testing.T) {
	pdf := []byte("synthetic voltage table PDF")
	const broken = "| Rated V | HV AC | voltage PE DC | V HV AC | LV DC |\n" +
		"| --- | --- | --- | --- | --- |\n" +
		"| 1140 | 3280 | 4640 | 5410 | 7650 |\n"
	const corrected = `<table><thead>
<tr><th>Rated V</th><th>Strong-PE AC (V)</th><th>Strong-PE DC (V)</th><th>Strong-Low AC (V)</th><th>Strong-Low DC (V)</th></tr>
</thead><tbody>
<tr><td>1140</td><td>3280</td><td>4640</td><td>5410</td><td>7650</td></tr>
</tbody></table>`
	const fence = "\x60\x60\x60"
	prefix := "说明\n\n" + fence + "md\n" + broken + fence + "\n\n"
	markdown := prefix + broken + "\n" + broken + "\n| next | section |\n"
	earlier := utf8.RuneCountInString(prefix)
	target := earlier + utf8.RuneCountInString(broken+"\n")
	box := []float64{0.1, 0.2, 0.9, 0.5}
	writeMinerUTableCorrections(t, pdf, minerUTableCorrection{
		Page: 9, MatchText: []string{"1140 3280 4640 5410 7650"}, HTML: corrected, BBox: box,
	})
	result := &types.ReadResult{MarkdownContent: markdown, SourceBlocks: []types.SourceBlock{
		{Start: earlier, End: target, Locator: types.SourceLocator{Type: types.SourceLocatorPDF, Page: 3}},
		{Start: target, End: target + utf8.RuneCountInString(broken), Locator: types.SourceLocator{Type: types.SourceLocatorPDF, Page: 9}},
	}}

	require.NoError(t, applyMinerUPDFTableCorrections(&types.ReadRequest{FileType: "pdf", FileContent: pdf}, result))
	require.Equal(t, 2, strings.Count(result.MarkdownContent, broken))
	require.Contains(t, result.MarkdownContent, "| next | section |")
	table := correctedPDFTable(t, result, 9, "mineru:table-correction:9:1", box)
	require.Contains(t, table, "<th>Strong-PE AC (V)</th><th>Strong-PE DC (V)</th>")
	require.Contains(t, table, "<td>1140</td><td>3280</td><td>4640</td><td>5410</td><td>7650</td>")
}

func correctedPDFTable(t *testing.T, result *types.ReadResult, page int, sourceID string, box []float64) string {
	t.Helper()

	for _, block := range result.SourceBlocks {
		if block.Locator.SourceID != sourceID {
			continue
		}
		require.Equal(t, page, block.Locator.Page)
		require.Equal(t, box, block.Locator.BBox)
		return string([]rune(result.MarkdownContent)[block.Start:block.End])
	}
	t.Fatalf("corrected PDF table source %q is missing", sourceID)
	return ""
}
