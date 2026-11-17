package reranking

import (
	"context"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
	"regexp"
	"sort"
	"strings"
)

// SelectBalanced only selects among supplied eligible results. It never changes
// scores, lowers thresholds, authorizes sources or invents evidence. With both
// sources present, reserve up to 20% (at least one for k>=2) for the web; unused
// slots are backfilled. k=1 prefers internal evidence. Ordinary MMR fills repeats.
func SelectBalanced(ctx context.Context, results []*types.SearchResult, k int, query string, diversify bool) []int {
	if k <= 0 {
		return nil
	}
	var internal, web []int
	for i, r := range results {
		if r == nil {
			continue
		}
		if r.ChunkType == "web_search" || r.KnowledgeSource == "web_search" {
			web = append(web, i)
		} else {
			internal = append(internal, i)
		}
	}
	less := func(a, b int) bool {
		if results[a].Score != results[b].Score {
			return results[a].Score > results[b].Score
		}
		return results[a].ID < results[b].ID
	}
	sort.SliceStable(internal, func(a, b int) bool { return less(internal[a], internal[b]) })
	sort.SliceStable(web, func(a, b int) bool { return less(web[a], web[b]) })
	webCount := 0
	if len(web) > 0 {
		if len(internal) == 0 {
			webCount = min(k, len(web))
		} else if k >= 2 {
			webCount = min(len(web), max(1, k/5))
		}
	}
	kbCount := min(len(internal), k-webCount)
	webCount = min(len(web), k-kbCount)
	titleHint, topic := searchutil.DocumentTitleHint(query)
	choose := func(indices []int, count int, knowledge bool) []int {
		var picks []int
		selected := map[int]bool{}
		docs := map[string]bool{}
		key := func(i int) string {
			r := results[i]
			if r.KnowledgeID == "" || r.ChunkType == "faq" {
				return "chunk:" + r.ID
			}
			return r.KnowledgeBaseID + ":" + r.KnowledgeID
		}
		add := func(i int) {
			if !selected[i] && len(picks) < count {
				picks = append(picks, i)
				selected[i] = true
				docs[key(i)] = true
			}
		}
		if knowledge && titleHint != "" {
			exact := -1
			for _, i := range indices {
				row := results[i]
				if searchutil.NormalizeDocumentTitle(row.KnowledgeTitle) != titleHint || strings.TrimSpace(row.Content) == "" {
					continue
				}
				if topic && exact < 0 {
					exact = i
				}
				// A table of contents locates passages but does not explain
				// the topic. Prefer eligible body evidence of that document.
				if !isContentsOnlyPassage(row.Content) {
					exact = i
					break
				}
			}
			if exact >= 0 {
				add(exact)
				// At most one additional illustrated passage of this exact
				// topic document; leave the remaining slots for other sources.
				// Every candidate has already passed the ordinary thresholds.
				if topic && diversify && count >= 3 && !hasPassageImage(results[exact].Content) {
					for _, i := range indices {
						if key(i) == key(exact) && hasPassageImage(results[i].Content) && !isContentsOnlyPassage(results[i].Content) {
							add(i)
							break
						}
					}
				}
			}
		}
		if knowledge && diversify {
			// Avoid trading a strong passage for a barely-related document merely to
			// increase source count. Every candidate already passed the model threshold.
			floor := 0.0
			if len(indices) > 0 {
				floor = results[indices[0]].Score * 0.75
			}
			for _, i := range indices {
				if !docs[key(i)] && results[i].Score >= floor {
					add(i)
				}
			}
		}
		var rest []*types.SearchResult
		var original []int
		for _, i := range indices {
			if !selected[i] {
				rest = append(rest, results[i])
				original = append(original, i)
			}
		}
		for _, i := range SelectMMR(ctx, rest, min(count-len(picks), len(rest)), DefaultMMRLambda) {
			add(original[i])
		}
		return picks
	}
	picks := choose(internal, kbCount, true)
	picks = append(picks, choose(web, webCount, false)...)
	return picks
}

var contentsEntryRE = regexp.MustCompile(`(?:\.{2,}|…+)\s*[0-9]+`)

func isContentsOnlyPassage(content string) bool {
	lower := strings.ToLower(content)
	return (strings.Contains(content, "目录") || strings.Contains(lower, "contents")) && len(contentsEntryRE.FindAllString(content, 4)) >= 3
}
func hasPassageImage(content string) bool {
	return searchutil.MarkdownImageRegex.MatchString(content) || searchutil.HTMLImageSrcRegex.MatchString(content)
}
