// Package parser walks a goldmark AST and produces a ParseResult.
//
// Only ATX-style headings are recognised (setext headings are excluded from
// the goldmark parser so their text is treated as ordinary body content).
//
// The `#` heading (level 1) is decorative: stored as ParseResult.Title.
// Sections are derived from `##` and deeper headings.
package parser

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gparser "github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// md is a goldmark instance that recognises only ATX headings.
// The SetextHeadingParser is intentionally omitted so that setext-style
// headings (underlines with === or ---) are parsed as ordinary paragraphs.
var md = goldmark.New(
	goldmark.WithParserOptions(
		gparser.WithBlockParsers(
			util.Prioritized(gparser.NewThematicBreakParser(), 200),
			util.Prioritized(gparser.NewListParser(), 300),
			util.Prioritized(gparser.NewListItemParser(), 400),
			util.Prioritized(gparser.NewCodeBlockParser(), 500),
			util.Prioritized(gparser.NewATXHeadingParser(), 600),
			util.Prioritized(gparser.NewFencedCodeBlockParser(), 700),
			util.Prioritized(gparser.NewBlockquoteParser(), 800),
			util.Prioritized(gparser.NewHTMLBlockParser(), 900),
			util.Prioritized(gparser.NewParagraphParser(), 1000),
		),
	),
)

// Section is one indexed unit of content — a single ATX heading (level ≥ 2)
// and its immediate body text.
type Section struct {
	ID               string // first 16 hex chars of SHA256(filePath + ":" + path)
	Path             string // e.g. "auth/api-keys/rotation-policy"
	FilePath         string // absolute path to the .md file
	HeadingStartByte int64  // byte offset of the heading's first '#'
	StartByte        int64  // byte offset of first content byte (line after heading)
	EndByte          int64  // exclusive byte offset of end of full subtree
	HeadingTxt       string // raw heading text (un-slugified), e.g. "API Keys"
	Level            int    // heading depth (2–6; # is never a section)
	Content          string // immediate body text only (excludes child sections)
	Line             int    // 1-based line number of the heading in the file
}

// HeadingInfo is a lightweight record for every ATX heading found in the file,
// including the decorative `#` title.  Used by ValidateFile.
type HeadingInfo struct {
	Level int
	Line  int    // 1-based
	Text  string // raw heading text as it appears in source
	Slug  string // slugified last segment; empty for level-1 headings
	Path  string // full derived path (e.g. "auth/api-keys"); empty for level 1
}

// ParseResult is the output of Parse.
type ParseResult struct {
	Title       string        // # heading text; empty if absent
	Description string        // preamble body before first ## heading; empty if absent
	Sections    []Section     // all indexed sections, in document order
	Headings    []HeadingInfo // every ATX heading in document order; used by ValidateFile
}

