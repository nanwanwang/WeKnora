package chatpipeline

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/searchutil"
)

var (
	verifiedTorqueQuery = regexp.MustCompile(`母排(?:螺栓)?|母线(?:螺栓)?`)
	verifiedTorqueScope = regexp.MustCompile(`(?i)Gleichstrom\s+und\s+für\s+Wechselstrom\s+bis\s+([0-9]+)\s*Hz`)
	torqueSplitCells    = regexp.MustCompile(`(?is)<t[dh]\b[^>]*>(.*?)</t[dh]>`)
	torqueBodyRow       = regexp.MustCompile(`(?is)<tr\b[^>]*>.*?</tr>`)
	torqueExampleSpec   = regexp.MustCompile(`(?i)^M\s*[0-9]+$`)
	torqueAskedSpec     = regexp.MustCompile(`(?i)\bM\s*[0-9]+\b`)
	torqueCellNumber    = regexp.MustCompile(`^[0-9]+(?:[.,][0-9]+)?$`)
	torqueWasher        = regexp.MustCompile(
		`(?i)Bei Gewinde (M\s*[0-9]+) und Stromschienen aus (.{1,120}?) sind zusätzliche Scheiben erforderlich`,
	)
	torqueTableNumber = regexp.MustCompile(`(?i)Tabelle\s+([0-9]+)\b`)
	torqueSpacedBis   = regexp.MustCompile(`\bbis([A-Z])`)
	overviewPurpose   = regexp.MustCompile(
		`(?i)^(?:#{1,6}\s*)?(?:[0-9]+[.、]?[ \t]*)?(?:目的|purpose)(?:[：:][ \t]*|[ \t]+|$)(.*)$`,
	)
	documentCode = regexp.MustCompile(
		`(?i)(?:文件编号|document (?:no\.?|number))\s*[:：]?\s*([A-Z]{1,8}[A-Z0-9-]*[0-9][A-Z0-9#-]*)\b`,
	)
	spacedMicro = regexp.MustCompile(`[µμ]\s+m\b`)
)

// An acceptance question may name the material instead of its identifier. Infer
// a candidate only from an explicit descriptor in that document, then reuse the
// same-document, same-identifier check for two different values of the same limit.
func inferredAcceptanceDifference(query string, sources []reviewSource) evidenceDifference {
	if !strings.Contains(query, "验收") || identifierRE.MatchString(query) {
		return evidenceDifference{}
	}
	type candidate struct{ document, identifier string }
	candidates := make(map[candidate]struct{})
	for _, source := range sources {
		if source.imageOCR || source.ref == "" {
			continue
		}
		for _, raw := range clauseSeparator.Split(source.body, -1) {
			plain := evidenceText(raw)
			for _, m := range identifierRE.FindAllStringIndex(plain, -1) {
				tail := withinRunes(strings.TrimSpace(plain[m[1]:]), 150)
				d := descriptorRE.FindStringSubmatch(tail)
				if d == nil {
					continue
				}
				descriptor := strings.TrimPrefix(d[1], "镀")
				if utf8.RuneCountInString(descriptor) >= 3 && strings.Contains(query, descriptor) &&
					namedLimitRE.MatchString(comparisonText(spacedMicro.ReplaceAllString(tail, "μm"))) {
					candidates[candidate{source.document, plain[m[0]:m[1]]}] = struct{}{}
				}
			}
		}
	}
	var verified evidenceDifference
	for c := range candidates {
		var scoped []reviewSource
		for _, source := range sources {
			if source.document == c.document && source.ref != "" {
				source.body = spacedMicro.ReplaceAllString(source.body, "μm")
				scoped = append(scoped, source)
			}
		}
		found := namedLimitDifference(query+" "+c.identifier, scoped)
		if len(found.excerpts) == 0 {
			continue
		}
		if len(verified.excerpts) != 0 {
			return evidenceDifference{}
		}
		verified = found
	}
	if len(verified.excerpts) > 0 {
		verified.chinese = "同一文件对同一标识给出的验收值不一致。最终验收口径须由文件归口部门确认。"
	}
	return verified
}

