package tui

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type editorMode int

const (
	modeNormal editorMode = iota
	modeInsert
	modeVisual
	modeSearch
)

type editor struct {
	lines      []string
	row        int
	col        int
	wantCol    int
	scrollTop  int
	width      int
	height     int
	mode       editorMode
	yankBuf    []string
	vAnchorRow int
	vAnchorCol int
	searchPat  string
	searchDir  int // +1 forward, -1 backward
	searchInput string
	gPressed   bool // waiting for second 'g'
	dPressed   bool // waiting for second 'd'
	yPressed   bool // waiting for second 'y'
}

func newEditorComponent(content string) editor {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	return editor{
		lines:     lines,
		searchDir: 1,
	}
}

func (e *editor) SetSize(w, h int) {
	e.width = w
	e.height = h
}

func (e *editor) GetText() string {
	return strings.Join(e.lines, "\n")
}

func (e *editor) SetContent(s string) {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	e.lines = lines
	e.row = 0
	e.col = 0
	e.wantCol = 0
	e.scrollTop = 0
	e.mode = modeNormal
	e.gPressed = false
	e.dPressed = false
	e.yPressed = false
}

func (e *editor) ModeString() string {
	switch e.mode {
	case modeInsert:
		return "INSERT"
	case modeVisual:
		return "VISUAL"
	case modeSearch:
		return "SEARCH"
	default:
		return "NORMAL"
	}
}

func (e *editor) clampCol() {
	line := e.lines[e.row]
	maxCol := len([]rune(line)) - 1
	if maxCol < 0 {
		maxCol = 0
	}
	if e.col > maxCol {
		e.col = maxCol
	}
	if e.col < 0 {
		e.col = 0
	}
}

func (e *editor) clampRow() {
	if e.row < 0 {
		e.row = 0
	}
	if e.row >= len(e.lines) {
		e.row = len(e.lines) - 1
	}
}

func (e *editor) scrollToCursor() {
	if e.height <= 0 {
		return
	}
	if e.row < e.scrollTop {
		e.scrollTop = e.row
	}
	if e.row >= e.scrollTop+e.height {
		e.scrollTop = e.row - e.height + 1
	}
	if e.scrollTop < 0 {
		e.scrollTop = 0
	}
}

func (e *editor) currentLine() string {
	if e.row < len(e.lines) {
		return e.lines[e.row]
	}
	return ""
}

// wordForward advances col to start of next word.
func (e *editor) wordForward() {
	runes := []rune(e.currentLine())
	col := e.col
	// skip current word chars
	for col < len(runes) && !unicode.IsSpace(runes[col]) {
		col++
	}
	// skip spaces
	for col < len(runes) && unicode.IsSpace(runes[col]) {
		col++
	}
	e.col = col
	e.clampCol()
}

// wordBackward moves col to start of previous word.
func (e *editor) wordBackward() {
	runes := []rune(e.currentLine())
	col := e.col
	if col > 0 {
		col--
	}
	// skip spaces
	for col > 0 && unicode.IsSpace(runes[col]) {
		col--
	}
	// skip word chars
	for col > 0 && !unicode.IsSpace(runes[col-1]) {
		col--
	}
	e.col = col
}

// wordEnd moves to end of current/next word.
func (e *editor) wordEnd() {
	runes := []rune(e.currentLine())
	col := e.col
	if col < len(runes)-1 {
		col++
	}
	// skip spaces
	for col < len(runes)-1 && unicode.IsSpace(runes[col]) {
		col++
	}
	// advance to end of word
	for col < len(runes)-1 && !unicode.IsSpace(runes[col+1]) {
		col++
	}
	e.col = col
}

func (e *editor) insertRuneAt(r rune) {
	line := []rune(e.currentLine())
	col := e.col
	if col > len(line) {
		col = len(line)
	}
	newLine := make([]rune, 0, len(line)+1)
	newLine = append(newLine, line[:col]...)
	newLine = append(newLine, r)
	newLine = append(newLine, line[col:]...)
	e.lines[e.row] = string(newLine)
	e.col++
}

func (e *editor) deleteCharBefore() {
	if e.col > 0 {
		line := []rune(e.currentLine())
		newLine := make([]rune, 0, len(line)-1)
		newLine = append(newLine, line[:e.col-1]...)
		newLine = append(newLine, line[e.col:]...)
		e.lines[e.row] = string(newLine)
		e.col--
	} else if e.row > 0 {
		// merge with previous line
		prevLine := e.lines[e.row-1]
		curLine := e.lines[e.row]
		e.col = len([]rune(prevLine))
		e.lines[e.row-1] = prevLine + curLine
		e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
		e.row--
	}
}

