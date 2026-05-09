package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"charm.land/lipgloss/v2"
)

// headerEntry is one parsed markdown heading.
type headerEntry struct {
	level   int    // 1=h1, 2=h2, 3=h3 …
	text    string // heading text without leading #s
	srcLine int    // 0-based line index in source
}

// parseHeaders extracts all ATX headings from raw markdown content.
func parseHeaders(content string) []headerEntry {
	var out []headerEntry
	for i, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		level := 0
		for _, c := range line {
			if c == '#' {
				level++
			} else {
				break
			}
		}
		text := strings.TrimSpace(line[level:])
		if text != "" {
			out = append(out, headerEntry{level: level, text: text, srcLine: i})
		}
	}
	return out
}

// ansiRe strips ANSI escape sequences so we can search rendered output as plain text.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// findRenderedLine returns the first rendered-output line index that contains
// headingText as a substring (after stripping ANSI codes). Returns 0 on miss.
func findRenderedLine(rendered, headingText string) int {
	for i, line := range strings.Split(rendered, "\n") {
		if strings.Contains(stripANSI(line), headingText) {
			return i
		}
	}
	return 0
}

// renderHeaderPicker draws the header-picker list into the editor pane.
func (m model) renderHeaderPicker(width, height int) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")).PaddingBottom(1)
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var sb strings.Builder
	title := "  Jump to heading  (↵ select · esc cancel)"
	sb.WriteString(titleStyle.Render(title))
	sb.WriteByte('\n')
	linesUsed := 2 // title + blank line from PaddingBottom

	if len(m.headers) == 0 {
		sb.WriteString(dimStyle.Render("  (no headings found)"))
		linesUsed++
		for linesUsed < height {
			sb.WriteByte('\n')
			linesUsed++
		}
		return sb.String()
	}

	// Determine visible window around cursor.
	listH := height - linesUsed
	start := m.headerCursor - listH/2
	if start < 0 {
		start = 0
	}
	if start+listH > len(m.headers) {
		start = len(m.headers) - listH
		if start < 0 {
			start = 0
		}
	}
	end := start + listH
	if end > len(m.headers) {
		end = len(m.headers)
	}

	for i := start; i < end; i++ {
		h := m.headers[i]
		indent := strings.Repeat("  ", h.level-1)
		label := indent + h.text
		// truncate if too wide
		maxW := width - 4
		if len([]rune(label)) > maxW {
			label = string([]rune(label)[:maxW])
		}
		if i == m.headerCursor {
			sb.WriteString(cursorStyle.Render("▶ " + label))
		} else {
			prefix := "  "
			if h.level == 1 {
				sb.WriteString(normalStyle.Render(prefix + label))
			} else {
				sb.WriteString(dimStyle.Render(prefix + label))
			}
		}
		sb.WriteByte('\n')
		linesUsed++
	}

	// Pad remaining lines.
	for linesUsed < height {
		sb.WriteByte('\n')
		linesUsed++
	}
	return sb.String()
}

// jumpToHeader finds the heading's position in the rendered glamour output
// and sets previewScrollTop accordingly.
func (m model) jumpToHeader(h headerEntry, width int) model {
	if m.previewContent == "" {
		return m
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return m
	}
	rendered, err := r.Render(m.previewContent)
	if err != nil {
		return m
	}
	m.previewScrollTop = findRenderedLine(rendered, h.text)
	return m
}