// Render a torque row only when the header, row and washer rule are from
// the same table region of one explicitly named source. The preceding scope
// paragraph can have a separate locator on the same page.
func renderVerifiedTorque(query string, sources []reviewSource) string {
	asksTorque := strings.Contains(query, "力矩") || strings.Contains(query, "扭矩")
	if !verifiedTorqueQuery.MatchString(query) || !asksTorque {
		return ""
	}
	title, _ := searchutil.DocumentTitleHint(query)
	if title == "" || strings.Count(query, "《") != 1 {
		return ""
	}
	var sameDocument []reviewSource
	var document string
	for _, source := range sources {
		if source.ref == "" || source.imageOCR || searchutil.NormalizeDocumentTitle(source.title) != title {
			continue
		}
		if document != "" && document != source.document {
			return ""
		}
		document = source.document
		sameDocument = append(sameDocument, source)
	}

	var heading reviewSource
	var table string
	for _, source := range sameDocument {
		plain := evidenceText(source.body)
		if !strings.Contains(plain, "Innenraum und Freiluft") {
			continue
		}
		if match := torqueTableNumber.FindStringSubmatch(plain); match != nil {
			if table != "" && (table != match[1] || !sharesTorqueLocation(source, heading, true)) {
				return ""
			}
			table, heading = match[1], source
		}
	}
	if table == "" {
		return ""
	}

	var frequency, rangeRef, washerThread, washerMaterial, washerRef string
	for _, source := range sameDocument {
		if !sharesTorqueLocation(source, heading, false) {
			continue
		}
		plain := evidenceText(source.body)
		if match := verifiedTorqueScope.FindStringSubmatch(plain); match != nil {
			if frequency != "" && frequency != match[1] {
				return ""
			}
			frequency, rangeRef = match[1], source.ref
		}
		match := torqueWasher.FindStringSubmatch(plain)
		if match != nil && sharesTorqueLocation(source, heading, true) {
			thread := normalizeTorqueThread(match[1])
			material := torqueSpacedBis.ReplaceAllString(match[2], `bis $1`)
			if washerRef != "" && (thread != washerThread || material != washerMaterial) {
				return ""
			}
			washerThread, washerMaterial, washerRef = thread, material, source.ref
		}
	}
	if frequency == "" || washerRef == "" {
		return ""
	}
	var requested string
	for _, spec := range torqueAskedSpec.FindAllString(query, -1) {
		thread := normalizeTorqueThread(spec)
		if requested != "" && requested != thread {
			return ""
		}
		requested = thread
	}
	for _, source := range sameDocument {
		if !sharesTorqueLocation(source, heading, true) {
			continue
		}
		for _, row := range torqueBodyRow.FindAllString(source.body, -1) {
			cells := torqueSplitCells.FindAllStringSubmatch(row, -1)
			if len(cells) < 3 || len(cells) > 4 {
				continue
			}
			thread := evidenceText(cells[len(cells)-3][1])
			indoor, mixed := evidenceText(cells[len(cells)-2][1]), evidenceText(cells[len(cells)-1][1])
			if !torqueExampleSpec.MatchString(thread) || !torqueCellNumber.MatchString(indoor) ||
				!torqueCellNumber.MatchString(mixed) {
				continue
			}
			thread = normalizeTorqueThread(thread)
			if requested != "" && thread != requested {
				continue
			}
			return "《" + escapeEvidenceQuote(strings.TrimSuffix(heading.title, ".pdf")) +
				"》附录表 " + table + " 给出了推荐额定拧紧力矩。 " + heading.ref +
				"\n\n适用范围：直流和不超过 " + frequency + " Hz 的交流。 " + rangeRef +
				"\n\n以 " + thread + " 为例，室内 " + strings.ReplaceAll(indoor, ",", ".") +
				" N·m，室内和露天 " + strings.ReplaceAll(mixed, ",", ".") + " N·m。 " + source.ref +
				"\n\n额外垫圈限于螺纹 " + washerThread + "，且母排材质为「" +
				escapeEvidenceQuote(washerMaterial) + "」。 " + washerRef
		}
	}
	return ""
}

