package searchutil

import "testing"

func TestLiteralLookup(t *testing.T) {
	for _, q := range []string{"VF-101-P200-0380-A-T4-E20-N-H-D", "PH-24080231", "9120170265O805654083", "更换电源板后测试正常", "power board failure"} {
		if LiteralLookup(" "+q+" ") != q {
			t.Errorf("pasted value rejected: %q", q)
		}
	}
	for _, q := range []string{"", "AB", "今年故障多少台", "统计各型号占比", "how many repairs", "第一行\n第二行"} {
		if LiteralLookup(q) != "" {
			t.Errorf("question treated as pasted value: %q", q)
		}
	}
}

func TestContainsLiteralPreservesIdentifierBoundaries(t *testing.T) {
	q := "VF-101-P200-0380-A-T4-E20-N-H-D"
	for _, body := range []string{"| " + q + " | 维修措施 |", "型号：" + q + "，已维修", "[vf-101-p200-0380-a-t4-e20-n-h-d]"} {
		if !ContainsLiteral(body, q) {
			t.Errorf("missed %q", body)
		}
	}
	for _, body := range []string{"X" + q, q + "-REV", q + "0", q + "_ALT"} {
		if ContainsLiteral(body, q) {
			t.Errorf("accepted different identifier %q", body)
		}
	}
	if !ContainsLiteral("故障：更换电源板后测试正常。", "更换电源板后测试正常") {
		t.Fatal("missed pasted prose")
	}
}
