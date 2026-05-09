package tui

import (
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
)

type fileItem struct{ name string }

func (f fileItem) Title() string       { return f.name + ".md" }
func (f fileItem) Description() string { return "" }
func (f fileItem) FilterValue() string { return f.name }

// singleLineDelegate renders one line per item with no spacing.
type singleLineDelegate struct {
	selectedStyle lipgloss.Style
	normalStyle   lipgloss.Style
}

func newSingleLineDelegate() singleLineDelegate {
	return singleLineDelegate{
		selectedStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("141")).
			Bold(true).
			PaddingLeft(1),
		normalStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			PaddingLeft(1),
	}
}

func (d singleLineDelegate) Height() int                             { return 1 }
func (d singleLineDelegate) Spacing() int                           { return 0 }
func (d singleLineDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d singleLineDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	fi, ok := listItem.(fileItem)
	if !ok {
		return
	}
	title := fi.Title()
	if index == m.Index() {
		fmt.Fprint(w, d.selectedStyle.Render("> "+title))
	} else {
		fmt.Fprint(w, d.normalStyle.Render("  "+title))
	}
}

func newFileList(files []string, width, height int) list.Model {
	items := make([]list.Item, len(files))
	for i, f := range files {
		items[i] = fileItem{name: f}
	}
	delegate := newSingleLineDelegate()
	l := list.New(items, delegate, width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	return l
}
