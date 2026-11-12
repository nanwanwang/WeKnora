package qdrant

import "testing"

func TestCreatePayloadExactIdentifiers(t *testing.T) {
	content := "| wo-12345678 | WO-12345678 | PO-87654321 |"
	payload := createPayload(&QdrantVectorEmbedding{
		Content: content, ChunkID: "chunk", KnowledgeID: "doc",
		KnowledgeBaseID: "kb", IsEnabled: true,
	})
	values := payload["exact_identifiers"].GetListValue().GetValues()
	if len(values) != 2 || values[0].GetStringValue() != "WO-12345678" || values[1].GetStringValue() != "PO-87654321" {
		t.Fatalf("unexpected identifiers: %v", values)
	}
	if payload[fieldContent].GetStringValue() != content || payload[fieldKnowledgeBaseID].GetStringValue() != "kb" || !payload[fieldIsEnabled].GetBoolValue() {
		t.Fatal("existing payload fields changed")
	}
	plain := createPayload(&QdrantVectorEmbedding{Content: "灯显示异常"})
	if len(plain["exact_identifiers"].GetListValue().GetValues()) != 0 {
		t.Fatal("plain text got an identifier")
	}
}
