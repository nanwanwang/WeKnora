package types

import (
	"reflect"
	"testing"
)

func TestExactIdentifiers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"whole identifier", "WO-12345678", []string{"WO-12345678"}},
		{"Chinese question and lowercase", "请查wo-12345678的维修记录", []string{"WO-12345678"}},
		{"deduplicate", "WO-12345678 wo-12345678", []string{"WO-12345678"}},
		{"multiple identifiers", "WO-12345678、PO-87654321", []string{"WO-12345678", "PO-87654321"}},
		{"date", "2026-11-12", []string{}},
		{"natural language", "灯显示异常", []string{}},
		{"short numeric suffix", "WO-12345", []string{}},
		{"long numeric suffix", "WO-123456789012345678901", []string{}},
		{"long prefix", "ABCDEFGHI-12345678", []string{}},
		{"identifier suffix", "WO-12345678-REV", []string{}},
		{"underscore boundary", "_WO-12345678", []string{}},
		{"punctuation", "(WO-12345678)", []string{"WO-12345678"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExactIdentifiers(tt.input); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
func TestContainsExactIdentifier(t *testing.T) {
	for _, content := range []string{"WO-123456780", "XWO-12345678", "WO-12345679", "WO-12345678-REV"} {
		if ContainsExactIdentifier(content, []string{"WO-12345678"}) {
			t.Fatalf("accepted partial or different identifier %q", content)
		}
	}
	if !ContainsExactIdentifier("| wo-12345678 |", []string{"WO-12345678"}) {
		t.Fatal("case insensitive match missing")
	}
	if !ContainsExactIdentifier("PO-87654321", []string{"WO-12345678", "PO-87654321"}) {
		t.Fatal("multiple identifier union missing")
	}
	if ContainsExactIdentifier("WO-12345678", nil) {
		t.Fatal("empty query matched")
	}
}
