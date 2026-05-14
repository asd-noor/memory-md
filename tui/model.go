package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
)

// ---------------------------------------------------------------------------
// Focus state
// ---------------------------------------------------------------------------

type focusState int

const (
	focusSidebar focusState = iota
	focusPreview
	focusEditor
	focusCommand
	focusHeaderPicker
)

// ---------------------------------------------------------------------------
// Styles
// ---------------------------------------------------------------------------

const sidebarWidth = 26

var (
	outerStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240"))

	sidebarBaseStyle = lipgloss.NewStyle().
		Width(sidebarWidth).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(lipgloss.Color("240"))

	sidebarFocusStyle = lipgloss.NewStyle().
		Width(sidebarWidth).
		Border(lipgloss.ThickBorder(), false, true, false, false).
		BorderForeground(lipgloss.Color("99"))

	statusBarBaseStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(lipgloss.Color("240"))

	statusBarFocusStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(lipgloss.Color("62"))

	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

type model struct {
	topFiles []string            // sorted top-level .md names (no ext)
	dirs     map[string][]string // dir → sorted sub-file names (no ext)
	dirOrder []string            // sorted dir names
	expanded map[string]bool     // which dirs are expanded
	memDir   string
	list         list.Model
	ed           editor
	cmdInput     textinput.Model
	focus        focusState
	current      string // loaded file name (no .md)
	savedContent string
	warnQuit        bool
	previewContent  string // file content shown in preview (= last saved)
	previewScrollTop int
	previewGPressed bool   // waiting for second 'g' in preview nav
	headers         []headerEntry
	headerCursor    int
	width           int
	height          int
	err             error
	statusMsg       string
}

func newModel(memDir string) model {
	topFiles, dirs, dirOrder := discoverEntries(memDir)

	ti := textinput.New()
	ti.Prompt = ":"
	ti.CharLimit = 256

	m := model{
		topFiles:  topFiles,
		dirs:      dirs,
		dirOrder:  dirOrder,
		expanded:  make(map[string]bool),
		memDir:    memDir,
		ed:        newEditorComponent(""),
		cmdInput:  ti,
		focus:     focusSidebar,
	}
	m.list = newFileList(m.buildItems(), sidebarWidth, 20)
	m.previewContent = ""
	return m
}

// discoverEntries reads the memory dir (depth 1 only) and returns:
// - topFiles: sorted .md names at the root (no ext)
// - dirs: map of dir name → sorted .md names inside (no ext)
// - dirOrder: sorted dir names
func discoverEntries(dir string) (topFiles []string, dirs map[string][]string, dirOrder []string) {
	dirs = make(map[string][]string)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			subs, _ := os.ReadDir(filepath.Join(dir, e.Name()))
			var subFiles []string
			for _, s := range subs {
				if !s.IsDir() && strings.HasSuffix(s.Name(), ".md") {
					subFiles = append(subFiles, strings.TrimSuffix(s.Name(), ".md"))
				}
			}
			if len(subFiles) > 0 {
				sort.Strings(subFiles)
				dirs[e.Name()] = subFiles
				dirOrder = append(dirOrder, e.Name())
			}
		} else if strings.HasSuffix(e.Name(), ".md") {
			topFiles = append(topFiles, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	sort.Strings(topFiles)
	sort.Strings(dirOrder)
	return
}

// buildItems constructs the flat list of treeItems reflecting current expand state.
func (m model) buildItems() []list.Item {
	var items []list.Item
	for _, f := range m.topFiles {
		items = append(items, treeItem{kind: kindFile, name: f})
	}
	for _, d := range m.dirOrder {
		exp := m.expanded[d]
		items = append(items, treeItem{kind: kindDir, name: d, expanded: exp})
		if exp {
			for _, f := range m.dirs[d] {
				items = append(items, treeItem{kind: kindSubFile, name: f, dir: d})
			}
		}
	}
	return items
}

func loadFile(memDir, name string) (string, error) {
	path := filepath.Join(memDir, name+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func saveFile(memDir, name, content string) error {
	path := filepath.Join(memDir, name+".md")
	return os.WriteFile(path, []byte(content), 0644)
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func (m model) Init() tea.Cmd {
	return nil
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.applyDimensions()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	// global ctrl+c
	if k == "ctrl+c" {
		if m.focus == focusCommand {
			m.focus = focusEditor
			m.cmdInput.Blur()
			return m, nil
		}
		if m.isDirty() {
			if m.warnQuit {
				return m, tea.Quit
			}
			m.warnQuit = true
			m.statusMsg = "unsaved changes — press ctrl+c again to quit, or :w to save"
			return m, nil
		}
		return m, tea.Quit
	}

	m.warnQuit = false
	m.err = nil
	m.statusMsg = ""

	switch m.focus {
	case focusCommand:
		return m.handleCommandFocus(msg)
	case focusSidebar:
		return m.handleSidebarFocus(msg)
	case focusPreview:
		return m.handlePreviewFocus(msg)
	case focusHeaderPicker:
		return m.handleHeaderPickerFocus(msg)
	case focusEditor:
		return m.handleEditorFocus(msg)
	}
	return m, nil
}

func (m model) handleCommandFocus(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "esc":
		m.focus = focusEditor
		m.cmdInput.Blur()
		m.cmdInput.SetValue("")
		return m, nil
	case "enter":
		cmd := strings.TrimSpace(m.cmdInput.Value())
		m.cmdInput.SetValue("")
		m.cmdInput.Blur()
		m.focus = focusEditor
		return m.execCmd(cmd)
	default:
		var tiCmd tea.Cmd
		m.cmdInput, tiCmd = m.cmdInput.Update(msg)
		return m, tiCmd
	}
}

func (m model) handleSidebarFocus(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "tab":
		if m.current != "" {
			m.focus = focusPreview
		}
		return m, nil
	case "q":
		if m.isDirty() {
			if m.warnQuit {
				return m, tea.Quit
			}
			m.warnQuit = true
			m.statusMsg = "unsaved changes — press q again to quit, or :w to save"
			return m, nil
		}
		return m, tea.Quit
	case "enter":
		sel := m.list.SelectedItem()
		if sel == nil {
			return m, nil
		}
		item := sel.(treeItem)
		switch item.kind {
		case kindDir:
			m.expanded[item.name] = !m.expanded[item.name]
			m.list.SetItems(m.buildItems())
		case kindFile, kindSubFile:
			content, err := loadFile(m.memDir, item.filePath())
			if err != nil {
				m.err = err
				return m, nil
			}
			m.current = item.filePath()
			m.previewContent = content
			m.savedContent = content
			m.ed.SetContent(content)
			m.previewScrollTop = 0
			m.focus = focusPreview
			m = m.applyDimensions()
		}
		return m, nil
	default:
		var listCmd tea.Cmd
		m.list, listCmd = m.list.Update(msg)
		return m, listCmd
	}
}

func (m model) handlePreviewFocus(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "tab":
		m.focus = focusSidebar
		m.previewGPressed = false
	case "q":
		return m, tea.Quit
	case "b":
		if m.current != "" {
			m.headers = parseHeaders(m.previewContent)
			m.headerCursor = 0
			m.focus = focusHeaderPicker
			m.previewGPressed = false
		}
	case "e":
		if m.current != "" {
			m.focus = focusEditor
			m.previewGPressed = false
		}
	case "j":
		m.previewScrollTop++
		m.previewGPressed = false
	case "k":
		if m.previewScrollTop > 0 {
			m.previewScrollTop--
		}
		m.previewGPressed = false
	case "ctrl+f", "ctrl+d":
		_, editorH := m.editorDims()
		m.previewScrollTop += editorH / 2
		m.previewGPressed = false
	case "ctrl+b", "ctrl+u":
		_, editorH := m.editorDims()
		if m.previewScrollTop -= editorH / 2; m.previewScrollTop < 0 {
			m.previewScrollTop = 0
		}
		m.previewGPressed = false
	case "g":
		if m.previewGPressed {
			m.previewScrollTop = 0
			m.previewGPressed = false
		} else {
			m.previewGPressed = true
		}
	case "G":
		m.previewScrollTop = 1<<31 - 1 // clamped in renderPreviewPane
		m.previewGPressed = false
	default:
		m.previewGPressed = false
	}
	return m, nil
}

func (m model) handleEditorFocus(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "tab":
		m.focus = focusSidebar
		return m, nil
	case "q":
		// return to preview from editor only in normal mode; in insert/visual pass it through
		if m.ed.mode == modeNormal {
			if m.isDirty() {
				m.warnQuit = true
				m.statusMsg = "unsaved changes — use :w to save or :q! to discard"
				return m, nil
			}
			return m.returnToPreview()
		}
		edCmd := m.ed.Update(msg)
		return m, edCmd
	case ":":
		m.focus = focusCommand
		m.cmdInput.SetValue("")
		m.cmdInput.Focus()
		return m, nil
	default:
		edCmd := m.ed.Update(msg)
		return m, edCmd
	}
}

func (m model) handleHeaderPickerFocus(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.focus = focusPreview
	case "j":
		if m.headerCursor < len(m.headers)-1 {
			m.headerCursor++
		}
	case "k":
		if m.headerCursor > 0 {
			m.headerCursor--
		}
	case "enter":
		if len(m.headers) > 0 {
			rightW, _ := m.editorDims()
			w := rightW - 1
			if w < 1 {
				w = 1
			}
			m = m.jumpToHeader(m.headers[m.headerCursor], w)
		}
		m.focus = focusPreview
	}
	return m, nil
}

