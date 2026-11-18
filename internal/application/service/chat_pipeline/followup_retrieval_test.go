package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestN273FollowupInheritsRetrievalContextWithRewriteDisabled(t *testing.T) {
	const first = "《母排设计规范》里50×6单铜排在环境温度25℃时的交流载流量是多少？"
	const second = "那要是换成直流供电呢，同样25℃下能过多少安？"
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: second, MaxRounds: 5},
		PipelineState: types.PipelineState{
			UserContent: second,
			History: []*types.History{
				{UserQuery: "《其他规范》中的63×6是怎样规定的？"},
				{UserQuery: first, Query: first + "\n\n[附件正文]\n80×6"},
			},
		},
	}
	called := false
	err := (&PluginQueryUnderstand{}).OnEvent(t.Context(), types.QUERY_UNDERSTAND, manage, func() *PluginError {
		called = true
		require.Equal(t, "《母排设计规范》 50×6 "+second, manage.RewriteQuery)
		require.Equal(t, second, manage.Query)
		require.Equal(t, second, manage.UserContent)
		return nil
	})
	require.Nil(t, err)
	require.True(t, called)
}

func TestQualifyFollowupPreservesExplicitNewContext(t *testing.T) {
	const previous = "《母排设计规范》里50 x 6的交流载流量是多少？"
	for _, tc := range []struct {
		name, query, previous, want string
	}{
		{"new size", "那改成63×6呢？", previous, "《母排设计规范》 那改成63×6呢？"},
		{"new document", "那换成《铝排规范》呢？", previous, "那换成《铝排规范》呢？"},
		{"new filename", "那换成替代规范.pdf呢？", previous, "那换成替代规范.pdf呢？"},
		{"same document", "同样《母排设计规范》里直流是多少？", previous, "50×6 同样《母排设计规范》里直流是多少？"},
		{"new topic", "什么是绝缘耐压？", previous, "什么是绝缘耐压？"},
		{"ambiguous prior size", "那要是换成直流呢？", "《母排设计规范》中的50×6和63×6分别是多少？", "《母排设计规范》 那要是换成直流呢？"},
		{"embedded model", "那要是换成直流呢？", "《母排设计规范》里的AB50x6型号适用什么条件？", "《母排设计规范》 那要是换成直流呢？"},
		{"no explicit title", "那要是换成直流呢？", "50×6单铜排交流载流量是多少？", "那要是换成直流呢？"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, qualifyFollowup(tc.query, tc.previous))
		})
	}
}
