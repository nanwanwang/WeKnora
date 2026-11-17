package chunker

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPresentHTMLTableEvidencePreservesIndependentColumnGroups(t *testing.T) {
	header := `<tr><td colspan="2">Inventory</td><td colspan="3">Shipping</td></tr>
<tr><td>Item</td><td>Bin</td><td>Destination</td><td>Service</td><td>Zone</td></tr>`
	rows := `<tr><td>Part-99</td><td>K-2</td><td>Oslo</td><td>Air</td><td>7</td></tr>
<tr><td>Part-42</td><td>B-9</td><td>Rome</td><td>Sea</td><td>3</td></tr>`
	context := tableContextPrefix + "Inventory（跨2列） | Shipping（跨3列）\nItem | Bin | Destination | Service | Zone"
	for _, view := range []string{
		PresentHTMLTableEvidence("<table>\n"+header+"\n"+rows+"\n</table>", ""),
		PresentHTMLTableEvidence(rows, context),
	} {
		require.Contains(t, view, "原表列组 Inventory")
		require.Contains(t, view, "Item = Part-99 | Bin = K-2")
		require.Contains(t, view, "原表列组 Shipping")
		require.Contains(t, view, "Destination = Oslo | Service = Air | Zone = 7")
		require.NotContains(t, view, "Bin = K-2 | Destination")
	}
	badContext := strings.Replace(context, "跨3列", "跨4列", 1)
	require.Equal(t, rows, PresentHTMLTableEvidence(rows, badContext))
	require.Equal(t, rows, PresentHTMLTableEvidence(rows, ""))
}

func TestPresentHTMLTableEvidenceCarriesOnlyDeclaredRowspans(t *testing.T) {
	body := `Before
<table><tbody>
<tr><td rowspan="2">Calibration</td><td>Short</td><td>Q ≤ 18 V and R ≥ 20 s</td><td rowspan="2">Check</td></tr>
<tr><td>Long</td><td>Q ≤ 40 V<br/>and R ≥ 12 s</td></tr>
<tr><td>Cleaning</td><td>Weekly</td><td>Dust test</td><td>Store</td></tr>
</tbody></table>
After`
	view := PresentHTMLTableEvidence(body, "")
	require.Contains(t, view, "Calibration | Short | Q ≤ 18 V and R ≥ 20 s | Check")
	require.Contains(t, view, "Calibration | Long | Q ≤ 40 V and R ≥ 12 s | Check")
	require.Contains(t, view, "Cleaning | Weekly | Dust test | Store")
	require.Contains(t, view, "Before")
	require.Contains(t, view, "After")
	fenced := "```html\n" + body + "\n```"
	require.Equal(t, fenced, PresentHTMLTableEvidence(fenced, ""))
	withImage := strings.Replace(body, "Dust test", `<img src="example.png">`, 1)
	require.Equal(t, withImage, PresentHTMLTableEvidence(withImage, ""))
}

func TestPresentTableEvidenceRecognizesRepeatedColumnsInLegacyIndex(t *testing.T) {
	body := "仓库 采购\n货号 描述 批号 描述\nBX-2 白盒 JD-9 红箱\nAX-3 黑盒 MD-8 蓝箱\n\n后续正文 L<5mm"
	view := PresentTableEvidence(body, "")
	require.Contains(t, view, "原表列组 仓库")
	require.Contains(t, view, "货号 = BX-2 | 描述 = 白盒")
	require.Contains(t, view, "原表列组 采购")
	require.Contains(t, view, "批号 = JD-9 | 描述 = 红箱")
	require.Contains(t, view, "后续正文 L<5mm")
	withoutRepeat := strings.Replace(body, "批号 描述", "批号 属性", 1)
	require.Equal(t, withoutRepeat, PresentTableEvidence(withoutRepeat, ""))
}
