package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestInferredAcceptanceDifferenceRequiresSameIdentifierAndLimit(t *testing.T) {
	first := reviewSource{document: "original", ref: "source-one",
		body: "AA-1/B7/CT 表示钢件钝化，镀层最小厚度为 7 μm，种类为蓝白镀层。"}
	second := reviewSource{document: "original", ref: "source-two",
		body: "AA-1/B7/CT 为蓝白镀层，最小厚度要求为 4 μ m。"}
	question := "钢件蓝白镀层的最小厚度到底按多少验收？"
	proof := inferredAcceptanceDifference(question, []reviewSource{first, second})
	require.Len(t, proof.excerpts, 2)
	answer := renderEvidenceDifference(proof, true)
	for _, want := range []string{"7 μm", "4 μm", "归口部门确认", "source-one", "source-two"} {
		require.Contains(t, answer, want)
	}

	second.document = "different-record"
	require.Empty(t, inferredAcceptanceDifference(question, []reviewSource{first, second}).excerpts)
	require.Empty(t, inferredAcceptanceDifference("蓝白镀层的用途是什么？", []reviewSource{first}).excerpts)
}

func TestVerifiedTorqueRequiresOneTableRegionAndKeepsConditions(t *testing.T) {
	location := func(id string) types.SourceLocators {
		return types.SourceLocators{{Type: types.SourceLocatorPDF, Page: 5, SourceID: id}}
	}
	sources := []reviewSource{
		{document: "doc", title: "母排规范.pdf", ref: "scope", locators: location("scope"),
			body: "Gleichstrom und für Wechselstrom bis 70 Hz."},
		{document: "doc", title: "母排规范.pdf", ref: "header", locators: location("table"),
			body: "<tr><td>Tabelle 4.</td><td>Innenraum</td><td>Innenraum und Freiluft</td></tr>"},
		{document: "doc", title: "母排规范.pdf", ref: "washers", locators: location("table"),
			body: "Bei Gewinde M 8 und Stromschienen aus G-X sind zusätzliche Scheiben erforderlich."},
		{document: "doc", title: "母排规范.pdf", ref: "different-table", locators: location("another-table"),
			body: "<tr><td>M 8</td><td>10</td><td>14</td></tr>"},
		{document: "doc", title: "母排规范.pdf", ref: "row", locators: location("table"),
			body: "<tr><td>M 8</td><td>11</td><td>17</td></tr>"},
	}
	question := "《母排规范》母排螺栓拧紧力矩是多少？"
	got := renderVerifiedTorque(question, sources)
	for _, want := range []string{"直流和不超过 70 Hz 的交流", "室内 11 N·m，室内和露天 17 N·m",
		"额外垫圈限于螺纹 M8", "scope", "washers", "row"} {
		require.Contains(t, got, want)
	}
	require.NotContains(t, got, "及以上")
	require.NotContains(t, got, "different-table")
	require.Empty(t, renderVerifiedTorque(question, sources[:4]))
	require.Empty(t, renderVerifiedTorque("《母排规范》母排 M6、M8 的拧紧力矩分别是多少？", sources))
}

func TestVerifiedOverviewCitesPurposeAndCodeFromSameDocument(t *testing.T) {
	sources := []reviewSource{
		{document: "a", title: "材料选型.pdf", ref: "purpose-a", body: "## 1.目的\n\n用于确定材料范围。\n\n## 2.应用"},
		{document: "a", title: "材料选型.pdf", ref: "cover-a", body: "文件编号 CK9876"},
		{document: "b", title: "成品检查.pdf", ref: "purpose-b", body: "1.目的\n用于规定成品的检验项目。"},
	}
	query := "《成品检查》《材料选型》分别规定了什么？"
	got := renderVerifiedNamedOverview(query, sources)
	require.Contains(t, got, "《成品检查》：用于规定成品的检验项目。 purpose-b")
	require.Contains(t, got, "《材料选型》（文件编号 CK9876） cover-a：用于确定材料范围。 purpose-a")
	require.Empty(t, renderVerifiedNamedOverview(query, sources[:2]))

	sources[1].document = "unrelated"
	require.Empty(t, renderVerifiedNamedOverview(query, sources))
}

func TestVerifiedOverviewAllowsDetailedAnswers(t *testing.T) {
	base := "《质量检验》《设备规范》各自的用途是什么？"
	require.Empty(t, namedOverviewEvidenceInstruction(base))
	require.NotEmpty(t, namedOverviewEvidenceInstruction(base+"请分列核心对象和每份文件自己的依据。"))
	require.NotEmpty(t, namedOverviewEvidenceInstruction("《甲规范》《乙规范》对供应商选材和质量确认分别规定了什么？"))
	require.Empty(t, namedOverviewEvidenceInstruction("甲、乙、丙三个型号的初始振荡频率分别是多少？"))
}