func (e *editor) splitLine() {
	line := []rune(e.currentLine())
	col := e.col
	if col > len(line) {
		col = len(line)
	}
	left := string(line[:col])
	right := string(line[col:])
	e.lines[e.row] = left
	// insert new line after current
	newLines := make([]string, len(e.lines)+1)
	copy(newLines, e.lines[:e.row+1])
	newLines[e.row+1] = right
	copy(newLines[e.row+2:], e.lines[e.row+1:])
	e.lines = newLines
	e.row++
	e.col = 0
}

func (e *editor) deleteLine(row int) {
	yanked := e.lines[row]
	e.yankBuf = []string{yanked}
	if len(e.lines) == 1 {
		e.lines[0] = ""
	} else {
		e.lines = append(e.lines[:row], e.lines[row+1:]...)
	}
	if e.row >= len(e.lines) {
		e.row = len(e.lines) - 1
	}
}

func (e *editor) visualMinMax() (int, int) {
	a, b := e.vAnchorRow, e.row
	if a > b {
		a, b = b, a
	}
	return a, b
}

// searchNext finds the next occurrence of e.searchPat starting from the current position.
func (e *editor) searchNext() {
	if e.searchPat == "" {
		return
	}
	total := len(e.lines)
	startRow := e.row
	startCol := e.col + 1
	for i := 0; i < total; i++ {
		r := (startRow + i*e.searchDir + total) % total
		line := e.lines[r]
		var idx int
		if e.searchDir > 0 {
			sc := 0
			if i == 0 {
				sc = startCol
			}
			if sc > len(line) {
				continue
			}
			idx = strings.Index(line[sc:], e.searchPat)
			if idx >= 0 {
				e.row = r
				e.col = sc + idx
				e.scrollToCursor()
				return
			}
		} else {
			end := len(line)
			if i == 0 && e.col > 0 {
				end = e.col
			}
			sub := line[:end]
			idx = strings.LastIndex(sub, e.searchPat)
			if idx >= 0 {
				e.row = r
				e.col = idx
				e.scrollToCursor()
				return
			}
		}
	}
}

func (e *editor) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if e.mode == modeSearch {
			return e.handleSearchMode(msg)
		}
		if e.mode == modeInsert {
			return e.handleInsertMode(msg)
		}
		if e.mode == modeVisual {
			return e.handleVisualMode(msg)
		}
		return e.handleNormalMode(msg)
	}
	return nil
}

func (e *editor) handleSearchMode(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		e.mode = modeNormal
		e.searchInput = ""
	case "enter":
		e.searchPat = e.searchInput
		e.searchInput = ""
		e.mode = modeNormal
		e.searchNext()
	case "backspace", "ctrl+h":
		if len(e.searchInput) > 0 {
			r := []rune(e.searchInput)
			e.searchInput = string(r[:len(r)-1])
		}
	default:
		if msg.Text != "" {
			e.searchInput += msg.Text
		}
	}
	return nil
}

func (e *editor) handleInsertMode(msg tea.KeyPressMsg) tea.Cmd {
	e.gPressed = false
	e.dPressed = false
	e.yPressed = false
	switch msg.String() {
	case "esc":
		e.mode = modeNormal
		e.clampCol()
	case "enter":
		e.splitLine()
		e.scrollToCursor()
	case "backspace", "ctrl+h":
		e.deleteCharBefore()
		e.scrollToCursor()
	default:
		if msg.Text != "" {
			for _, r := range msg.Text {
				e.insertRuneAt(r)
			}
			e.scrollToCursor()
		}
	}
	return nil
}

func (e *editor) handleVisualMode(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		e.mode = modeNormal
	case "h":
		if e.col > 0 {
			e.col--
		}
	case "l":
		line := []rune(e.currentLine())
		if e.col < len(line)-1 {
			e.col++
		}
	case "j":
		if e.row < len(e.lines)-1 {
			e.row++
			e.scrollToCursor()
		}
	case "k":
		if e.row > 0 {
			e.row--
			e.scrollToCursor()
		}
	case "d":
		minR, maxR := e.visualMinMax()
		// yank first
		yanked := make([]string, maxR-minR+1)
		copy(yanked, e.lines[minR:maxR+1])
		e.yankBuf = yanked
		// delete lines
		if maxR-minR+1 >= len(e.lines) {
			e.lines = []string{""}
		} else {
			e.lines = append(e.lines[:minR], e.lines[maxR+1:]...)
		}
		e.row = minR
		e.clampRow()
		e.clampCol()
		e.scrollToCursor()
		e.mode = modeNormal
	case "y":
		minR, maxR := e.visualMinMax()
		yanked := make([]string, maxR-minR+1)
		copy(yanked, e.lines[minR:maxR+1])
		e.yankBuf = yanked
		e.row = e.vAnchorRow
		e.col = e.vAnchorCol
		e.clampCol()
		e.mode = modeNormal
	}
	return nil
}

