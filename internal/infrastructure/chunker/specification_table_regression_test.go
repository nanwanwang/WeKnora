package chunker

import (
	"strings"
	"testing"
)

const busbarTableHeader = `<table><tbody>
<tr><td rowspan="4">母排尺寸长×厚（mm）</td><td colspan="8">铜</td></tr>
<tr><td colspan="4">交 流</td><td colspan="4">直 流</td></tr>
<tr><td colspan="8">不同环境温度的载流量(A)</td></tr>
<tr><td>25℃</td><td>30℃</td><td>35 ℃</td><td>40℃</td><td>25℃</td><td>30℃</td><td>35℃</td><td>40℃</td></tr>
`

const busbarTableRows = `<tr><td>15×3</td><td>210</td><td>197</td><td>185</td><td>170</td><td>210</td><td>197</td><td>185</td><td>170</td></tr>
<tr><td>20×3</td><td>275</td><td>258</td><td>242</td><td>223</td><td>275</td><td>258</td><td>242</td><td>223</td></tr>
<tr><td>50×6</td><td>955</td><td>898</td><td>840</td><td>774</td><td>960</td><td>902</td><td>845</td><td>778</td></tr>
<tr><td>63×6</td><td>1125</td><td>1056</td><td>990</td><td>912</td><td>1145</td><td>1079</td><td>1010</td><td>928</td></tr>
<tr><td>80×6</td><td>1480</td><td>1390</td><td>1300</td><td>1200</td><td>1510</td><td>1420</td><td>1330</td><td>1225</td></tr>`

func TestN273TableColumnEvidenceAfterSplit(t *testing.T) {
	source := busbarTableHeader + busbarTableRows + "\n</tbody></table>"
	cfg := SplitterConfig{ChunkSize: 330, ChunkOverlap: 0, Strategy: StrategyAuto}
	result := SplitParentChild(source, SplitterConfig{ChunkSize: 1500}, cfg)

	for _, chunk := range result.Children {
		if !strings.Contains(chunk.Content, "<tr><td>50×6") || strings.Contains(chunk.Content, "<table>") {
			continue
		}
		evidence := PresentTableEvidence(chunk.Content, chunk.ContextHeader)
		assertTableEvidenceRow(t, evidence, "母排尺寸长×厚（mm） = 50×6", []string{
			"交 流 / 不同环境温度的载流量(A) / 25℃ = 955",
			"直 流 / 不同环境温度的载流量(A) / 25℃ = 960",
		}, []string{"1010", "1145"})
		if !strings.Contains(chunk.Content, "<td>960</td>") {
			t.Fatal("the cited child must contain the original value")
		}
		return
	}
	t.Fatal("no body-only child chunk containing the 50×6 row")
}

const specificationHeader = `<table><thead>
<tr><th rowspan="2">PARAMETER</th><th rowspan="2">TEST CONDITIONS</th><th colspan="3">TL284xB</th><th colspan="3">TL384xB</th><th rowspan="2">UNIT</th></tr>
<tr><th>MIN</th><th>TYP</th><th>MAX</th><th>MIN</th><th>TYP</th><th>MAX</th></tr></thead><tbody>
`

func TestN278DischargeCurrentKeepsConditionAndTypicalColumn(t *testing.T) {
	source := specificationHeader + `<tr><td rowspan="2">Discharge current</td><td>TJ = 25°C, RT/CT = 2 V</td><td>7.8</td><td>8.3</td><td>8.8</td><td>7.8</td><td>8.3</td><td>8.8</td><td rowspan="2">mA</td></tr>
<tr><td>RT/CT = 2 V</td><td>7.5</td><td></td><td>8.8</td><td>7.6</td><td></td><td>8.8</td></tr>
</tbody></table>`
	evidence := PresentTableEvidence(source, "")
	assertTableEvidenceRow(t, evidence, "TEST CONDITIONS = TJ = 25°C, RT/CT = 2 V", []string{
		"PARAMETER = Discharge current", "TL284xB / MIN = 7.8", "TL284xB / TYP = 8.3",
		"TL284xB / MAX = 8.8", "UNIT = mA",
	}, []string{"TL284xB / TYP = 8.8", "7.5"})
	assertTableEvidenceRow(t, evidence, "TEST CONDITIONS = RT/CT = 2 V", []string{
		"PARAMETER = Discharge current", "TL284xB / MIN = 7.5", "TL284xB / MAX = 8.8", "UNIT = mA",
	}, []string{"TL284xB / TYP = 8.8", "8.3"})
}

func TestN281WorstCaseKeepsSeparateModelRanges(t *testing.T) {
	source := specificationHeader + `<tr><td>Output voltage</td><td>IO = 1 mA, TJ = 25°C</td><td>4.95</td><td>5</td><td>5.05</td><td>4.9</td><td>5</td><td>5.1</td><td>V</td></tr>
<tr><td>Output voltage, worst-case variation</td><td>VCC = 12 V to 25 V, IO = 1 mA to 20 mA</td><td>4.9</td><td></td><td>5.1</td><td>4.82</td><td></td><td>5.18</td><td>V</td></tr>
</tbody></table>`
	evidence := PresentTableEvidence(source, "")
	assertTableEvidenceRow(t, evidence, "PARAMETER = Output voltage, worst-case variation", []string{
		"TEST CONDITIONS = VCC = 12 V to 25 V, IO = 1 mA to 20 mA",
		"TL284xB / MIN = 4.9", "TL284xB / MAX = 5.1", "TL384xB / MIN = 4.82", "TL384xB / MAX = 5.18", "UNIT = V",
	}, []string{"4.95", "5.05", "TYP = 5"})
}

func TestQualifiedTableLeavesUnalignedBodyUnlabelled(t *testing.T) {
	source := specificationHeader + `<tr><td>Spec</td><td>Test 1</td><td>1</td><td>2</td><td>3</td><td>4</td><td>5</td><td>6</td><td>V</td></tr>
<tr><td>Spec</td><td>Test 2</td><td>7</td><td>8</td></tr>
</tbody></table>`
	evidence := PresentTableEvidence(source, "")
	if strings.Contains(evidence, "TL284xB / MIN = ") {
		t.Fatalf("ambiguous table must not fabricate a column mapping: %s", evidence)
	}
}

func assertTableEvidenceRow(t *testing.T, evidence, key string, want, unwanted []string) {
	t.Helper()
	for _, line := range strings.Split(evidence, "\n") {
		if !strings.Contains(line, key) {
			continue
		}
		for _, part := range want {
			if !strings.Contains(line, part) {
				t.Errorf("missing %q in row %q", part, line)
			}
		}
		for _, part := range unwanted {
			if strings.Contains(line, part) {
				t.Errorf("unexpected %q in row %q", part, line)
			}
		}
		return
	}
	t.Fatalf("missing row %q in evidence:\n%s", key, evidence)
}