// Parse walks filePath's source bytes and returns a ParseResult.
// Duplicate paths (same slug under same parent) are resolved last-wins.
func Parse(filePath string, source []byte) *ParseResult {
	name := strings.TrimSuffix(filepath.Base(filePath), ".md")
	fileSize := int64(len(source))

	// ── Phase 1: walk document children into an ordered item list ────────────

	type item struct {
		isHeading  bool
		pos        int64  // heading → HeadingStartByte; block → Lines()[0].Start
		level      int    // heading only
		lineNum    int    // heading only; 1-based
		headingTxt string // heading only
	}

	reader := text.NewReader(source)
	doc := md.Parser().Parse(reader)

	var items []item
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		if h, ok := n.(*ast.Heading); ok {
			pos := int64(h.Pos())
			lineNum := bytes.Count(source[:pos], []byte{'\n'}) + 1
			txt := atxHeadingText(h, source)
			items = append(items, item{
				isHeading:  true,
				pos:        pos,
				level:      h.Level,
				lineNum:    lineNum,
				headingTxt: txt,
			})
		} else {
			lines := n.Lines()
			if lines != nil && lines.Len() > 0 {
				items = append(items, item{pos: int64(lines.At(0).Start)})
			}
		}
	}

	// ── Phase 2: extract heading-only slice and compute StartByte per heading ─

	// hs[i] is the i-th heading (any level) in document order.
	// hsStart[i] is the StartByte for hs[i]: the pos of the next item in the
	// full items list (which may be a content block or another heading), or
	// fileSize if this is the last item.
	var hs []item
	var hsStart []int64

	for i, it := range items {
		if !it.isHeading {
			continue
		}
		hs = append(hs, it)
		nextPos := fileSize
		if i+1 < len(items) {
			nextPos = items[i+1].pos
		}
		hsStart = append(hsStart, nextPos)
	}

	// ── Phase 3: build ParseResult ────────────────────────────────────────────

	result := &ParseResult{}

	// Locate first section heading (level ≥ 2) for description computation.
	firstSectionPos := fileSize
	var titlePos int64 = -1
	for _, h := range hs {
		if h.level == 1 && titlePos < 0 {
			titlePos = h.pos
			result.Title = h.headingTxt
		}
		if h.level >= 2 && firstSectionPos == fileSize {
			firstSectionPos = h.pos
		}
		if titlePos >= 0 && firstSectionPos < fileSize {
			break
		}
	}

	// Description: bytes between end-of-title-line and first ## heading.
	descStart := int64(0)
	if titlePos >= 0 {
		// advance past the title heading's line
		nl := bytes.IndexByte(source[titlePos:], '\n')
		if nl >= 0 {
			descStart = titlePos + int64(nl) + 1
		} else {
			descStart = fileSize
		}
	}
	if descStart < firstSectionPos {
		result.Description = strings.TrimSpace(string(source[descStart:firstSectionPos]))
	}

	// Walk headings; maintain a stack for path derivation.
	type stackEntry struct {
		level int
		slug  string
	}
	var stack []stackEntry

	// pathSeen maps path → current index in result.Sections (for last-wins dedup).
	pathSeen := make(map[string]int)

	for i, h := range hs {
		hinfo := HeadingInfo{Level: h.level, Line: h.lineNum, Text: h.headingTxt}

		if h.level == 1 {
			result.Headings = append(result.Headings, hinfo)
			continue
		}

		// Pop stack entries whose level ≥ this heading's level.
		for len(stack) > 0 && stack[len(stack)-1].level >= h.level {
			stack = stack[:len(stack)-1]
		}

		slug := Slugify(h.headingTxt)

		// Build full path: filename / ancestor-slugs… / this-slug
		parts := make([]string, 0, len(stack)+2)
		parts = append(parts, name)
		for _, e := range stack {
			parts = append(parts, e.slug)
		}
		parts = append(parts, slug)
		path := strings.Join(parts, "/")

		stack = append(stack, stackEntry{level: h.level, slug: slug})

		hinfo.Slug = slug
		hinfo.Path = path
		result.Headings = append(result.Headings, hinfo)

		startByte := hsStart[i]

		// EndByte: first subsequent heading with level ≤ this heading's level.
		endByte := fileSize
		for j := i + 1; j < len(hs); j++ {
			if hs[j].level <= h.level {
				endByte = hs[j].pos
				break
			}
		}

		// bodyEndByte: start of the very next heading (any level), or fileSize.
		// Content must not include child section text.
		bodyEndByte := fileSize
		if i+1 < len(hs) {
			bodyEndByte = hs[i+1].pos
		}

		content := ""
		if startByte < bodyEndByte {
			content = strings.TrimSpace(string(source[startByte:bodyEndByte]))
		}

		// Last-wins for duplicate paths.
		if idx, exists := pathSeen[path]; exists {
			result.Sections = append(result.Sections[:idx], result.Sections[idx+1:]...)
			for k, v := range pathSeen {
				if v > idx {
					pathSeen[k] = v - 1
				}
			}
			delete(pathSeen, path)
		}

		sec := Section{
			ID:               sectionID(filePath, path),
			Path:             path,
			FilePath:         filePath,
			HeadingStartByte: h.pos,
			StartByte:        startByte,
			EndByte:          endByte,
			HeadingTxt:       h.headingTxt,
			Level:            h.level,
			Content:          content,
			Line:             h.lineNum,
		}
		pathSeen[path] = len(result.Sections)
		result.Sections = append(result.Sections, sec)
	}

	return result
}