func (m model) returnToPreview() (tea.Model, tea.Cmd) {
	m.previewContent = m.savedContent
	m.focus = focusPreview
	m.previewScrollTop = 0
	m.warnQuit = false
	return m, nil
}

func (m model) execCmd(cmd string) (tea.Model, tea.Cmd) {
	switch cmd {
	case "w":
		if m.current == "" {
			m.err = fmt.Errorf("no file open")
			return m, nil
		}
		if err := saveFile(m.memDir, m.current, m.ed.GetText()); err != nil {
			m.err = err
			return m, nil
		}
		m.savedContent = m.ed.GetText()
		m.previewContent = m.savedContent
		m.statusMsg = fmt.Sprintf("written %s.md", m.current)
	case "wq":
		if m.current == "" {
			m.err = fmt.Errorf("no file open")
			return m, nil
		}
		if err := saveFile(m.memDir, m.current, m.ed.GetText()); err != nil {
			m.err = err
			return m, nil
		}
		m.savedContent = m.ed.GetText()
		m.previewContent = m.savedContent
		return m.returnToPreview()
	case "q":
		if m.isDirty() {
			m.err = fmt.Errorf("unsaved changes (use :wq to save, :q! to discard)")
			return m, nil
		}
		return m.returnToPreview()
	case "q!":
		return m.returnToPreview()
	default:
		m.err = fmt.Errorf("unknown command: %s", cmd)
	}
	return m, nil
}

