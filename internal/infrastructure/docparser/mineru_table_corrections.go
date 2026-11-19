package docparser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	minerUTableCorrectionsEnv = "WEKNORA_MINERU_PDF_TABLE_CORRECTIONS_DIR"
	maxTableCorrectionsBytes  = 1 << 20
)

type minerUTableCorrection struct {
	Page      int       `json:"page"`
	MatchText []string  `json:"match_text"`
	HTML      string    `json:"html"`
	BBox      []float64 `json:"bbox"`
}

type minerUTableEdit struct {
	start, end int
	table      string
	locator    types.SourceLocator
}

// applyMinerUPDFTableCorrections runs before splitting and indexing. The
// server operator supplies verified tables in a local manifest.
func applyMinerUPDFTableCorrections(req *types.ReadRequest, result *types.ReadResult) error {
	directory := strings.TrimSpace(os.Getenv(minerUTableCorrectionsEnv))
	if directory == "" || minerUCleanFileType(req.FileType) != "pdf" {
		return nil
	}
	digest := sha256.Sum256(req.FileContent)
	file, err := os.Open(filepath.Join(directory, hex.EncodeToString(digest[:])+".json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open MinerU PDF table corrections: %w", err)
	}
	defer func() { _ = file.Close() }()
	payload, err := io.ReadAll(io.LimitReader(file, maxTableCorrectionsBytes+1))
	if err != nil {
		return fmt.Errorf("read MinerU PDF table corrections: %w", err)
	}
	if len(payload) > maxTableCorrectionsBytes {
		return fmt.Errorf("MinerU PDF table corrections exceed %d bytes", maxTableCorrectionsBytes)
	}
	var corrections []minerUTableCorrection
	if err := json.Unmarshal(payload, &corrections); err != nil {
		return fmt.Errorf("decode MinerU PDF table corrections: %w", err)
	}
	if len(corrections) == 0 {
		return nil
	}

	markdown := result.MarkdownContent
	tables := minerUCorrectionTableSpans(markdown)
	fences := fencedCodeBlockPattern.FindAllStringIndex(markdown, -1)
	edits := make([]minerUTableEdit, 0, len(corrections))
	for i, correction := range corrections {
		edit, err := correction.findEdit(markdown, tables, fences, result.SourceBlocks)
		if err != nil {
			return fmt.Errorf("MinerU PDF page %d table correction %d: %w", correction.Page, i+1, err)
		}
		edit.locator.SourceID = fmt.Sprintf("mineru:table-correction:%d:%d", correction.Page, i+1)
		edits = append(edits, edit)
	}
	return applyMinerUTableEdits(result, edits)
}

// A parser can emit a GFM table even when the source PDF uses merged cells.
// Keep HTML candidates and add only Markdown blocks with a valid delimiter row.
var minerUMarkdownTableBlockPattern = regexp.MustCompile(
	`(?m)^[ \t]*\|[^\r\n]*\|[ \t]*(?:\r?\n[ \t]*\|[^\r\n]*\|[ \t]*)+`,
)

func minerUCorrectionTableSpans(markdown string) [][]int {
	htmlTables := htmlTableBlockPattern.FindAllStringIndex(markdown, -1)
	tables := htmlTables
	for _, span := range minerUMarkdownTableBlockPattern.FindAllStringIndex(markdown, -1) {
		if indexInsideSpans(span[0], htmlTables) {
			continue
		}
		lines := strings.SplitN(markdown[span[0]:span[1]], "\n", 3)
		if len(lines) == 3 && markdownTableSeparatorPattern.MatchString(strings.TrimSuffix(lines[1], "\r")) {
			tables = append(tables, span)
		}
	}
	return tables
}

func (c minerUTableCorrection) findEdit(
	markdown string, tables, fences [][]int, sources []types.SourceBlock,
) (minerUTableEdit, error) {
	var edit minerUTableEdit
	if c.Page < 1 || !validCorrectionBBox(c.BBox) || len(c.MatchText) == 0 {
		return edit, fmt.Errorf("expected a PDF page, normalized bounding box and table markers")
	}
	replacement, err := validateCorrectionTable(c.HTML)
	if err != nil {
		return edit, err
	}
	markers := make([]string, len(c.MatchText))
	for i, marker := range c.MatchText {
		markers[i] = compactTableText(marker)
		if markers[i] == "" {
			return edit, fmt.Errorf("empty table marker")
		}
	}
	for _, span := range tables {
		if indexInsideSpans(span[0], fences) || !tableContainsMarker(markdown[span[0]:span[1]], markers) {
			continue
		}
		start := utf8.RuneCountInString(markdown[:span[0]])
		end := start + utf8.RuneCountInString(markdown[span[0]:span[1]])
		if tableConflictsWithPDFPage(start, end, c.Page, sources) {
			continue
		}
		if edit.table != "" {
			return edit, fmt.Errorf("table markers match multiple tables on the page")
		}
		edit = minerUTableEdit{start: start, end: end, table: replacement, locator: types.SourceLocator{
			Type: types.SourceLocatorPDF, Page: c.Page, BBox: c.BBox,
		}}
	}
	if edit.table == "" {
		return edit, fmt.Errorf("no table with the expected markers and page")
	}
	return edit, nil
}

