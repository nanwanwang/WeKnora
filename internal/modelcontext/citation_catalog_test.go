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
		{"[ref id=\"76``/>", ""},
		{"[ref id=\"1``/>", ""},
		{"<ref id=\"1`/>", ""},
		{"[ref id=\"76``", ""},
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

func TestMalformedChunkRefsAreDiscardedWithoutGuessingCitations(t *testing.T) {
	registry := NewRegistry(true)
	chunk := registry.RegisterChunk(ChunkReference{
		ChunkID: "current-chunk", KnowledgeBaseID: "current-kb", DocumentTitle: "Current document",
	})
	valid := registry.CitationTemplate(chunk)
	canonical := `<kb doc="Current document" chunk_id="current-chunk" kb_id="current-kb" />`
	cases := []struct{ in, want string }{
		{`预处理 <ref id="chunk="53"/>。`, "预处理 。"},
		{`恢复 <ref id="chunk="81/>。`, "恢复 。"},
		{`测量 <ref id="chunk="54/>。`, "测量 。"},
		{`避免凝露 <ref id="chunk="32/> <ref id="chunk="53/>。`, "避免凝露  。"},
		{`<ref id="chunk="1"/>` + valid, canonical},
		{`[ref id="chunk="12/>`, ""},
		{`[ref id="chunk="53`, ""},
		{`<ref id="chunk=`, ""},
		{`<ref id="chunk="53`, ""},
		{`{"answer":"依据 <ref id=\"chunk=\"32/>。"}`, `{"answer":"依据 。"}`},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, registry.DecodeOutputText(tc.in), "%q", tc.in)
		for split := 0; split <= len(tc.in); split++ {
			decoder := registry.StreamDecoder()
			got := decoder.Feed(tc.in[:split]) + decoder.Feed(tc.in[split:]) + decoder.Flush()
			require.Equal(t, tc.want, got, "%q split at %d", tc.in, split)
		}
	}
}
