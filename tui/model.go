package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kujtimiihoxha/vimtea"
)

const (
	sidebarWidth = 26
	borderSize   = 2 // top+bottom or left+right border chars
	cmdBoxHeight = 3 // border-top + input line + border-bottom
)

type focusState int

const (
	focusSidebar focusState = iota
	focusEditor
	focusCommand
)

var (
	normalBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("240"))

	focusBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("62"))

	cmdBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("141")).
			Padding(0, 1)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	errStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("9"))

	warnStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("11"))
)

type model struct {
	files        []string // .md file names without extension
	memDir       string   // $MEMORY_MD_DIR
	list         list.Model
	editor       vimtea.Editor
	cmdInput     textinput.Model
	focus        focusState
	current      string // currently loaded file name (without .md)
	savedContent string // content at the last successful save
	warnQuit     bool   // user pressed q/ctrl+c once with unsaved changes
	width        int
	height       int
	err          error
	statusMsg    string // transient message shown in status bar
}

func newModel(memDir string) model {
	files := listFiles(memDir)
	l := newFileList(files, sidebarWidth, 20)
	ed := newEditor("")

	ti := textinput.New()
	ti.Prompt = ":"
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	ti.Placeholder = "w / wq / q / q!"

	return model{
		files:    files,
		memDir:   memDir,
		list:     l,
		editor:   ed,
		cmdInput: ti,
		focus:    focusSidebar,
	}
}

func (m model) isDirty() bool {
	if m.current == "" {
		return false
	}
	return m.editor.GetBuffer().Text() != m.savedContent
}

func (m model) Init() tea.Cmd {
	return m.editor.Init()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.applyDimensions()
		return m, nil

	case saveRequestMsg:
		m.warnQuit = false
		if m.current != "" {
			content := m.editor.GetBuffer().Text()
			if err := saveFile(m.memDir, m.current, content); err != nil {
				m.err = err
			} else {
				m.savedContent = content
				m.err = nil
			}
		}
		return m, nil

	case tea.KeyMsg:
		m.err = nil
		m.statusMsg = ""

		// --- Global keys ---
		switch msg.String() {
		case "ctrl+c":
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
				return m, nil
			}
			return m, tea.Quit
		}

		// --- Per-focus routing ---
		switch m.focus {

		case focusCommand:
			switch msg.String() {
			case "esc":
				m.focus = focusEditor
				m.cmdInput.Blur()
				return m, nil
			case "enter":
				cmd := m.execCmd(m.cmdInput.Value())
				m.focus = focusEditor
				m.cmdInput.Blur()
				return m, cmd
			default:
				var tiCmd tea.Cmd
				m.cmdInput, tiCmd = m.cmdInput.Update(msg)
				return m, tiCmd
			}

		case focusSidebar:
			m.warnQuit = false
			switch msg.String() {
			case "tab":
				m.focus = focusEditor
				return m, nil
			case "q":
				if m.isDirty() {
					if m.warnQuit {
						return m, tea.Quit
					}
					m.warnQuit = true
					return m, nil
				}
				return m, tea.Quit
			case "enter":
				if sel := m.list.SelectedItem(); sel != nil {
					item := sel.(fileItem)
					content, err := loadFile(m.memDir, item.name)
					if err != nil {
						m.err = err
					} else {
						m.current = item.name
						m.savedContent = content
						m.editor = newEditor(content)
						m.editor = m.applyEditorSize(m.editor)
						m.focus = focusEditor
						cmds = append(cmds, m.editor.Init())
					}
				}
				return m, tea.Batch(cmds...)
			}
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			cmds = append(cmds, cmd)

		case focusEditor:
			m.warnQuit = false
			switch msg.String() {
			case "tab":
				m.focus = focusSidebar
				return m, nil
			case ":":
				// Intercept colon — open floating command input.
				m.focus = focusCommand
				m.cmdInput.SetValue("")
				cmds = append(cmds, m.cmdInput.Focus())
				return m, tea.Batch(cmds...)
			}
			// All other keys go to vimtea.
			newEd, cmd := m.editor.Update(msg)
			m.editor = newEd.(vimtea.Editor)
			cmds = append(cmds, cmd)
		}

	default:
		// Non-key messages (cursor blink, etc.) — pass to both components.
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
		newEd, cmd := m.editor.Update(msg)
		m.editor = newEd.(vimtea.Editor)
		cmds = append(cmds, cmd)
		m.cmdInput, cmd = m.cmdInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// execCmd parses and executes a command-line command.