func (e *editor) handleNormalMode(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()

	// handle pending two-key combos
	if e.gPressed {
		e.gPressed = false
		if k == "g" {
			e.row = 0
			e.clampCol()
			e.scrollToCursor()
			return nil
		}
	}
	if e.dPressed {
		e.dPressed = false
		if k == "d" {
			e.deleteLine(e.row)
			e.clampCol()
			e.scrollToCursor()
			return nil
		}
	}
	if e.yPressed {
		e.yPressed = false
		if k == "y" {
			e.yankBuf = []string{e.currentLine()}
			return nil
		}
	}

	switch k {
	case "h":
		if e.col > 0 {
			e.col--
			e.wantCol = e.col
		}
	case "l":
		line := []rune(e.currentLine())
		if e.col < len(line)-1 {
			e.col++
			e.wantCol = e.col
		}
	case "j":
		if e.row < len(e.lines)-1 {
			e.row++
			e.col = e.wantCol
			e.clampCol()
			e.scrollToCursor()
		}
	case "k":
		if e.row > 0 {
			e.row--
			e.col = e.wantCol
			e.clampCol()
			e.scrollToCursor()
		}
	case "w":
		e.wordForward()
		e.wantCol = e.col
	case "b":
		e.wordBackward()
		e.wantCol = e.col
	case "e":
		e.wordEnd()
		e.wantCol = e.col
	case "0":
		e.col = 0
		e.wantCol = 0
	case "$":
		line := []rune(e.currentLine())
		if len(line) > 0 {
			e.col = len(line) - 1
		} else {
			e.col = 0
		}
		e.wantCol = e.col
	case "ctrl+f", "ctrl+d":
		half := e.height / 2
		if half < 1 {
			half = 1
		}
		e.row += half
		e.clampRow()
		e.col = e.wantCol
		e.clampCol()
		e.scrollToCursor()
	case "ctrl+b", "ctrl+u":
		half := e.height / 2
		if half < 1 {
			half = 1
		}
		e.row -= half
		e.clampRow()
		e.col = e.wantCol
		e.clampCol()
		e.scrollToCursor()
	case "g":
		e.gPressed = true
	case "G":
		e.row = len(e.lines) - 1
		e.clampCol()
		e.scrollToCursor()
	case "d":
		e.dPressed = true
	case "y":
		e.yPressed = true
	case "p":
		if len(e.yankBuf) > 0 {
			// paste below current row
			insertAt := e.row + 1
			newLines := make([]string, 0, len(e.lines)+len(e.yankBuf))
			newLines = append(newLines, e.lines[:insertAt]...)
			newLines = append(newLines, e.yankBuf...)
			newLines = append(newLines, e.lines[insertAt:]...)
			e.lines = newLines
			e.row = insertAt
			e.col = 0
			e.scrollToCursor()
		}
	case "P":
		if len(e.yankBuf) > 0 {
			// paste above current row
			newLines := make([]string, 0, len(e.lines)+len(e.yankBuf))
			newLines = append(newLines, e.lines[:e.row]...)
			newLines = append(newLines, e.yankBuf...)
			newLines = append(newLines, e.lines[e.row:]...)
			e.lines = newLines
			e.col = 0
			e.scrollToCursor()
		}
	case "i":
		e.mode = modeInsert
	case "I":
		e.col = 0
		e.mode = modeInsert
	case "a":
		line := []rune(e.currentLine())
		if e.col < len(line) {
			e.col++
		}
		e.mode = modeInsert
	case "A":
		e.col = len([]rune(e.currentLine()))
		e.mode = modeInsert
	case "o":
		// insert blank line below
		newLines := make([]string, len(e.lines)+1)
		copy(newLines, e.lines[:e.row+1])
		newLines[e.row+1] = ""
		copy(newLines[e.row+2:], e.lines[e.row+1:])
		e.lines = newLines
		e.row++
		e.col = 0
		e.mode = modeInsert
		e.scrollToCursor()
	case "O":
		// insert blank line above
		newLines := make([]string, len(e.lines)+1)
		copy(newLines, e.lines[:e.row])
		newLines[e.row] = ""
		copy(newLines[e.row+1:], e.lines[e.row:])
		e.lines = newLines
		e.col = 0
		e.mode = modeInsert
		e.scrollToCursor()
	case "v":
		e.mode = modeVisual
		e.vAnchorRow = e.row
		e.vAnchorCol = e.col
	case "/":
		e.mode = modeSearch
		e.searchInput = ""
		e.searchDir = 1
	case "?":
		e.mode = modeSearch
		e.searchInput = ""
		e.searchDir = -1
	case "n":
		e.searchNext()
	case "N":
		e.searchDir = -e.searchDir
		e.searchNext()
		e.searchDir = -e.searchDir
	}
	return nil
}

