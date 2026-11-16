package searchutil

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTopicClassificationKeepsSpecificQueriesFocused(t *testing.T) {
	for _, q := range []string{"EMC设计", "emc 设计", "碳素结构钢", "thermal design"} {
		require.True(t, IsTopicQuery(q))
		require.True(t, IsBroadTopicQuery(q), q)
	}
	for _, q := range []string{"485接口EMC设计", "USB接口EMC设计", "VC1 PLC系列", "GB/T 700-2006", "EMC设计.pdf", "PCB--emc设计指南", "UART interface EMC design"} {
		require.True(t, IsTopicQuery(q), q)
		require.False(t, IsBroadTopicQuery(q), q)
	}
	for _, q := range []string{"", "a", "如何设计EMC？", "请查询。再说明"} {
		require.False(t, IsTopicQuery(q), q)
		require.False(t, IsBroadTopicQuery(q), q)
	}
	require.Equal(t, "emc设计", NormalizeDocumentTitle(" EMC　设计.PDF "))
}
