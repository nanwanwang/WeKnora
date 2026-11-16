package web_search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// DeepSeekProvider backports standalone Search support to the v0.9.0 interface.
// Search is independent of the model used to generate the final answer.
type DeepSeekProvider struct {
	client           *http.Client
	endpoint, apiKey string
}

func ValidateDeepSeekParameters(params types.WebSearchProviderParameters) error {
	if strings.TrimSpace(params.APIKey) == "" {
		return fmt.Errorf("API key is required for DeepSeek provider")
	}
	return nil
}
func NewDeepSeekProvider(params types.WebSearchProviderParameters) (interfaces.WebSearchProvider, error) {
	if err := ValidateDeepSeekParameters(params); err != nil {
		return nil, err
	}
	client, err := NewSearchHTTPClient(60*time.Second, params.ProxyURL)
	if err != nil {
		return nil, err
	}
	return &DeepSeekProvider{client, "https://api.deepseek.com/v1/tools/search", strings.TrimSpace(params.APIKey)}, nil
}
func (p *DeepSeekProvider) Name() string { return "deepseek" }
func (p *DeepSeekProvider) Search(ctx context.Context, query string, count int, includeDate bool) ([]*types.WebSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is empty")
	}
	if runes := []rune(query); len(runes) > 128 {
		query = string(runes[:128])
	}
	count = boundedSearchCount(count)
	body, err := postSearchJSON(ctx, p.client, p.endpoint, p.apiKey, map[string]any{"query": query, "count": count}, p.Name())
	if err != nil {
		return nil, err
	}
	var response struct {
		Results []struct {
			Title, URL, Snippet, Summary, Content string
			Date                                  *string
		} `json:"results"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("DeepSeek: invalid search response: %w", err)
	}
	if len(response.Error) > 0 && string(response.Error) != "null" {
		return nil, fmt.Errorf("DeepSeek: search API returned an error; check credentials and quota")
	}
	if response.Results == nil {
		return nil, fmt.Errorf("DeepSeek: search response is missing results")
	}
	results := make([]*types.WebSearchResult, 0, len(response.Results))
	for _, item := range response.Results {
		if strings.TrimSpace(item.URL) == "" {
			continue
		}
		snippet := strings.TrimSpace(item.Summary)
		if snippet == "" {
			snippet = strings.TrimSpace(item.Snippet)
		}
		result := &types.WebSearchResult{Title: item.Title, URL: strings.TrimSpace(item.URL), Snippet: snippet, Content: item.Content, Source: p.Name()}
		if includeDate && item.Date != nil {
			if date, err := time.Parse(time.RFC3339Nano, *item.Date); err == nil {
				result.PublishedAt = &date
			}
		}
		results = append(results, result)
		if len(results) >= count {
			break
		}
	}
	return results, nil
}

func boundedSearchCount(count int) int {
	if count <= 0 {
		return 10
	}
	if count > 50 {
		return 50
	}
	return count
}

// Do not include upstream bodies in errors: a provider may echo credentials.
// The shared client preserves proxy support, context cancellation and SSRF rules.
func postSearchJSON(ctx context.Context, client *http.Client, endpoint, key string, payload any, provider string) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s search request failed: %w", provider, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s search API returned HTTP %d; check credentials, quota and service availability", provider, resp.StatusCode)
	}
	const limit = 4 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("%s search response exceeds size limit", provider)
	}
	return data, nil
}
