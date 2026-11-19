package repository

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/searchutil"
)

const (
	namedOverviewCandidatesPerDocument = 32
	namedDetailCandidatesPerDocument   = 128
	namedOverviewPassagesPerDocument   = 3
)

type documentTitlePassage struct {
	ID, KnowledgeID, KnowledgeBaseID, Content, TagID, Title string
	TitleExact                                              int
}

// Rank a bounded set of passages from one resolved document by wording shared
// with its own subquestion. A cover or TOC alone is insufficient.
func selectDocumentDetailPassages(question string, passages []documentTitlePassage) []documentTitlePassage {
	grams := make(map[string]int)
	for _, phrase := range strings.FieldsFunc(strings.ToLower(question), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		chars := []rune(phrase)
		for width := 3; width <= min(7, len(chars)); width++ {
			for start := 0; start+width <= len(chars); start++ {
				grams[string(chars[start:start+width])] = width * width
			}
		}
	}
	type ranked struct {
		row   documentTitlePassage
		score int
	}
	var candidates []ranked
	for _, row := range passages {
		lines := overviewBodyLines(row.Content)
		body := strings.Join(strings.Fields(strings.ToLower(strings.Join(lines, " "))), "")
		score := 0
		for term, weight := range grams {
			if strings.Contains(body, term) {
				score += weight
			}
		}
		if score > 0 {
			candidates = append(candidates, ranked{row, score})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	out := make([]documentTitlePassage, min(len(candidates), namedOverviewPassagesPerDocument))
	for i := range out {
		out[i] = candidates[i].row
	}
	return out
}

var (
	overviewHeading   = regexp.MustCompile(`(?i)^(?:#{1,6})?(?:[0-9]+[0-9.．、)）]*)?(目的|适用范围|范围|purpose|scope)[.．:：]?$`)
	overviewTOCLeader = regexp.MustCompile(`[.·…．]{2,}\s*[0-9]*\s*$`)
)

func overviewBodyLines(content string) []string {
	var lines []string
	for _, text := range strings.Split(content, "\n") {
		line := strings.TrimSpace(text)
		if line == "" || overviewTOCLeader.MatchString(line) || strings.HasPrefix(line, "![") ||
			strings.HasPrefix(line, "文件名称") || strings.HasPrefix(line, "文件编号") ||
			strings.TrimSpace(strings.TrimLeft(line, "#")) == "目录" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func hasOverviewProse(lines []string, minimumLength int) bool {
	for _, line := range lines {
		if utf8.RuneCountInString(line) >= minimumLength && strings.ContainsAny(line, "。；，:;.") {
			return true
		}
	}
	return false
}

func overviewPurposeScore(lines []string) int {
	score := 0
	for i, line := range lines {
		if overviewHeading.MatchString(strings.Join(strings.Fields(line), "")) && hasOverviewProse(lines[i+1:], 8) {
			score++
		}
	}
	return score
}

var overviewAspects = []struct {
	question []string
	evidence []string
}{
	{
		question: []string{"选材", "选用", "选型"},
		evidence: []string{"材料选用规范", "选用规范", "选型规范", "选用应", "选用规范参考", "选型要求", "优先选用", "应用场景", "材料标识"},
	},
	{
		question: []string{"质量确认", "质量检验", "来料", "验收", "责任", "职责"},
		evidence: []string{
			"职责分工", "部门职责", "材料检验规范", "原材料通用规范要求", "检测方法",
			"提供相关材料检验报告", "供应商提供", "随机抽检", "检验报告", "来料检验", "确认阶段",
		},
	},
}

func countOverviewTerms(body string, terms []string) int {
	score := 0
	for _, term := range terms {
		if strings.Contains(body, term) {
			score++
		}
	}
	return score
}

// Select the purpose and the best supporting passages for requested aspects
// from one exact authorized document. A chunk can contain both TOC and prose.
func selectNamedOverviewPassages(question string, passages []documentTitlePassage) []documentTitlePassage {
	question = searchutil.DocumentOverviewQuestionBody(question)
	lines := make([][]string, len(passages))
	purpose, purposeScore := -1, 0
	for i, passage := range passages {
		lines[i] = overviewBodyLines(passage.Content)
		if score := overviewPurposeScore(lines[i]); score > purposeScore {
			purpose, purposeScore = i, score
		}
	}
	out := make([]documentTitlePassage, 0, namedOverviewPassagesPerDocument)
	selected := make([]bool, len(passages))
	add := func(index int) {
		if index >= 0 && !selected[index] && len(out) < namedOverviewPassagesPerDocument {
			out = append(out, passages[index])
			selected[index] = true
		}
	}
	add(purpose)
	for _, aspect := range overviewAspects {
		if countOverviewTerms(question, aspect.question) == 0 {
			continue
		}
		body, bodyScore := -1, 0
		for i, ls := range lines {
			if selected[i] || (purpose >= 0 && i < purpose) || !hasOverviewProse(ls, 20) {
				continue
			}
			content := strings.Join(strings.Fields(strings.Join(ls, " ")), "")
			if score := countOverviewTerms(content, aspect.evidence); score > bodyScore {
				body, bodyScore = i, score
			}
		}
		add(body)
	}
	// Keep a supporting identity/header if the question did not fill the budget.
	for i, row := range passages {
		content := strings.Join(strings.Fields(row.Content), "")
		if strings.Contains(content, "文件编号") || strings.Contains(strings.ToLower(content), "documentnumber") {
			add(i)
			break
		}
	}
	for i, ls := range lines {
		if i > purpose && hasOverviewProse(ls, 8) {
			add(i)
		}
	}
	for i := range passages {
		if len(out) == namedOverviewPassagesPerDocument {
			break
		}
		if len(lines[i]) > 0 {
			add(i)
		}
	}
	return out
}