// lineNumWidth returns the width needed for line numbers.
func (e *editor) lineNumWidth() int {
	digits := len(fmt.Sprintf("%d", len(e.lines)))
	if digits < 1 {
		digits = 1
	}
	return digits
}

// View renders the editor content.
func (e *editor) View() string {
	if e.height <= 0 || e.width <= 0 {
		return ""
	}

	numW := e.lineNumWidth()
	// format: "  1 │ content"
	prefixW := numW + 3 // " │ "

	cursorStyle := lipgloss.NewStyle().Reverse(true)
	visualStyle := lipgloss.NewStyle().Background(lipgloss.Color("238"))
	lineNumStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	visMinR, visMaxR := -1, -1
	if e.mode == modeVisual {
		visMinR, visMaxR = e.visualMinMax()
	}

	var sb strings.Builder
	renderHeight := e.height
	if e.mode == modeSearch {
		renderHeight = e.height - 1
	}

	for i := 0; i < renderHeight; i++ {
		lineIdx := e.scrollTop + i
		if i > 0 {
			sb.WriteByte('\n')
		}

		if lineIdx >= len(e.lines) {
			// empty line padding
			sb.WriteString(lineNumStyle.Render(strings.Repeat(" ", numW)))
			sb.WriteString(sepStyle.Render(" │ "))
			sb.WriteString(strings.Repeat(" ", e.width-prefixW))
			continue
		}

		// line number
		numStr := fmt.Sprintf("%*d", numW, lineIdx+1)
		sb.WriteString(lineNumStyle.Render(numStr))
		sb.WriteString(sepStyle.Render(" │ "))

		line := []rune(e.lines[lineIdx])
		contentW := e.width - prefixW
		if contentW < 0 {
			contentW = 0
		}

		inVisual := e.mode == modeVisual && lineIdx >= visMinR && lineIdx <= visMaxR

		if lineIdx == e.row {
			// render cursor line
			col := e.col
			if col > len(line) {
				col = len(line)
			}

			// chars before cursor
			before := string(line[:col])
			// cursor char
			var cursorChar string
			if col < len(line) {
				cursorChar = string(line[col])
			} else {
				cursorChar = " "
			}
			// chars after cursor
			var after string
			if col < len(line) {
				after = string(line[col+1:])
			}

			if inVisual {
				sb.WriteString(visualStyle.Render(before))
				sb.WriteString(cursorStyle.Render(cursorChar))
				sb.WriteString(visualStyle.Render(after))
			} else {
				sb.WriteString(before)
				sb.WriteString(cursorStyle.Render(cursorChar))
				sb.WriteString(after)
			}
			// pad to content width
			rendered := len([]rune(before)) + 1 + len([]rune(after))
			if rendered < contentW {
				sb.WriteString(strings.Repeat(" ", contentW-rendered))
			}
		} else if inVisual {
			content := string(line)
			sb.WriteString(visualStyle.Render(content))
			if len(line) < contentW {
				sb.WriteString(strings.Repeat(" ", contentW-len(line)))
			}
		} else {
			content := string(line)
			if len(content) > contentW {
				content = string([]rune(content)[:contentW])
			}
			sb.WriteString(content)
			if len(line) < contentW {
				sb.WriteString(strings.Repeat(" ", contentW-len(line)))
			}
		}
	}

	// search mode: add search input as last line
	if e.mode == modeSearch {
		sb.WriteByte('\n')
		dir := "/"
		if e.searchDir < 0 {
			dir = "?"
		}
		searchLine := dir + e.searchInput
		sb.WriteString(searchLine)
		if len([]rune(searchLine)) < e.width {
			sb.WriteString(strings.Repeat(" ", e.width-len([]rune(searchLine))))
		}
	}

	return sb.String()
}
