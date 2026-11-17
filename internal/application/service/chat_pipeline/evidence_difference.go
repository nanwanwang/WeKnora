package chatpipeline

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	ruleQuestionRE  = regexp.MustCompile(`(?i)边界|界限|分界|恰好|正好|刚好|等于|判定|归类|划分|对照|不一致|不同|矛盾|冲突|哪个|哪种|还是|能否|能按|验收|\b(?:exactly|boundary|classif\w*|conflict\w*|inconsistent|which|compare)\b`)
	identifierRE    = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9._-]*(?:/[A-Za-z0-9][A-Za-z0-9._-]*){2,}`)
	namedLimitRE    = regexp.MustCompile(`(最小|最大|最低|最高)([\p{Han}]{1,14}?)(?:要求)?(?:为|是|[:=])\s*(` + evidenceNumberPattern + `)\s*(` + evidenceUnitPattern + `)`)
	clauseSeparator = regexp.MustCompile(`[。；;]|\n\s*\n`)
	descriptorRE    = regexp.MustCompile(`^为([^,，。;；]{2,18})[,，]`)
)

type evidenceExcerpt struct {
	text, ref string
}

type evidenceDifference struct {
	chinese, english string
	excerpts         []evidenceExcerpt
}

func findEvidenceDifference(query string, sources []reviewSource) evidenceDifference {
	if !ruleQuestionRE.MatchString(query) {
		return evidenceDifference{}
	}
	points := quantitiesIn(query)
	if observed := compositeQuantityDifference(points, sources); len(observed.excerpts) != 0 {
		return observed
	}
	if observed := inclusiveBoundaryDifference(points, sources); len(observed.excerpts) != 0 {
		return observed
	}
	return namedLimitDifference(query, sources)
}

func compositeQuantityDifference(points map[evidenceQuantity]bool, sources []reviewSource) evidenceDifference {
	for _, source := range sources {
		for _, block := range clauseSeparator.Split(source.body, -1) {
			quote := evidenceText(block)
			if utf8.RuneCountInString(quote) > 380 {
				continue
			}
			type pair struct{ first, second evidenceQuantity }
			sides := map[pair]string{}
			for _, m := range compoundRE.FindAllStringSubmatch(comparisonText(quote), -1) {
				first, second := parseEvidenceQuantity(m[2], m[3]), parseEvidenceQuantity(m[4], m[5])
				if first.unit == second.unit || !points[second] || hasSeparateComparison(sources, source.document, second) {
					continue
				}
				key, direction := pair{first, second}, m[1][:1]
				if previous := sides[key]; previous != "" && previous != direction {
					return evidenceDifference{
						chinese:  "原文将两个量并列写出，比较符位于前一个量前。后一项没有独立的比较方向，这一项的判定待确认。",
						english:  "The source lists two quantities after one comparator. The rule for the second quantity needs confirmation.",
						excerpts: []evidenceExcerpt{{text: quote, ref: source.ref}},
					}
				}
				sides[key] = direction
			}
		}
	}
	return evidenceDifference{}
}

func hasSeparateComparison(sources []reviewSource, document string, point evidenceQuantity) bool {
	for _, source := range sources {
		if source.document != document {
			continue
		}
		for _, candidate := range comparisonsIn(comparisonText(evidenceText(source.body))) {
			if candidate.quantity == point {
				return true
			}
		}
	}
	return false
}

type limitKey struct {
	document string
	subject  string
	point    evidenceQuantity
}

func inclusiveBoundaryDifference(points map[evidenceQuantity]bool, sources []reviewSource) evidenceDifference {
	if len(points) == 0 {
		return evidenceDifference{}
	}
	search := make(map[limitKey]map[string]evidenceExcerpt)
	var keys []limitKey
	for _, source := range sources {
		lines := strings.Split(source.body, "\n")
		for index, line := range lines {
			normalized := comparisonText(evidenceText(line))
			for _, observed := range comparisonsIn(normalized) {
				if !points[observed.quantity] {
					continue
				}
				subject := comparisonSubject(normalized[:observed.index])
				length := utf8.RuneCountInString(subject)
				if length < 2 || length > 48 {
					continue
				}
				key := limitKey{document: source.document, subject: subject, point: observed.quantity}
				if search[key] == nil {
					search[key] = map[string]evidenceExcerpt{}
					keys = append(keys, key)
				}
				if search[key][observed.operator].text == "" {
					quote := boundaryExcerpt(lines, index)
					search[key][observed.operator] = evidenceExcerpt{text: quote, ref: source.ref}
				}
			}
		}
	}
	for _, key := range keys {
		operators := search[key]
		for _, side := range []struct{ strict, inclusive, otherStrict, otherInclusive string }{
			{"<", "<=", ">", ">="}, {">", ">=", "<", "<="},
		} {
			first, second := operators[side.strict], operators[side.inclusive]
			if first.text == "" || second.text == "" ||
				(operators[side.otherStrict].text == "" && operators[side.otherInclusive].text == "") {
				continue
			}
			return evidenceDifference{
				chinese:  "检索到的原文对同一数值边界用了不同的比较符。等值处适用哪项规定待确认。",
				english:  "The retrieved source uses different comparison operators for the same boundary. The applicable rule at equality needs confirmation.",
				excerpts: []evidenceExcerpt{first, second},
			}
		}
	}
	return evidenceDifference{}
}

func comparisonSubject(prefix string) string {
	if cut := strings.LastIndexAny(prefix, ",，:：|;；。"); cut >= 0 {
		prefix = prefix[cut+1:]
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	for _, start := range []string{"如果", "当", "若", "when ", "if "} {
		prefix = strings.TrimPrefix(prefix, start)
	}
	return strings.Join(strings.Fields(prefix), "")
}

func boundaryExcerpt(lines []string, index int) string {
	end := index + 1
	for end < len(lines) && end-index < 5 && strings.TrimSpace(lines[end]) != "" {
		end++
	}
	return withinRunes(evidenceText(strings.Join(lines[index:end], "\n")), 280)
}

type namedLimitKey struct {
	document, extremum, subject, unit string
}

type namedLimitWitness struct {
	value, description string
	excerpt            evidenceExcerpt
}

func namedLimitDifference(query string, sources []reviewSource) evidenceDifference {
	for _, item := range identifierRE.FindAllString(query, -1) {
		byLimit := map[namedLimitKey]namedLimitWitness{}
		for _, source := range sources {
			for _, clause := range clauseSeparator.Split(source.body, -1) {
				plain := evidenceText(clause)
				for _, match := range identifierRE.FindAllStringIndex(plain, -1) {
					if !sameEvidenceIdentifier(plain[match[0]:match[1]], item) {
						continue
					}
					tail := withinRunes(strings.TrimSpace(plain[match[1]:]), 150)
					next := identifierRE.FindStringIndex(tail)
					if next != nil {
						tail = tail[:next[0]]
					}
					value := namedLimitRE.FindStringSubmatch(comparisonText(tail))
					if value == nil || !strings.Contains(strings.Join(strings.Fields(query), ""), value[2]) {
						continue
					}
					q := parseEvidenceQuantity(value[3], value[4])
					key := namedLimitKey{source.document, value[1], value[2], q.unit}
					current := namedLimitWitness{value: q.number, excerpt: evidenceExcerpt{
						text: withinRunes(plain[match[0]:match[1]]+" "+tail, 175), ref: source.ref,
					}}
					if descriptor := descriptorRE.FindStringSubmatch(comparisonText(tail)); descriptor != nil {
						current.description = descriptor[1]
					}
					if previous, ok := byLimit[key]; ok && previous.value != current.value {
						list := []evidenceExcerpt{previous.excerpt, current.excerpt}
						list = append(list, relatedLimitExcerpts(sources, item, key, previous, current)...)
						return evidenceDifference{
							chinese:  "同一标识的数值要求在本轮引用中出现了不同写法，正式判定值待确认。",
							english:  "The retrieved source gives different values for the same named requirement. The applicable value needs confirmation.",
							excerpts: list,
						}
					}
					byLimit[key] = current
				}
			}
		}
	}
	return evidenceDifference{}
}

func sameEvidenceIdentifier(inSource, inQuestion string) bool {
	source, question := strings.ToLower(inSource), strings.ToLower(inQuestion)
	return source == question || strings.HasSuffix(source, "-"+question)
}

func relatedLimitExcerpts(sources []reviewSource, item string, key namedLimitKey, a, b namedLimitWitness) []evidenceExcerpt {
	var related []evidenceExcerpt
	for _, source := range sources {
		if source.document != key.document {
			continue
		}
		for _, raw := range clauseSeparator.Split(source.body, -1) {
			clause := evidenceText(raw)
			if utf8.RuneCountInString(clause) > 220 {
				continue
			}
			kind := strings.Join(strings.Fields(comparisonText(clause)), "")
			if !strings.Contains(kind, "以上") && !strings.Contains(kind, "以下") {
				continue
			}
			mark := strings.Index(strings.ToLower(clause), strings.ToLower(item))
			// A table entry may put the numerical requirement before the identifier.
			table := mark >= 0 && utf8.RuneCountInString(clause[:mark]) <= 95
			prose := mark < 0 && strings.Contains(clause, key.subject) &&
				((a.description != "" && strings.Contains(kind, a.description)) ||
					(b.description != "" && strings.Contains(kind, b.description)))
			if !table && !prose {
				continue
			}
			quantities := quantitiesIn(clause)
			if quantities[evidenceQuantity{a.value, key.unit}] || quantities[evidenceQuantity{b.value, key.unit}] {
				related = append(related, evidenceExcerpt{text: clause, ref: source.ref})
			}
			if len(related) == 2 {
				return related
			}
		}
	}
	return related
}

func renderEvidenceDifference(difference evidenceDifference, han bool) string {
	message := difference.english
	heading := "Retrieved source excerpts"
	if han {
		message, heading = difference.chinese, "检索到的原文"
	}
	var lines []string
	for _, excerpt := range difference.excerpts {
		line := "- " + escapeEvidenceQuote(excerpt.text)
		if excerpt.ref != "" {
			line += " " + excerpt.ref
		}
		lines = append(lines, line)
	}
	return message + "\n\n**" + heading + "**\n\n" + strings.Join(lines, "\n")
}
