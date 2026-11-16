package web_search

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testSearchProvider(kind string, server *httptest.Server) interfaces.WebSearchProvider {
	if kind == "deepseek" {
		return &DeepSeekProvider{server.Client(), server.URL, "test-secret"}
	}
	return &TencentProvider{server.Client(), server.URL, "test-secret"}
}
func TestStandaloneSearchMapping(t *testing.T) {
	for _, kind := range []string{"deepseek", "tencent"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Errorf("invalid method/auth")
				}
				var req map[string]any
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatal(err)
				}
				if kind == "deepseek" {
					if req["count"] != float64(1) || len([]rune(req["query"].(string))) != 128 {
						t.Errorf("invalid DeepSeek request: %v", req)
					}
					fmt.Fprint(w, `{"results":[{"title":"新闻","url":"https://example.com/a","summary":"long summary","snippet":"short","date":"2026-11-16T01:00:00Z"},{"url":"https://example.com/b"}]}`)
				} else {
					if req["Cnt"] != float64(10) || req["Mode"] != float64(0) || req["Query"] == nil {
						t.Errorf("invalid Tencent request: %v", req)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"Response": map[string]any{"Pages": []string{`{"title":"新闻","url":"https://example.com/a","content":"long summary","passage":"short","date":"2026/11/16 09:00:00"}`, `{"url":"https://example.com/b"}`}}})
				}
			}))
			defer server.Close()
			got, err := testSearchProvider(kind, server).Search(context.Background(), strings.Repeat("中", 140), 1, true)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Snippet != "long summary" || got[0].Source != kind || got[0].URL != "https://example.com/a" {
				t.Fatalf("unexpected results: %+v", got)
			}
			if got[0].PublishedAt == nil || got[0].PublishedAt.UTC().Format(time.RFC3339) != "2026-11-16T01:00:00Z" {
				t.Fatalf("unexpected date: %v", got[0].PublishedAt)
			}
		})
	}
}
func TestStandaloneSearchFailures(t *testing.T) {
	for _, kind := range []string{"deepseek", "tencent"} {
		for _, status := range []int{401, 429, 500} {
			t.Run(fmt.Sprintf("%s/%d", kind, status), func(t *testing.T) {
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); fmt.Fprint(w, "test-secret") }))
				defer s.Close()
				_, err := testSearchProvider(kind, s).Search(context.Background(), "test", 5, false)
				if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) || strings.Contains(err.Error(), "test-secret") {
					t.Fatalf("unsafe/missing error: %v", err)
				}
			})
		}
	}
	for _, tc := range []struct{ kind, body string }{
		{"deepseek", `{"error":{"message":"test-secret"}}`},
		{"deepseek", `{}`}, {"deepseek", `invalid`},
		{"tencent", `{"Response":{"Error":{"Code":"ResourceNotFound","Message":"test-secret"}}}`},
		{"tencent", `{"Response":{"Pages":["invalid"]}}`}, {"tencent", `{}`},
	} {
		t.Run(tc.kind+tc.body, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer s.Close()
			_, err := testSearchProvider(tc.kind, s).Search(context.Background(), "test", 5, false)
			if err == nil || strings.Contains(err.Error(), "test-secret") {
				t.Fatalf("unsafe/missing error: %v", err)
			}
		})
	}
}
func TestStandaloneSearchLimitsAndCancellation(t *testing.T) {
	for _, kind := range []string{"deepseek", "tencent"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var req map[string]any
				_ = json.NewDecoder(r.Body).Decode(&req)
				key := "count"
				if kind == "tencent" {
					key = "Cnt"
				}
				if req[key] != float64(50) {
					t.Errorf("count: %v", req)
				}
				if kind == "deepseek" {
					fmt.Fprint(w, `{"results":[]}`)
				} else {
					fmt.Fprint(w, `{"Response":{"Pages":[]}}`)
				}
			}))
			defer s.Close()
			p := testSearchProvider(kind, s)
			if _, err := p.Search(context.Background(), "  ", 5, false); err == nil {
				t.Fatal("empty query accepted")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := p.Search(ctx, "test", 5, false); err == nil {
				t.Fatal("cancellation ignored")
			}
			got, err := p.Search(context.Background(), "test", 100, false)
			if err != nil || len(got) != 0 || calls != 1 {
				t.Fatalf("empty results/calls: %v %v %d", got, err, calls)
			}
		})
	}
}
func TestStandaloneCredentialsAndResponseLimit(t *testing.T) {
	for _, factory := range []func(types.WebSearchProviderParameters) (interfaces.WebSearchProvider, error){NewDeepSeekProvider, NewTencentProvider} {
		if _, err := factory(types.WebSearchProviderParameters{APIKey: " "}); err == nil {
			t.Fatal("blank key accepted")
		}
		if _, err := factory(types.WebSearchProviderParameters{APIKey: "key"}); err != nil {
			t.Fatal(err)
		}
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", (4<<20)+1)) }))
	defer s.Close()
	if _, err := testSearchProvider("deepseek", s).Search(context.Background(), "test", 1, false); err == nil {
		t.Fatal("oversized response accepted")
	}
}
func TestStandaloneFallbackAndNoDate(t *testing.T) {
	for _, tc := range []struct{ kind, body string }{
		{"deepseek", `{"results":[{"url":""},{"url":"https://example.com","snippet":"fallback","date":null}]}`},
		{"tencent", `{"Response":{"Pages":["{}","{\"url\":\"https://example.com\",\"passage\":\"fallback\",\"date\":\"2026-11-16\"}"]}}`},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer s.Close()
			got, err := testSearchProvider(tc.kind, s).Search(context.Background(), "test", 0, false)
			if err != nil || len(got) != 1 || got[0].Snippet != "fallback" || got[0].PublishedAt != nil {
				t.Fatalf("fallback: %+v %v", got, err)
			}
		})
	}
}