func (m *model) execCmd(input string) tea.Cmd {
	cmd := strings.TrimSpace(input)
	switch cmd {
	case "w":
		return m.save()
	case "wq":
		saveCmd := m.save()
		return tea.Batch(saveCmd, tea.Quit)
	case "q":
		if m.isDirty() {
			m.statusMsg = "unsaved changes — use :q! to force quit or :wq to save and quit"
			return nil
		}
		return tea.Quit
	case "q!":
		return tea.Quit
	default:
		if cmd != "" {
			m.statusMsg = "unknown command: " + cmd
		}
		return nil
	}
}

func (m *model) save() tea.Cmd {
	if m.current == "" {
		m.statusMsg = "no file selected"
		return nil
	}
	content := m.editor.GetBuffer().Text()
	if err := saveFile(m.memDir, m.current, content); err != nil {
		m.err = err
		return nil
	}
	m.savedContent = content
	m.statusMsg = "saved " + m.current + ".md"
	return nil
}

// --- View ---

func (m model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}
	return m.render()
}

func (m model) render() string {
	// When the command box is open it sits below the panels; shrink panels
	// so the total fits within the terminal height.
	extraH := 0
	if m.focus == focusCommand {
		extraH = cmdBoxHeight
	}

	statusH := 1
	innerH := m.height - borderSize - statusH - extraH
	if innerH < 1 {
		innerH = 1
	}

	editorW := m.width - sidebarWidth - borderSize*2
	if editorW < 10 {
		editorW = 10
	}

	// Sidebar
	var sStyle lipgloss.Style
	if m.focus == focusSidebar {
		sStyle = focusBorderStyle
	} else {
		sStyle = normalBorderStyle
	}
	sidePanel := sStyle.Width(sidebarWidth).Height(innerH).Render(m.list.View())

	// Editor
	var eStyle lipgloss.Style
	if m.focus == focusEditor || m.focus == focusCommand {
		eStyle = focusBorderStyle
	} else {
		eStyle = normalBorderStyle
	}
	editorPanel := eStyle.Width(editorW).Height(innerH).Render(m.editor.View())

	layout := lipgloss.JoinHorizontal(lipgloss.Top, sidePanel, editorPanel)

	// Command box (shown below panels when active).
	if m.focus == focusCommand {
		inputView := m.cmdInput.View()
		// Inner width = editor panel width minus padding (2×1) and border (2×1).
		innerW := editorW - 4
		if innerW < 10 {
			innerW = 10
		}
		m.cmdInput.Width = innerW
		box := cmdBoxStyle.Width(innerW).Render(inputView)
		// Right-align the box under the editor panel.
		leftPad := strings.Repeat(" ", sidebarWidth+borderSize)
		return fmt.Sprintf("%s\n%s%s\n%s", layout, leftPad, box, m.statusBar())
	}

	return fmt.Sprintf("%s\n%s", layout, m.statusBar())
}

func (m model) statusBar() string {
	if m.err != nil {
		return errStyle.Render("error: " + m.err.Error())
	}
	if m.warnQuit {
		return warnStyle.Render("unsaved changes — press q or ctrl+c again to quit without saving")
	}
	if m.statusMsg != "" {
		return statusStyle.Render(m.statusMsg)
	}

	fileInfo := "(no file selected)"
	if m.current != "" {
		fileInfo = m.current + ".md"
		if m.isDirty() {
			fileInfo += " [modified]"
		}
	}

	keys := "Tab:switch  :w save  :q quit"
	left := statusStyle.Render(fileInfo)
	right := statusStyle.Render(keys)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// --- Dimensions ---

func (m model) applyDimensions() model {
	m.list.SetSize(sidebarWidth, m.innerH())
	m.editor = m.applyEditorSize(m.editor)
	return m
}

func (m model) innerH() int {
	statusH := 1
	h := m.height - borderSize - statusH
	if h < 1 {
		h = 1
	}
	return h
}

func (m model) applyEditorSize(ed vimtea.Editor) vimtea.Editor {
	innerH := m.innerH()
	editorW := m.width - sidebarWidth - borderSize*2
	if editorW < 10 {
		editorW = 10
	}
	// vimtea with status bar renders (h-1) total lines.
	// lipgloss border interior = innerH-2. Pass innerH-1 so output = innerH-2.
	newEd, _ := ed.SetSize(editorW, innerH-1)
	return newEd.(vimtea.Editor)
}
