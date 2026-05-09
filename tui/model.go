package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	sidebarWidth = 26
	borderSize   = 2 // top+bottom or left+right border chars
)

type focusState int

const (
	focusSidebar focusState = iota
	focusEditor
)

var (
	normalBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240"))

	focusBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("62"))

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	errStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("9"))

	warnStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("11"))
)

type model struct {
	files    []string      // .md file names without extension
	memDir   string        // $MEMORY_MD_DIR
	list     list.Model    // sidebar
	textarea textarea.Model // editor
	focus    focusState
	current  string // currently loaded file name (without .md)
	dirty    bool   // unsaved changes flag
	warnQuit bool   // user pressed q once with dirty file
	width    int
	height   int
	err      error
}

func newModel(memDir string) model {
	files := listFiles(memDir)

	l := newFileList(files, sidebarWidth, 20)
	ta := newEditor(80, 20)

	return model{
		files:   files,
		memDir:  memDir,
		list:    l,
		textarea: ta,
		focus:   focusSidebar,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.applyDimensions()
		return m, nil

	case tea.KeyPressMsg:
		m.err = nil // clear old error on new key

		switch msg.String() {
		case "ctrl+c":
			if m.dirty {
				if m.warnQuit {
					return m, tea.Quit
				}
				m.warnQuit = true
				return m, nil
			}
			return m, tea.Quit

		case "tab":
			m.warnQuit = false
			if m.focus == focusSidebar {
				m.focus = focusEditor
				cmd := m.textarea.Focus()
				cmds = append(cmds, cmd)
			} else {
				m.focus = focusSidebar
				m.textarea.Blur()
			}
			return m, tea.Batch(cmds...)

		case "ctrl+s":
			m.warnQuit = false
			if m.current != "" {
				if err := saveFile(m.memDir, m.current, m.textarea.Value()); err != nil {
					m.err = err
				} else {
					m.dirty = false
				}
			}
			return m, nil
		}

		// q only quits when sidebar is focused (otherwise it's editor input)
		if msg.String() == "q" && m.focus == focusSidebar {
			if m.dirty {
				if m.warnQuit {
					return m, tea.Quit
				}
				m.warnQuit = true
				return m, nil
			}
			return m, tea.Quit
		}

		if m.focus == focusSidebar {
			// Enter key loads the selected file
			if msg.String() == "enter" {
				if sel := m.list.SelectedItem(); sel != nil {
					item := sel.(fileItem)
					content, err := loadFile(m.memDir, item.name)
					if err != nil {
						m.err = err
					} else {
						m.current = item.name
						m.textarea.SetValue(content)
						m.dirty = false
						m.focus = focusEditor
						cmd := m.textarea.Focus()
						cmds = append(cmds, cmd)
					}
				}
				return m, tea.Batch(cmds...)
			}
			// Other keys go to list
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			cmds = append(cmds, cmd)
		} else {
			// Editor focused — track dirty state
			oldVal := m.textarea.Value()
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			if m.current != "" && m.textarea.Value() != oldVal {
				m.dirty = true
				m.warnQuit = false
			}
			cmds = append(cmds, cmd)
		}

	default:
		// Pass non-key messages to both components
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m model) render() string {
	if m.width == 0 {
		return "Initializing..."
	}

	statusH := 1
	innerH := m.height - borderSize - statusH
	if innerH < 1 {
		innerH = 1
	}

	// Sidebar
	sideContent := m.list.View()
	var sStyle lipgloss.Style
	if m.focus == focusSidebar {
		sStyle = focusBorderStyle
	} else {
		sStyle = normalBorderStyle
	}
	sidePanel := sStyle.Width(sidebarWidth).Height(innerH).Render(sideContent)

	// Editor
	editorContent := m.textarea.View()
	editorW := m.width - sidebarWidth - borderSize*2 // 2 panels × 2 border chars wide
	if editorW < 10 {
		editorW = 10
	}
	var eStyle lipgloss.Style
	if m.focus == focusEditor {
		eStyle = focusBorderStyle
	} else {
		eStyle = normalBorderStyle
	}
	editorPanel := eStyle.Width(editorW).Height(innerH).Render(editorContent)

	layout := lipgloss.JoinHorizontal(lipgloss.Top, sidePanel, editorPanel)

	return fmt.Sprintf("%s\n%s", layout, m.statusBar())
}

func (m model) statusBar() string {
	if m.err != nil {
		return errStyle.Render("Error: " + m.err.Error())
	}
	if m.warnQuit {
		return warnStyle.Render("Unsaved changes! Press q or Ctrl+C again to quit without saving.")
	}

	fileInfo := "(no file selected)"
	if m.current != "" {
		fileInfo = m.current + ".md"
		if m.dirty {
			fileInfo += " [modified]"
		}
	}

	keys := "Tab:switch | Ctrl+S:save | q:quit"
	left := statusStyle.Render(fileInfo)
	right := statusStyle.Render(keys)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m model) applyDimensions() model {
	statusH := 1
	innerH := m.height - borderSize - statusH
	if innerH < 1 {
		innerH = 1
	}

	editorW := m.width - sidebarWidth - borderSize*2
	if editorW < 10 {
		editorW = 10
	}

	m.list.SetSize(sidebarWidth, innerH)
	m.textarea.SetWidth(editorW)
	m.textarea.SetHeight(innerH - 1) // -1 for textarea internal chrome
	return m
}
