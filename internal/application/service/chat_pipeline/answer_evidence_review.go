package chatpipeline

import (
	"html"
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/types"
)

var explicitFormatRE = regexp.MustCompile(`(?i)\b(?:json|yaml|csv|tsv|xml)\b`)

type reviewSource struct {
	document string
	body     string
	header   string
	ref      string
}

type answerEvidenceReview struct {
	pending string
	rows    []evidenceRuleRow
	han     bool
}

type evidenceRuleRow struct {
	text        string
	ref         string
	comparisons []evidenceComparison
}

func reviewAnswerEvidence(request *types.ChatManage, registry *modelcontext.Registry) answerEvidenceReview {
	query := strings.TrimSpace(request.Query)
	if query == "" || len(request.Images) != 0 || explicitFormatRE.MatchString(query) {
		return answerEvidenceReview{}
	}
	var sources []reviewSource
	for _, r := range expandCitationSources(orderedPipelineReferences(request)) {
		if r == nil || r.KnowledgeBaseID == "" || r.KnowledgeID == "" ||
			r.ChunkType != string(types.ChunkTypeText) || len(r.Content) > 32<<10 || registry.ChunkHandle(r.ID) == "" {
			continue
		}
		sources = append(sources, reviewSource{
			document: r.KnowledgeBaseID + "/" + r.KnowledgeID, body: r.Content, header: r.ContextHeader,
			ref: registry.CitationTemplate(registry.ChunkHandle(r.ID)),
		})
	}
	han := hasHan(query)
	review := answerEvidenceReview{han: han}
	if difference := findEvidenceDifference(query, sources); len(difference.excerpts) != 0 {
		review.pending = renderEvidenceDifference(difference, han)
		return review
	}
	for _, source := range sources {
		citation := registry.DecodeOutputText(source.ref)
		if citation == "" {
			continue
		}
		body := chunker.PresentTableEvidence(source.body, source.header)
		for _, raw := range strings.Split(body, "\n") {
			line := evidenceText(raw)
			if line == "" || withinRunes(line, 260) != line {
				continue
			}
			comparisons := distinctComparisons(comparisonsIn(comparisonText(line)))
			if len(comparisons) > 2 {
				review.rows = append(review.rows, evidenceRuleRow{text: line, ref: citation, comparisons: comparisons})
			}
		}
	}
	return review
}

func distinctComparisons(comparisons []evidenceComparison) []evidenceComparison {
	distinct := make([]evidenceComparison, 0, len(comparisons))
	for _, c := range comparisons {
		duplicate := false
		for _, existing := range distinct {
			if existing.operator == c.operator && existing.quantity == c.quantity {
				duplicate = true
				break
			}
		}
		if !duplicate {
			distinct = append(distinct, c)
		}
	}
	return distinct
}

// Supplement only an already cited row. Two matching conditions identify the
// row; if they could point to different remaining values, leave it alone.
func (review answerEvidenceReview) supplement(answer string) string {
	if len(review.rows) == 0 || len(answer) > 64<<10 {
		return ""
	}
	var recognized []evidenceRuleRow
	used := comparisonsIn(comparisonText(html.UnescapeString(citationRE.ReplaceAllString(answer, ""))))
	for _, row := range review.rows {
		if !strings.Contains(answer, row.ref) {
			continue
		}
		matched := 0
		for _, wanted := range row.comparisons {
			for _, observed := range used {
				if observed.quantity == wanted.quantity && observed.operator == wanted.operator {
					matched++
					break
				}
			}
		}
		if matched >= 2 {
			recognized = append(recognized, row)
		}
	}
	var lines []string
	for _, row := range recognized {
		missing, ambiguous := false, false
		for _, want := range row.comparisons {
			found := false
			for _, got := range used {
				found = found || (want.operator == got.operator && want.quantity == got.quantity)
			}
			if !found {
				missing = true
				break
			}
		}
		if !missing {
			continue
		}
		for _, other := range recognized {
			if row.text != other.text && sharedComparisons(row, other) >= 2 {
				ambiguous = true
				break
			}
		}
		if !ambiguous {
			line := "- " + escapeEvidenceQuote(strings.TrimPrefix(row.text, "- ")) + " " + row.ref
			if len(lines) == 0 || lines[len(lines)-1] != line {
				lines = append(lines, line)
			}
		}
		if len(lines) == 4 {
			break
		}
	}
	if len(lines) == 0 {
		return ""
	}
	heading := "Complete conditions in the cited row"
	if review.han {
		heading = "引用行的完整条件"
	}
	return "\n\n**" + heading + "**\n\n" + strings.Join(lines, "\n")
}

func sharedComparisons(a, b evidenceRuleRow) int {
	count := 0
	for _, left := range a.comparisons {
		for _, right := range b.comparisons {
			if left.quantity == right.quantity && left.operator == right.operator {
				count++
				break
			}
		}
	}
	return count
}

var evidenceQuoteEscapes = strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "*", "\\*", "`", "\\`", "|", "\\|")

func escapeEvidenceQuote(quote string) string {
	return evidenceQuoteEscapes.Replace(html.EscapeString(quote))
}