func compactTableText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, text)
}

func tableContainsMarker(table string, markers []string) bool {
	tokenizer := html.NewTokenizer(strings.NewReader(table))
	var text strings.Builder
	for kind := tokenizer.Next(); kind != html.ErrorToken; kind = tokenizer.Next() {
		if kind == html.TextToken {
			text.Write(tokenizer.Text())
		}
	}
	compact := compactTableText(text.String())
	for _, marker := range markers {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

// Broken tables may have no aligned span. In that case the unique marker
// and exact PDF digest identify the table; the manifest supplies its locator.
func tableConflictsWithPDFPage(start, end, page int, sources []types.SourceBlock) bool {
	for _, source := range sources {
		locator := source.Locator
		if locator.Type != types.SourceLocatorPDF || locator.Page < 1 || source.End <= start || source.Start >= end {
			continue
		}
		if locator.Page != page {
			return true
		}
	}
	return false
}

func validCorrectionBBox(box []float64) bool {
	return len(box) == 4 && 0 <= box[0] && box[0] < box[2] && box[2] <= 1 &&
		0 <= box[1] && box[1] < box[3] && box[3] <= 1
}

// Keep spans and escaped cell text so later chunks can carry the header path.
func validateCorrectionTable(fragment string) (string, error) {
	context := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := html.ParseFragment(strings.NewReader(strings.TrimSpace(fragment)), context)
	if err != nil || len(nodes) != 1 || nodes[0].DataAtom != atom.Table || !validCorrectionTableNode(nodes[0]) {
		return "", fmt.Errorf("expected one HTML table with only structural table tags and span attributes")
	}
	var output strings.Builder
	if err := html.Render(&output, nodes[0]); err != nil {
		return "", err
	}
	return splitHTMLTableRows(output.String()), nil
}

func validCorrectionTableNode(node *html.Node) bool {
	if node.Type == html.TextNode {
		return true
	}
	if node.Type != html.ElementNode || node.Namespace != "" {
		return false
	}
	switch node.DataAtom {
	case atom.Table, atom.Thead, atom.Tbody, atom.Tfoot, atom.Tr, atom.Th, atom.Td:
	default:
		return false
	}
	for _, attr := range node.Attr {
		if (node.DataAtom != atom.Th && node.DataAtom != atom.Td) || attr.Namespace != "" ||
			(attr.Key != "rowspan" && attr.Key != "colspan") {
			return false
		}
		span, err := strconv.ParseUint(attr.Val, 10, 8)
		if err != nil || span == 0 {
			return false
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.DataAtom == atom.Table || !validCorrectionTableNode(child) {
			return false
		}
	}
	return true
}

func applyMinerUTableEdits(result *types.ReadResult, edits []minerUTableEdit) error {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	original := []rune(result.MarkdownContent)
	var markdown strings.Builder
	var sources []types.SourceBlock
	appendOriginal := func(start, end, offset int) {
		markdown.WriteString(string(original[start:end]))
		for _, source := range result.SourceBlocks {
			left, right := max(start, source.Start), min(end, source.End)
			if left < right {
				source.Start, source.End = left+offset, right+offset
				sources = append(sources, source)
			}
		}
	}
	previous, offset := 0, 0
	for _, edit := range edits {
		if edit.start < previous {
			return fmt.Errorf("MinerU PDF table corrections overlap")
		}
		appendOriginal(previous, edit.start, offset)
		markdown.WriteString(edit.table)
		size := utf8.RuneCountInString(edit.table)
		start := edit.start + offset
		sources = append(sources, types.SourceBlock{Start: start, End: start + size, Locator: edit.locator})
		offset += size - (edit.end - edit.start)
		previous = edit.end
	}
	appendOriginal(previous, len(original), offset)
	result.MarkdownContent, result.SourceBlocks = markdown.String(), sources
	return nil
}
