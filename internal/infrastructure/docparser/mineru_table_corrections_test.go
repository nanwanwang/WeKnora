package docparser

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func marshalCorrectionFixture(t *testing.T, fixture any) []byte {
	t.Helper()

	data, err := json.Marshal(fixture)
	require.NoError(t, err)
	return data
}

const (
	correctionHeader = `<table><thead>
<tr><th rowspan="2">PARAMETER</th><th rowspan="2">TEST CONDITIONS</th>
<th colspan="3">TL284xB</th><th colspan="3">TL384xB</th><th rowspan="2">UNIT</th></tr>
<tr><th>MIN</th><th>TYP</th><th>MAX</th><th>MIN</th><th>TYP</th><th>MAX</th></tr></thead><tbody>
`
	reversedOscillator = `<table><tr><td>tnerr uc egrahcsiD</td><td>8.8 3.8 8.7</td></tr></table>`
	reversedReference  = `<table><tr><td>noitairav esac-tsrow</td><td>81.5 28.4</td></tr></table>`
)

func writeMinerUTableCorrections(t *testing.T, pdf []byte, corrections ...minerUTableCorrection) string {
	t.Helper()

	directory := t.TempDir()
	path := filepath.Join(directory, fmt.Sprintf("%x.json", sha256.Sum256(pdf)))
	require.NoError(t, os.WriteFile(path, marshalCorrectionFixture(t, corrections), 0o600))
	t.Setenv(minerUTableCorrectionsEnv, directory)
	return path
}

func TestMinerUPDFTableCorrectionsTIRegressions(t *testing.T) {
	pdf := []byte("synthetic PDF revision for TI table regression")
	oscBox, refBox := []float64{0.1, 0.4, 0.9, 0.6}, []float64{0.1, 0.1, 0.9, 0.3}
	writeMinerUTableCorrections(t, pdf,
		minerUTableCorrection{Page: 5, MatchText: []string{"discharge current", "tnerrucegrahcsid"}, BBox: oscBox,
			HTML: correctionHeader +
				"<tr><td>Discharge current</td><td>TJ = 25°C, RT/CT = 2 V</td>" +
				"<td>7.8</td><td>8.3</td><td>8.8</td><td>7.8</td><td>8.3</td><td>8.8</td><td>mA</td></tr>\n" +
				"<tr><td>Discharge current</td><td>RT/CT = 2 V</td>" +
				"<td>7.5</td><td></td><td>8.8</td><td>7.6</td><td></td><td>8.8</td><td>mA</td></tr>\n</tbody></table>"},
		minerUTableCorrection{Page: 5, MatchText: []string{"noitairavesactsrow"}, BBox: refBox,
			HTML: correctionHeader +
				"<tr><td>Output voltage</td><td>IO = 1 mA, TJ = 25°C</td>" +
				"<td>4.95</td><td>5</td><td>5.05</td><td>4.9</td><td>5</td><td>5.1</td><td>V</td></tr>\n" +
				"<tr><td>Output voltage, worst-case variation</td><td>VCC = 12 V to 25 V, IO = 1 mA to 20 mA</td>" +
				"<td>4.9</td><td></td><td>5.1</td><td>4.82</td><td></td><td>5.18</td><td>V</td></tr>\n</tbody></table>"})
	const prefix, suffix = "手册前文\n\n", "\n\n下一页内容"
	// The reference table is broken badly enough that MinerU missed its
	// locator. The manifest supplies the verified source location for it.
	markdown := prefix + reversedReference + "\n\n" + reversedOscillator + suffix
	layout := marshalCorrectionFixture(t, []map[string]any{
		{"type": "text", "text": strings.TrimSpace(prefix), "page_idx": 3},
		{"type": "table", "table_body": reversedOscillator, "page_idx": 4},
		{"type": "text", "text": strings.TrimSpace(suffix), "page_idx": 5},
	})
	req := &types.ReadRequest{FileType: "pdf", FileContent: pdf}
	result, err := finishMinerURead(req, markdown, nil, layout)
	require.NoError(t, err)
	require.NotContains(t, result.MarkdownContent, "8.8 3.8 8.7")
	require.NotContains(t, result.MarkdownContent, "81.5 28.4")
	content := []rune(result.MarkdownContent)
	byID := make(map[string]types.SourceBlock, len(result.SourceBlocks))
	for _, source := range result.SourceBlocks {
		byID[source.Locator.SourceID] = source
	}
	first := byID["mineru:legacy:3:0"]
	assert.Equal(t, 4, first.Locator.Page)
	assert.Equal(t, strings.TrimSpace(prefix), strings.TrimSpace(string(content[first.Start:first.End])))
	last := byID["mineru:legacy:5:2"]
	assert.Equal(t, 6, last.Locator.Page)
	assert.Equal(t, strings.TrimSpace(suffix), string(content[last.Start:last.End]))

	specs := []struct {
		name, parameter, condition, unit string
		correction                       int
		box                              []float64
		values                           []string
	}{
		{
			"N278", "Discharge current", "TJ = 25°C, RT/CT = 2 V", "mA",
			1, oscBox, []string{"7.8", "8.3", "8.8", "7.8", "8.3", "8.8"},
		},
		{
			"N278 adjacent RT/CT row", "Discharge current", "RT/CT = 2 V", "mA",
			1, oscBox, []string{"7.5", "", "8.8", "7.6", "", "8.8"},
		},
		{
			"N281", "Output voltage, worst-case variation", "VCC = 12 V to 25 V, IO = 1 mA to 20 mA", "V",
			2, refBox, []string{"4.9", "", "5.1", "4.82", "", "5.18"},
		},
		{
			"N281 adjacent nominal row", "Output voltage", "IO = 1 mA, TJ = 25°C", "V",
			2, refBox, []string{"4.95", "5", "5.05", "4.9", "5", "5.1"},
		},
	}
	for _, spec := range specs {
		t.Run(spec.name, func(t *testing.T) {
			source := byID[fmt.Sprintf("mineru:table-correction:5:%d", spec.correction)]
			require.Equal(t, 5, source.Locator.Page)
			require.Equal(t, spec.box, source.Locator.BBox)
			table := string(content[source.Start:source.End])
			require.Contains(t, table, strings.TrimSpace(correctionHeader))
			cells := append([]string{spec.parameter, spec.condition}, spec.values...)
			cells = append(cells, spec.unit)
			require.Contains(t, table, "<tr><td>"+strings.Join(cells, "</td><td>")+"</td></tr>")
		})
	}
}