func (m model) isDirty() bool {
	return m.current != "" && m.ed.GetText() != m.savedContent
}

func (m model) editorDims() (rightW, editorH int) {
	innerW := m.width - 2    // outer border
	innerH := m.height - 2
	rightW = innerW - sidebarWidth - 1 // sidebar right border
	editorH = innerH - 2               // status bar (1 line + 1 border)
	if rightW < 1 {
		rightW = 1
	}
	if editorH < 1 {
		editorH = 1
	}
	return
}

func (m model) applyDimensions() model {
	innerH := m.height - 2
	if innerH < 1 {
		innerH = 1
	}
	m.list.SetSize(sidebarWidth, innerH)
	rightW, editorH := m.editorDims()
	m.ed.SetSize(rightW, editorH)
	return m
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m model) render() string {
	if m.width == 0 || m.height == 0 {
		return "initializing..."
	}

	innerH := m.height - 2
	if innerH < 1 {
		innerH = 1
	}
	rightW, editorH := m.editorDims()

	// sidebar
	sStyle := sidebarBaseStyle
	if m.focus == focusSidebar {
		sStyle = sidebarFocusStyle
	}
	sidebar := sStyle.Height(innerH).Render(m.list.View())

	// status bar
	var statusContent string
	if m.focus == focusCommand {
		statusContent = m.cmdInput.View()
	} else {
		statusContent = m.statusBar(rightW)
	}
	sbStyle := statusBarBaseStyle
	if m.focus == focusEditor || m.focus == focusCommand || m.focus == focusPreview {
		sbStyle = statusBarFocusStyle
	}
	status := sbStyle.Width(rightW).Render(statusContent)

	// main content pane
	var mainContent string
	switch m.focus {
	case focusPreview:
		mainContent = m.renderPreviewPane(rightW, editorH)
	case focusHeaderPicker:
		mainContent = m.renderHeaderPicker(rightW, editorH)
	default:
		mainContent = m.ed.View()
	}
	mainView := lipgloss.NewStyle().
		Width(rightW).
		Height(editorH).
		Render(mainContent)

	// right panel: status on top, content below
	right := lipgloss.JoinVertical(lipgloss.Left, status, mainView)

	// full inner area
	inner := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, right)

	return outerStyle.Render(inner)
}

