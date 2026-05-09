package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// fileItem implements list.Item for a markdown file.
type fileItem struct {
	name string // file name without .md extension
}

func (f fileItem) Title() string       { return f.name }
func (f fileItem) Description() string { return "" }
func (f fileItem) FilterValue() string { return f.name }

// singleLineDelegate renders each file as a single line.
type singleLineDelegate struct {
	selected lipgloss.Style
	normal   lipgloss.Style
}

func newSingleLineDelegate() singleLineDelegate {
	return singleLineDelegate{
		selected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("141")).
			Bold(true).
			PaddingLeft(1),
		normal: lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			PaddingLeft(1),
	}
}

func (d singleLineDelegate) Height() int                               { return 1 }
func (d singleLineDelegate) Spacing() int                              { return 0 }
func (d singleLineDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd  { return nil }
func (d singleLineDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	fi, ok := item.(fileItem)
	if !ok {
		return
	}
	name := fi.name + ".md"
	if index == m.Index() {
		fmt.Fprint(w, d.selected.Render(name))
	} else {
		fmt.Fprint(w, d.normal.Render(name))
	}
}

// newFileList builds a list.Model from a slice of file names (without .md).
func newFileList(files []string, width, height int) list.Model {
	items := make([]list.Item, len(files))
	for i, f := range files {
		items[i] = fileItem{name: f}
	}
	l := list.New(items, newSingleLineDelegate(), width, height)
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	return l
}
