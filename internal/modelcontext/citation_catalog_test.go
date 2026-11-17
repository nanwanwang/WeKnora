package modelcontext

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCitationCatalogRequiresExplicitCurrentEvidence(t *testing.T) {
	r := NewRegistry(true)
	chunk := r.RegisterChunk(ChunkReference{ChunkID: "allowed-chunk", KnowledgeID: "allowed-doc", KnowledgeBaseID: "allowed-kb", DocumentTitle: "Guide"})
	web := r.RegisterWeb("https://example.com/source", "External")
	history := r.RegisterContextChunk(ChunkReference{ChunkID: "history-chunk", KnowledgeBaseID: "other-kb"})
	require.Empty(t, r.DecodeOutputText(`<ref id="1"/>`), "numbers are not inferred even when chunk://1 exists")
	require.Empty(t, r.CitationTemplate(history))
	require.Empty(t, r.CitationTemplate("chunk://999"))
	require.Empty(t, r.CitationTemplate("doc://1"))
	require.Empty(t, r.CitationTemplate("kb://1"))
	require.Empty(t, r.CitationTemplate("1"))
	require.Equal(t, `<ref id="1"/>`, r.CitationTemplate(chunk))
	require.Equal(t, `<ref id="2"/>`, r.CitationTemplate(web), "web and chunk numbers must not collide")
	require.Equal(t, `<ref id="1"/>`, r.CitationTemplate(chunk))
	cases := []struct{ in, want string }{
		{`<ref id="1"/>`, `<kb doc="Guide" chunk_id="allowed-chunk" kb_id="allowed-kb" />`},
		{`[ref id="1"/>`, `<kb doc="Guide" chunk_id="allowed-chunk" kb_id="allowed-kb" />`},
		{`<ref id="2"/>`, `<web url="https://example.com/source" title="External" />`},
		{`[ref id="999"/>`, ""},
		{`[ref id="ref id="1"/>`, ""},
		{`[ref id="1`, ""},
		{`<ref id="18/3/>`, ""},
		{`[ref id="1,2"/>`, ""},
		{`<ref id="1/2`, ""},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, r.DecodeOutputText(tc.in))
		for split := 0; split <= len(tc.in); split++ {
			d := r.StreamDecoder()
			require.Equal(t, tc.want, d.Feed(tc.in[:split])+d.Feed(tc.in[split:])+d.Flush())
		}
	}
	next := NewRegistry(true)
	next.RegisterChunk(ChunkReference{ChunkID: "another-current-chunk"})
	require.Empty(t, next.DecodeOutputText(`<ref id="1"/>`), "catalogs must not survive into another request")
	disabled := NewRegistry(false)
	h := disabled.RegisterChunk(ChunkReference{ChunkID: "allowed-chunk"})
	require.Empty(t, disabled.CitationTemplate(h))
	require.Empty(t, disabled.DecodeOutputText(`<ref id="1"/>`))
}
