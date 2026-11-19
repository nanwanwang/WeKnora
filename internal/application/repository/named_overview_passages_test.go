package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNamedOverviewPassagesUsesPurposeAfterTOC(t *testing.T) {
	rows := []documentTitlePassage{
		{ID: "cover", Content: "文件编号 CI1\n修订记录\n目录"},
		{ID: "contents", Content: "# 目录\n2.目的 ................... 3\n2.适用范围 .......... 3"},
		{ID: "body", Content: "附录 D …… 9\n2. 目 的\n本规范规定设备的设计和安全操作。\n2.适用范围\n适用于公司设备和说明书。"},
	}
	got := selectNamedOverviewPassages("《甲规范》《乙规范》各自的用途是什么？", rows)
	require.NotEmpty(t, got)
	require.Equal(t, rows[2], got[0])
	require.NotContains(t, got, rows[1])
}

func TestNamedOverviewPassagesIncludesSelectionAndQuality(t *testing.T) {
	rows := []documentTitlePassage{
		{ID: "cover", Content: "文件编号 CI1\n修订记录\n目录"},
		{ID: "purpose", Content: "1.目的\n本规范旨在帮助设计人员选择材料，并为采购人员提供质量确认指导。"},
		{ID: "unrelated", Content: "此处是另一项例行检测，现场人员应记录结果。"},
		{ID: "selection", Content: "4.材料选用规范\n根据产品的应用场景与材料标识选择，查阅表中公布的材料名单。"},
		{ID: "quality", Content: "5.材料检验规范\n职责分工由质量主管确定，供应商提供检验报告，收货时随机抽检。"},
	}
	got := selectNamedOverviewPassages("《甲规范》《乙规范》对供应商选材和质量确认分别规定了什么？", rows)
	require.Equal(t, []documentTitlePassage{rows[1], rows[3], rows[4]}, got)
}
