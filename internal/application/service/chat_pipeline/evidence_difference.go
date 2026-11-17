package chatpipeline

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/searchutil"
)

var (
	ruleQuestionRE   = regexp.MustCompile(`(?i)边界|界限|分界|恰好|正好|刚好|等于|判定|归类|划分|对照|不一致|不同|矛盾|冲突|哪个|哪种|还是|能否|能按|验收|\b(?:exactly|boundary|classif\w*|conflict\w*|inconsistent|which|compare)\b`)
	identifierRE     = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9._-]*(?:/[A-Za-z0-9][A-Za-z0-9._-]*){2,}`)
	namedLimitRE     = regexp.MustCompile(`(最小|最大|最低|最高)([\p{Han}]{1,14}?)(?:要求)?(?:为|是|[:=])\s*(` + evidenceNumberPattern + `)\s*(` + evidenceUnitPattern + `)`)
	clauseSeparator  = regexp.MustCompile(`[。；;]|\n\s*\n`)
	descriptorRE     = regexp.MustCompile(`^为([^,，。;；]{2,18})[,，]`)
	htmlRowTagRE     = regexp.MustCompile(`(?i)</?tr\b[^>]*>`)
	htmlCellTagRE    = regexp.MustCompile(`(?i)</?t[dh]\b[^>]*>`)
	qualifierRangeRE = regexp.MustCompile(`(` + evidenceNumberPattern + `)\s*(` + evidenceUnitPattern + `)?\s*[~–—\-至到]\s*(` + evidenceNumberPattern + `)\s*(` + evidenceUnitPattern + `)?`)
	copySuffixRE     = regexp.MustCompile(`\([0-9]+\)$`)
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
	if observed := inclusiveBoundaryDifference(query, points, sources); len(observed.excerpts) != 0 {
		return observed
	}
	return namedLimitDifference(query, sources)
}

func compositeQuantityDifference(points map[evidenceQuantity]bool, sources []reviewSource) evidenceDifference {
	for _, source := range sources {
		if source.imageOCR {
			continue
		}
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
		if source.document != document || source.imageOCR {
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

type boundaryWitness struct {
	text, ocr       evidenceExcerpt
	tableQualifiers []string
	ocrContext      string
}

var boundarySides = [...]struct{ strict, inclusive, otherStrict, otherInclusive string }{
	{"<", "<=", ">", ">="}, {">", ">=", "<", "<="},
}

func inclusiveBoundaryDifference(query string, points map[evidenceQuantity]bool, sources []reviewSource) evidenceDifference {
	if len(points) == 0 {
		return evidenceDifference{}
	}
	search := make(map[limitKey]map[string]boundaryWitness)
	titles := make(map[string]string)
	var keys []limitKey
	for _, source := range sources {
		titles[source.document] = source.title
		lines := strings.Split(htmlRowTagRE.ReplaceAllString(source.body, "\n"), "\n")
		for index, line := range lines {
			// Keep the cell boundaries when extracting a subject. Otherwise a
			// merged row label becomes part of the name of the next column.
			tableLine := htmlCellTagRE.ReplaceAllString(line, "|")
			normalized := comparisonText(evidenceText(tableLine))
			for _, observed := range comparisonsIn(normalized) {
				if !points[observed.quantity] {
					continue
				}
				prefix := normalized[:observed.index]
				var qualifiers []string
				if tableLine != line {
					qualifiers = boundaryRowQualifiers(prefix)
					if !boundaryQualifiersApply(qualifiers, query) {
						continue
					}
				}
				subject := comparisonSubject(prefix)
				length := utf8.RuneCountInString(subject)
				if length < 2 || length > 48 {
					continue
				}
				key := limitKey{document: source.document, subject: subject, point: observed.quantity}
				if search[key] == nil {
					search[key] = map[string]boundaryWitness{}
					keys = append(keys, key)
				}
				witness := search[key][observed.operator]
				if source.imageOCR && witness.ocr.text == "" {
					witness.ocr = evidenceExcerpt{text: boundaryExcerpt(lines, index), ref: source.ref}
					witness.ocrContext = source.body
				} else if !source.imageOCR && witness.text.text == "" {
					witness.text = evidenceExcerpt{text: boundaryExcerpt(lines, index), ref: source.ref}
					witness.tableQualifiers = qualifiers
				}
				search[key][observed.operator] = witness
			}
		}
	}
	for _, key := range keys {
		operators := search[key]
		for _, side := range boundarySides {
			strict, inclusive := operators[side.strict], operators[side.inclusive]
			if operators[side.otherStrict].text.text == "" && operators[side.otherInclusive].text.text == "" {
				continue
			}
			first, second := strict.text, inclusive.text
			ocr := false
			if first.text == "" && second.text != "" && strict.ocr.text != "" &&
				boundaryQualifiersApply(inclusive.tableQualifiers, strict.ocrContext) {
				first, ocr = strict.ocr, true
			} else if first.text != "" && second.text == "" && inclusive.ocr.text != "" &&
				boundaryQualifiersApply(strict.tableQualifiers, inclusive.ocrContext) {
				second, ocr = inclusive.ocr, true
			}
			if first.text == "" || second.text == "" {
				continue
			}
			if ocr {
				return evidenceDifference{
					chinese:  "检索到的正文与图中文字识别结果对同一数值边界用了不同的比较符。等值处的规定待确认，请核对原图。",
					english:  "The text and the figure transcription use different comparison operators for the same boundary. Check the original figure to confirm the rule at equality.",
					excerpts: []evidenceExcerpt{first, second},
				}
			}
			return evidenceDifference{
				chinese:  "检索到的原文对同一数值边界用了不同的比较符。等值处适用哪项规定待确认。",
				english:  "The retrieved source uses different comparison operators for the same boundary. The applicable rule at equality needs confirmation.",
				excerpts: []evidenceExcerpt{first, second},
			}
		}
	}
	return compareSeparateBoundarySources(query, keys, search, titles)
}

// Retrieval can return the table from one copy and the diagram from another.
// Require a complete textual rule and a self-contained OCR overlap, and cite
// them as separate sources. Neither establishes which document is authoritative.
func compareSeparateBoundarySources(query string, keys []limitKey, search map[limitKey]map[string]boundaryWitness, titles map[string]string) evidenceDifference {
	var requested string
	if strings.Contains(query, "《") {
		name, _ := searchutil.DocumentTitleHint(query)
		if name == "" {
			return evidenceDifference{}
		}
		requested = copySuffixRE.ReplaceAllString(comparisonText(name), "")
	}
	type scopeKey struct {
		library, title, subject string
		point                   evidenceQuantity
	}
	byScope := make(map[scopeKey][]limitKey)
	keyScope := make(map[limitKey]scopeKey)
	for _, key := range keys {
		lib, _, ok := strings.Cut(key.document, "/")
		title := copySuffixRE.ReplaceAllString(comparisonText(searchutil.NormalizeDocumentTitle(titles[key.document])), "")
		if !ok || title == "" || (requested != "" && requested != title) {
			continue
		}
		scope := scopeKey{lib, title, key.subject, key.point}
		byScope[scope] = append(byScope[scope], key)
		keyScope[key] = scope
	}
	for _, key := range keys {
		for _, side := range boundarySides {
			written := search[key][side.strict]
			opposite := search[key][side.otherInclusive].text
			if written.text.text == "" || opposite.text == "" || len(written.tableQualifiers) == 0 {
				continue
			}
			for _, other := range byScope[keyScope[key]] {
				if other.document == key.document {
					continue
				}
				image := search[other][side.inclusive]
				otherImage := search[other][side.otherInclusive].ocr
				if image.ocr.text == "" || image.ocr.ref == "" || image.ocr.ref != otherImage.ref ||
					!boundaryQualifiersApply(written.tableQualifiers, image.ocrContext) {
					continue
				}
				return evidenceDifference{
					chinese:  "两份资料中的表格和图中文字识别结果使用了不同的边界写法。等值处的规定待确认，请分别核对原图和适用版本。",
					english:  "The table and figure transcription from separate sources use different boundary operators. Check the original figure and the applicable version before deciding the rule at equality.",
					excerpts: []evidenceExcerpt{written.text, opposite, image.ocr, otherImage},
				}
			}
		}
	}
	return evidenceDifference{}
}

// An earlier table cell can qualify the rule in the current cell. Do not join
// a qualified table rule to a diagram or question for a different condition.
func boundaryRowQualifiers(prefix string) []string {
	cells := strings.Split(prefix, "|")
	if len(cells) < 3 {
		return nil
	}
	var qualifiers []string
	for _, cell := range cells[1 : len(cells)-1] {
		if strings.TrimSpace(cell) != "" {
			qualifiers = append(qualifiers, cell)
		}
	}
	return qualifiers
}

func boundaryQualifiersApply(qualifiers []string, context string) bool {
	if len(qualifiers) == 0 {
		return true
	}
	context = comparisonText(context)
	contextText := strings.Join(strings.Fields(context), "")
	var ranges map[[2]evidenceQuantity]bool
	for _, cell := range qualifiers {
		qualifier := strings.Join(strings.Fields(cell), "")
		if strings.Contains(contextText, qualifier) {
			continue
		}
		// Match the entire range, not two quantities that appear in unrelated rules.
		key, ok := boundaryRangeKey(qualifierRangeRE.FindStringSubmatch(cell))
		if !ok {
			return false
		}
		if ranges == nil {
			ranges = make(map[[2]evidenceQuantity]bool)
			for _, m := range qualifierRangeRE.FindAllStringSubmatch(context, -1) {
				if candidate, ok := boundaryRangeKey(m); ok {
					ranges[candidate] = true
				}
			}
		}
		if !ranges[key] {
			return false
		}
	}
	return true
}

func boundaryRangeKey(m []string) ([2]evidenceQuantity, bool) {
	if len(m) != 5 || (m[2] == "" && m[4] == "") {
		return [2]evidenceQuantity{}, false
	}
	firstUnit, secondUnit := m[2], m[4]
	if firstUnit == "" {
		firstUnit = secondUnit
	}
	if secondUnit == "" {
		secondUnit = firstUnit
	}
	return [2]evidenceQuantity{parseEvidenceQuantity(m[1], firstUnit), parseEvidenceQuantity(m[3], secondUnit)}, true
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
			if source.imageOCR {
				continue
			}
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
		if source.document != key.document || source.imageOCR {
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
