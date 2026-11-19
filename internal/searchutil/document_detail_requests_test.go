package searchutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDocumentDetailRequestsKeepEditionAndIndependentQuestions(t *testing.T) {
	query := "安排两项试验。按《GB/T 1234.5-2024》，试样如何恢复？" +
		"依据《材料储存规范.pdf》，合格条件是什么？请分文件作答。"
	require.Equal(t, []DocumentDetailRequest{
		{Title: "gbt1234.5-2024", Question: "试样如何恢复"},
		{Title: "材料储存规范", Question: "合格条件是什么"},
	}, DocumentDetailRequests(query))
	require.Equal(t, []string{"gbt1234.5-2024", "材料储存规范"}, RequestedEvidenceTitles(query))
	require.True(t, MatchesEvidenceDocument("gbt1234.5-2024", "GB-T 1234.5-2024（试验方法）.pdf"))
	require.False(t, MatchesEvidenceDocument("gbt1234.5-2024", "GB/T 1234.5-2020.pdf"))
	require.False(t, MatchesEvidenceDocument("gbt1234.5-2024", "GBT 1234.5-20240.pdf"))
	require.False(t, MatchesEvidenceDocument("材料储存规范", "材料储存规范(1).pdf"))
}

func TestDocumentDetailRequestsSkipsUnscopedAndDuplicateQuestions(t *testing.T) {
	for _, query := range []string{
		"《检验规范》和《储存规范》的试验条件有什么不同？",
		"按 GB/T 1234.5，试样如何恢复？按《储存规范》，判据是什么？",
		"按 GB/T 1234.5-2024，如何恢复？依据《GBT 1234.5-2024》，怎样检测？",
	} {
		require.Empty(t, DocumentDetailRequests(query), query)
	}
}
