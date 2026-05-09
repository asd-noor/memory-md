package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kujtimiihoxha/vimtea"
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
	files       []string     // .md file names without extension
	memDir      string       // $MEMORY_MD_DIR
	list        list.Model   // sidebar
	editor      vimtea.Editor
	focus       focusState
	current     string // currently loaded file name (without .md)
	savedContent string // content at the last successful save
	warnQuit    bool   // user pressed q/ctrl+c once with unsaved changes
	width       int
	height      int
	err         error
}

func newModel(memDir string) model {
	files := listFiles(memDir)
	l := newFileList(files, sidebarWidth, 20)
	ed := newEditor("")

	return model{
		files:  files,
		memDir: memDir,
		list:   l,
		editor: ed,
		focus:  focusSidebar,
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
		m.err = nil // clear stale error on new keypress

		switch msg.String() {
		case "ctrl+c":
			if m.isDirty() {
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
			} else {
				m.focus = focusSidebar
			}
			return m, nil
		}

		// q quits only from the sidebar
		if msg.String() == "q" && m.focus == focusSidebar {
			if m.isDirty() {
				if m.warnQuit {
					return m, tea.Quit
				}
				m.warnQuit = true
				return m, nil
			}
			return m, tea.Quit
		}

		if m.focus == focusSidebar {
			if msg.String() == "enter" {
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
		} else {
			// Editor focused — route key to vimtea
			newEd, cmd := m.editor.Update(msg)
			m.editor = newEd.(vimtea.Editor)
			cmds = append(cmds, cmd)
		}

	default:
		// Non-key messages: update both components so vimtea cursor blink
		// and list animations keep running.
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
		newEd, cmd := m.editor.Update(msg)
		m.editor = newEd.(vimtea.Editor)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}
	return m.render()
}

func (m model) render() string {
	statusH := 1
	innerH := m.height - borderSize - statusH
	if innerH < 1 {
		innerH = 1
	}

	// Sidebar panel
	sideContent := m.list.View()
	var sStyle lipgloss.Style
	if m.focus == focusSidebar {
		sStyle = focusBorderStyle
	} else {
		sStyle = normalBorderStyle
	}
	sidePanel := sStyle.Width(sidebarWidth).Height(innerH).Render(sideContent)

	// Editor panel
	editorContent := m.editor.View()
	editorW := m.width - sidebarWidth - borderSize*2
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
		if m.isDirty() {
			fileInfo += " [modified]"
		}
	}

	keys := "Tab:switch focus | :w save | q:quit"
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

	m.list.SetSize(sidebarWidth, innerH)
	m.editor = m.applyEditorSize(m.editor)
	return m
}

func (m model) applyEditorSize(ed vimtea.Editor) vimtea.Editor {
	statusH := 1
	innerH := m.height - borderSize - statusH
	if innerH < 1 {
		innerH = 1
	}

	editorW := m.width - sidebarWidth - borderSize*2
	if editorW < 10 {
		editorW = 10
	}

	// vimtea renders (h-1) lines when status bar is enabled (content + 1 status
	// line). The lipgloss border panel's interior is (innerH-2) rows. Pass
	// (innerH-1) so vimtea outputs exactly (innerH-2) lines — a perfect fit.
	newEd, _ := ed.SetSize(editorW, innerH-1)
	return newEd.(vimtea.Editor)
}