func normalizeTorqueThread(text string) string {
	return strings.Join(strings.Fields(strings.ToUpper(text)), "")
}

func sharesTorqueLocation(a, b reviewSource, sameRegion bool) bool {
	if a.ref == b.ref {
		return true
	}
	for _, left := range a.locators {
		if left.Page <= 0 {
			continue
		}
		for _, right := range b.locators {
			if left.Page == right.Page && (!sameRegion || (left.SourceID != "" && left.SourceID == right.SourceID)) {
				return true
			}
		}
	}
	return false
}

type purposeWithCitation struct {
	text, ref string
}

func verifiedOverviewPurpose(body string) string {
	lines := strings.Split(body, "\n")
	for index, line := range lines {
		if strings.Contains(line, ".....") {
			continue
		}
		m := overviewPurpose.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		text := evidenceText(m[1])
		for _, following := range lines[index+1:] {
			following = strings.TrimSpace(following)
			if following == "" {
				if text != "" {
					break
				}
				continue
			}
			if strings.HasPrefix(following, "#") || strings.HasPrefix(following, "<") ||
				strings.Contains(following, "适用范围") || strings.HasPrefix(following, "![](") {
				break
			}
			text = strings.TrimSpace(text + " " + evidenceText(following))
			if strings.HasSuffix(text, "。") || strings.HasSuffix(text, "；") {
				break
			}
		}
		if n := utf8.RuneCountInString(text); n >= 8 && n <= 250 && !strings.Contains(text, ".....") {
			return text
		}
	}
	return ""
}

// Named purpose overviews retain each document as an independent source. Codes
// can only be emitted with evidence from the very same document.
func renderVerifiedNamedOverview(query string, sources []reviewSource) string {
	titles := searchutil.NamedDocumentOverviewTitles(query)
	if len(titles) == 0 || namedOverviewDetailsRequested(query) {
		return ""
	}
	var sections []string
	for _, title := range titles {
		var doc, heading string
		var purpose, code purposeWithCitation
		for _, source := range sources {
			if source.imageOCR || source.ref == "" || searchutil.NormalizeDocumentTitle(source.title) != title {
				continue
			}
			if doc != "" && doc != source.document {
				return ""
			}
			doc, heading = source.document, source.title
			if purpose.text == "" {
				purpose = purposeWithCitation{verifiedOverviewPurpose(source.body), source.ref}
			}
			for _, m := range documentCode.FindAllStringSubmatch(evidenceText(source.body), -1) {
				if code.text != "" && code.text != m[1] {
					return ""
				}
				code = purposeWithCitation{m[1], source.ref}
			}
		}
		if purpose.text == "" {
			return ""
		}
		label := "《" + escapeEvidenceQuote(strings.TrimSuffix(heading, ".pdf")) + "》"
		if code.text != "" {
			label += "（文件编号 " + escapeEvidenceQuote(code.text) + "）"
			if code.ref != purpose.ref {
				label += " " + code.ref
			}
		}
		sections = append(sections, "- "+label+"："+escapeEvidenceQuote(purpose.text)+" "+purpose.ref)
	}
	return strings.Join(sections, "\n")
}

// The verified purpose excerpts answer a document overview. Let the model
// use the additional passages when the question requests specific provisions.
func namedOverviewDetailsRequested(query string) bool {
	query = searchutil.DocumentOverviewQuestionBody(query)
	for _, aspect := range []string{"适用范围", "核心对象", "适用对象", "职责", "责任", "验收", "选材", "质量确认", "质量检验"} {
		if strings.Contains(query, aspect) {
			return true
		}
	}
	return false
}

func namedOverviewEvidenceInstruction(query string) string {
	if len(searchutil.NamedDocumentOverviewTitles(query)) == 0 || !namedOverviewDetailsRequested(query) {
		return ""
	}
	return "\n按题目所列的每份文档分别回答各项要求，给出各自原文引用。" +
		"问题要求列出用途、核心对象、适用范围或依据时，逐项写清。" +
		"把各文件的适用对象、职责、条件和阶段写在各自小节。" +
		"各份原文均有依据时，才概括共同要求。"
}