func (m model) statusBar(width int) string {
	if m.err != nil {
		return errStyle.Render(m.err.Error())
	}
	if m.warnQuit {
		return warnStyle.Render(m.statusMsg)
	}
	if m.statusMsg != "" {
		return statusStyle.Render(m.statusMsg)
	}

	// default: filename left, hints right
	filename := m.current
	if filename == "" {
		filename = "[no file]"
	}
	if m.isDirty() {
		filename += " [+]"
	}

	var right string
	switch m.focus {
	case focusPreview:
		right = "b:headings  e:edit  Tab:sidebar  j/k:scroll  q:quit"
	case focusEditor:
		right = m.ed.ModeString() + "  Tab:sidebar  :w :wq :q :q!"
	default:
		right = "Tab:switch  q:quit"
	}
	left := filename

	gap := width - len([]rune(left)) - len([]rune(right))
	if gap < 1 {
		gap = 1
	}

	return statusStyle.Render(left + strings.Repeat(" ", gap) + right)
}

func (m model) renderPreviewPane(width, height int) string {
	// Reserve 1 char on the right for the scrollbar.
	contentWidth := width - 1
	if contentWidth < 1 {
		contentWidth = 1
	}

	if m.previewContent == "" {
		content := lipgloss.NewStyle().Width(contentWidth).Height(height).Render("")
		bar := previewScrollbar(height, 0, 0, 0)
		return lipgloss.JoinHorizontal(lipgloss.Top, content, bar)
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(contentWidth),
	)
	var rendered string
	if err == nil {
		if out, rerr := r.Render(m.previewContent); rerr == nil {
			rendered = out
		}
	}
	if rendered == "" {
		rendered = m.previewContent
	}
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	totalLines := len(lines)

	scrollTop := m.previewScrollTop
	maxScroll := totalLines - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scrollTop > maxScroll {
		scrollTop = maxScroll
	}
	if scrollTop < 0 {
		scrollTop = 0
	}

	end := scrollTop + height
	if end > totalLines {
		end = totalLines
	}
	visible := lines[scrollTop:end]

	var sb strings.Builder
	for i, l := range visible {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(l)
	}
	for i := len(visible); i < height; i++ {
		sb.WriteByte('\n')
	}

	content := lipgloss.NewStyle().Width(contentWidth).Height(height).Render(sb.String())
	bar := previewScrollbar(height, totalLines, scrollTop, height)
	return lipgloss.JoinHorizontal(lipgloss.Top, content, bar)
}

var (
	previewTrackStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	previewThumbStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// previewScrollbar builds a single-column scrollbar string of `height` lines.
// It shows a thumb (█) proportional to viewHeight/totalLines at the correct
// position, and a track (│) everywhere else. When all lines fit in view the
// track is dimmed to indicate no overflow.
func previewScrollbar(height, totalLines, scrollTop, viewHeight int) string {
	trackStyle := previewTrackStyle
	thumbStyle := previewThumbStyle

	bar := make([]string, height)

	if totalLines <= viewHeight || viewHeight == 0 {
		// Content fits — show a dim track, no thumb.
		for i := range bar {
			bar[i] = trackStyle.Render("│")
		}
		return strings.Join(bar, "\n")
	}

	// Thumb size proportional to how much of the content is visible.
	thumbSize := height * viewHeight / totalLines
	if thumbSize < 1 {
		thumbSize = 1
	}
	maxThumbStart := height - thumbSize
	thumbStart := scrollTop * maxThumbStart / (totalLines - viewHeight)

	for i := range bar {
		if i >= thumbStart && i < thumbStart+thumbSize {
			bar[i] = thumbStyle.Render("█")
		} else {
			bar[i] = trackStyle.Render("│")
		}
	}
	return strings.Join(bar, "\n")
}
