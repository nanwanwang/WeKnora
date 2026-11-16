package modelcontext

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestBracketCitationsUseCurrentEvidence(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		r := NewRegistry(enabled)
		r.sources.RegisterChunk(ChunkReference{ChunkID: "chunk-1", DocumentTitle: "Doc", KnowledgeBaseID: "allowed-kb"})
		r.sources.RegisterWeb("https://example.com", "Example")
		r.sources.registerChunk(ChunkReference{ChunkID: "history-only"}, false)
		kb := `<kb doc="Doc" chunk_id="chunk-1" kb_id="allowed-kb" />`
		web := `<web url="https://example.com" title="Example" />`
		if !enabled {
			kb = ""
			web = ""
		}
		cases := []struct{ in, want string }{
			{`依据 [ref id="chunk://1"/>。`, "依据 " + kb + "。"},
			{`依据 [ref id="chunk://1"]。`, "依据 " + kb + "。"},
			{`依据 [ref id="chunk://1"/]。`, "依据 " + kb + "。"},
			{`依据 [REF ID="chunk://1">。`, "依据 " + kb + "。"},
			{`[ref id="url://1"/>`, web},
			{`[ref id="chunk://999"/>`, ""},
			{`[ref id="chunk://2"/>`, ""},
			{`[ref id="doc://1"/>`, ""},
			{`[ref id="kb://1"/>`, ""},
			{`[ref id="chunk://1"`, ""},
			{`[ref id="example"]`, `[ref id="example"]`},
			{`[reference](https://example.com) [1] [`, `[reference](https://example.com) [1] [`},
			{`<ref id="chunk://1"/>[ref id="chunk://1"/>`, kb + kb},
		}
		for _, tc := range cases {
			require.Equal(t, tc.want, r.DecodeOutputText(tc.in), "enabled=%v input=%s", enabled, tc.in)
			for split := 0; split <= len(tc.in); split++ {
				d := r.StreamDecoder()
				got := d.Feed(tc.in[:split]) + d.Feed(tc.in[split:]) + d.Flush()
				require.Equal(t, tc.want, got, "enabled=%v input=%s split=%d", enabled, tc.in, split)
			}
			d := r.StreamDecoder()
			var b strings.Builder
			for i := range len(tc.in) {
				b.WriteString(d.Feed(tc.in[i : i+1]))
			}
			b.WriteString(d.Flush())
			require.Equal(t, tc.want, b.String())
		}
	}
}