func TestMinerUPDFTableCorrectionsRequireExactScope(t *testing.T) {
	pdf := []byte("expected revision")
	path := writeMinerUTableCorrections(t, pdf, minerUTableCorrection{
		Page: 5, BBox: []float64{0.1, 0.4, 0.9, 0.6},
		MatchText: []string{"tnerrucegrahcsid"}, HTML: "<table><tr><td>Discharge current</td></tr></table>",
	})
	req := &types.ReadRequest{FileType: ".PDF", FileContent: pdf}
	wrongPage := marshalCorrectionFixture(t, []map[string]any{
		{"type": "table", "table_body": reversedOscillator, "page_idx": 5},
	})
	const fence = "\x60\x60\x60"
	for _, test := range []struct {
		markdown string
		layout   []byte
		message  string
	}{
		{reversedOscillator, wrongPage, "no table with the expected markers and page"},
		{reversedOscillator + "\n\n" + reversedOscillator, nil, "match multiple tables"},
		{"unrelated content", nil, "no table with the expected markers and page"},
		{fence + "html\n" + reversedOscillator + "\n" + fence, nil, "no table with the expected markers and page"},
	} {
		got, err := finishMinerURead(req, test.markdown, nil, test.layout)
		require.ErrorContains(t, err, test.message)
		assert.Nil(t, got)
	}
	require.NoError(t, os.WriteFile(path, []byte("{invalid"), 0o600))
	_, err := finishMinerURead(req, reversedOscillator, nil, nil)
	require.ErrorContains(t, err, "decode MinerU PDF table corrections")
	for _, other := range []*types.ReadRequest{
		{FileType: "pdf", FileContent: []byte("different revision")},
		{FileType: "docx", FileContent: pdf},
		{FileType: "xlsx", FileContent: pdf},
	} {
		got, err := finishMinerURead(other, reversedOscillator, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, reversedOscillator, got.MarkdownContent)
	}
	t.Setenv(minerUTableCorrectionsEnv, "")
	got, err := finishMinerURead(req, reversedOscillator, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, reversedOscillator, got.MarkdownContent)
}

func TestMinerUPDFTableCorrectionHTML(t *testing.T) {
	for _, markup := range []string{
		"<p>outside the table</p><table></table>",
		"<table><tr><td><img src=x></td></tr></table>",
		"<table><tr><td onclick='x'>data</td></tr></table>",
		"<table><tr><td colspan='0'>data</td></tr></table>",
		"<table><tr><td><table><tr><td>nested</td></tr></table></td></tr></table>",
	} {
		_, err := validateCorrectionTable(markup)
		require.Error(t, err, markup)
	}
}
