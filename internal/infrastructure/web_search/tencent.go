package web_search

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"net/http"
	"strings"
	"time"
)

// TencentProvider uses the WSA service API key, not a CAM SecretId/SecretKey.
// https://cloud.tencent.com/document/product/1806/130615
type TencentProvider struct {
	client           *http.Client
	endpoint, apiKey string
}

func ValidateTencentParameters(params types.WebSearchProviderParameters) error {
	if strings.TrimSpace(params.APIKey) == "" {
		return fmt.Errorf("Tencent search service API key is required (not CAM SecretId/SecretKey)")
	}
	return nil
}
func NewTencentProvider(params types.WebSearchProviderParameters) (interfaces.WebSearchProvider, error) {
	if err := ValidateTencentParameters(params); err != nil {
		return nil, err
	}
	client, err := NewSearchHTTPClient(60*time.Second, params.ProxyURL)
	if err != nil {
		return nil, err
	}
	return &TencentProvider{client, "https://api.wsa.cloud.tencent.com/SearchPro", strings.TrimSpace(params.APIKey)}, nil
}
func (p *TencentProvider) Name() string { return "tencent" }
func (p *TencentProvider) Search(ctx context.Context, query string, count int, includeDate bool) ([]*types.WebSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is empty")
	}
	count = boundedSearchCount(count)
	// Tencent accepts only 10/20/30/40/50. Trim results to the caller's limit.
	body, err := postSearchJSON(ctx, p.client, p.endpoint, p.apiKey, map[string]any{"Query": query, "Cnt": ((count + 9) / 10) * 10, "Mode": 0}, p.Name())
	if err != nil {
		return nil, err
	}
	var response struct {
		Response *struct {
			Pages     []string
			Error     *struct{ Code string }
			RequestID string `json:"RequestId"`
		}
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("Tencent: invalid search response: %w", err)
	}
	if response.Response == nil {
		return nil, fmt.Errorf("Tencent: missing Response envelope")
	}
	if response.Response.Error != nil {
		return nil, fmt.Errorf("Tencent search API error (%s)", safeTencentErrorCode(response.Response.Error.Code))
	}
	if response.Response.Pages == nil {
		return nil, fmt.Errorf("Tencent: search response is missing Pages")
	}
	results := make([]*types.WebSearchResult, 0, len(response.Response.Pages))
	for _, raw := range response.Response.Pages {
		var page struct{ Title, URL, Passage, Content, Date string }
		if err := json.Unmarshal([]byte(raw), &page); err != nil {
			return nil, fmt.Errorf("Tencent: invalid page JSON: %w", err)
		}
		if strings.TrimSpace(page.URL) == "" {
			continue
		}
		snippet := strings.TrimSpace(page.Content)
		if snippet == "" {
			snippet = strings.TrimSpace(page.Passage)
		}
		// Tencent content is a dynamic summary, not the full page body.
		result := &types.WebSearchResult{Title: page.Title, URL: strings.TrimSpace(page.URL), Snippet: snippet, Source: p.Name()}
		if includeDate {
			if date, ok := parseTencentDate(page.Date); ok {
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
func safeTencentErrorCode(code string) string {
	// Never expose arbitrary upstream text (including possible credential echoes).
	switch strings.Split(code, ".")[0] {
	case "AuthFailure", "UnauthorizedOperation", "RequestLimitExceeded", "ResourceNotFound", "ResourceUnavailable", "InvalidParameter", "InvalidParameterValue", "InternalError", "FailedOperation", "LimitExceeded":
		return strings.Split(code, ".")[0]
	default:
		return "UnknownError"
	}
}
func parseTencentDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if d, e := time.Parse(time.RFC3339Nano, value); e == nil {
		return d, true
	}
	// SearchPro examples use Chinese local wall time without an offset.
	for _, layout := range []string{"2006-01-02 15:04:05", "2006/01/02 15:04:05", "2006-01-02", "2006/01/02"} {
		if d, e := time.ParseInLocation(layout, value, time.FixedZone("Asia/Shanghai", 8*60*60)); e == nil {
			return d, true
		}
	}
	return time.Time{}, false
}