// ── Validation ────────────────────────────────────────────────────────────────

// Issue represents a single structural violation found by ValidateFile.
type Issue struct {
	Line    int
	Message string
}

// ValidateFile checks a ParseResult against the four structural rules and
// returns one Issue per violation (empty slice means the file is clean).
//
// Rules:
//  1. At most one # heading.
//  2. The # heading must appear before any ## heading.
//  3. Heading levels must not skip (e.g. ## → #### without ### in between).
//  4. No duplicate paths (two siblings that slugify to the same segment).
func ValidateFile(r *ParseResult) []Issue {
	var issues []Issue

	titleCount := 0
	firstSectionLine := 0 // line of the first ## heading seen so far

	type stackEntry struct {
		level int
		line  int
	}
	var stack []stackEntry // level-2+ headings only

	// slugKey identifies a (parentPath, slug) pair for duplicate detection.
	type slugKey struct{ parentPath, slug string }
	slugSeen := make(map[slugKey]int) // → line of first occurrence

	for _, h := range r.Headings {
		switch {
		case h.Level == 1:
			titleCount++
			if titleCount > 1 {
				issues = append(issues, Issue{
					Line:    h.Line,
					Message: "multiple # headings — only one allowed",
				})
			}
			if firstSectionLine > 0 {
				issues = append(issues, Issue{
					Line:    h.Line,
					Message: "# heading must appear before any ## heading",
				})
			}

		default: // level >= 2
			if firstSectionLine == 0 {
				firstSectionLine = h.Line
			}

			// Pop stack entries with level >= this heading's level.
			for len(stack) > 0 && stack[len(stack)-1].level >= h.Level {
				stack = stack[:len(stack)-1]
			}

			// Rule 3: check for level skip.
			expectedMinLevel := 2
			if len(stack) > 0 {
				expectedMinLevel = stack[len(stack)-1].level + 1
			}
			if h.Level > expectedMinLevel {
				issues = append(issues, Issue{
					Line: h.Line,
					Message: fmt.Sprintf(
						"heading level skips from %s to %s",
						strings.Repeat("#", expectedMinLevel),
						strings.Repeat("#", h.Level),
					),
				})
			}

			stack = append(stack, stackEntry{level: h.Level, line: h.Line})

			// Rule 4: duplicate path within the same parent.
			lastSlash := strings.LastIndex(h.Path, "/")
			parentPath := ""
			if lastSlash >= 0 {
				parentPath = h.Path[:lastSlash]
			}
			key := slugKey{parentPath, h.Slug}
			if prevLine, exists := slugSeen[key]; exists {
				issues = append(issues, Issue{
					Line: h.Line,
					Message: fmt.Sprintf(
						"duplicate path: %s (also at line %d)",
						h.Path, prevLine,
					),
				})
			} else {
				slugSeen[key] = h.Line
			}
		}
	}

	return issues
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// Slugify converts heading text to a URL-safe path segment:
// lowercase, spaces → '-', all other non-alphanumeric characters stripped.
func Slugify(text string) string {
	text = strings.ToLower(text)
	var buf strings.Builder
	for _, r := range text {
		switch {
		case r == ' ':
			buf.WriteRune('-')
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-':
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// atxHeadingText returns the raw heading text from the first Lines() segment
// of an ATX heading node (the text after the '## ' prefix).
func atxHeadingText(h *ast.Heading, source []byte) string {
	lines := h.Lines()
	if lines == nil || lines.Len() == 0 {
		return ""
	}
	seg := lines.At(0)
	return strings.TrimSpace(string(seg.Value(source)))
}

// sectionID returns the first 16 hex characters of SHA256(filePath + ":" + path).
func sectionID(filePath, path string) string {
	sum := sha256.Sum256([]byte(filePath + ":" + path))
	return fmt.Sprintf("%x", sum[:])[:16]
}
