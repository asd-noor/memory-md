package tui

import (
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
)

// ---------------------------------------------------------------------------
// Item kinds
// ---------------------------------------------------------------------------

type itemKind int

const (
	kindFile    itemKind = iota // top-level .md file
	kindDir                     // subdirectory header
	kindSubFile                 // .md file inside an expanded dir
)

// treeItem is the single list.Item type used for all sidebar entries.
type treeItem struct {
	kind     itemKind
	name     string // file/dir name (no .md extension, no path prefix)
	dir      string // parent dir name — set only for kindSubFile
	expanded bool   // set only for kindDir
}

func (t treeItem) Title() string {
	switch t.kind {
	case kindFile:
		return t.name + ".md"
	case kindDir:
		if t.expanded {
			return "▼ " + t.name + "/"
		}
		return "▶ " + t.name + "/"
	case kindSubFile:
		return t.name + ".md"
	}
	return t.name
}
func (t treeItem) Description() string { return "" }
func (t treeItem) FilterValue() string {
	if t.kind == kindSubFile {
		return t.dir + "/" + t.name
	}
	return t.name
}

// filePath returns the relative path (no .md) suitable for loadFile/saveFile.
// Returns "" for kindDir items.
func (t treeItem) filePath() string {
	switch t.kind {
	case kindFile:
		return t.name
	case kindSubFile:
		return t.dir + "/" + t.name
	}
	return ""
}

// ---------------------------------------------------------------------------
// Delegate
// ---------------------------------------------------------------------------

type singleLineDelegate struct {
	fileSelected lipgloss.Style
	fileNormal   lipgloss.Style
	dirSelected  lipgloss.Style
	dirNormal    lipgloss.Style
	subSelected  lipgloss.Style
	subNormal    lipgloss.Style
}

func newSingleLineDelegate() singleLineDelegate {
	return singleLineDelegate{
		fileSelected: lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Bold(true).PaddingLeft(1),
		fileNormal:   lipgloss.NewStyle().Foreground(lipgloss.Color("252")).PaddingLeft(1),
		dirSelected:  lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true).PaddingLeft(1),
		dirNormal:    lipgloss.NewStyle().Foreground(lipgloss.Color("244")).PaddingLeft(1),
		subSelected:  lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Bold(true).PaddingLeft(3),
		subNormal:    lipgloss.NewStyle().Foreground(lipgloss.Color("252")).PaddingLeft(3),
	}
}

func (d singleLineDelegate) Height() int                              { return 1 }
func (d singleLineDelegate) Spacing() int                            { return 0 }
func (d singleLineDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d singleLineDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(treeItem)
	if !ok {
		return
	}
	selected := index == m.Index()
	title := item.Title()
	switch item.kind {
	case kindFile:
		if selected {
			fmt.Fprint(w, d.fileSelected.Render(title))
		} else {
			fmt.Fprint(w, d.fileNormal.Render(title))
		}
	case kindDir:
		if selected {
			fmt.Fprint(w, d.dirSelected.Render(title))
		} else {
			fmt.Fprint(w, d.dirNormal.Render(title))
		}
	case kindSubFile:
		if selected {
			fmt.Fprint(w, d.subSelected.Render(title))
		} else {
			fmt.Fprint(w, d.subNormal.Render(title))
		}
	}
}

// ---------------------------------------------------------------------------
// List constructor
// ---------------------------------------------------------------------------

func newFileList(items []list.Item, width, height int) list.Model {
	delegate := newSingleLineDelegate()
	l := list.New(items, delegate, width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	return l
}
