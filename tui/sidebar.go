package tui

import (
	"github.com/charmbracelet/bubbles/list"
)

// fileItem implements list.Item for a markdown file.
type fileItem struct {
	name string // file name without .md extension
}

func (f fileItem) Title() string       { return f.name }
func (f fileItem) Description() string { return f.name + ".md" }
func (f fileItem) FilterValue() string { return f.name }

// newFileList builds a list.Model from a slice of file names (without .md).
func newFileList(files []string, width, height int) list.Model {
	items := make([]list.Item, len(files))
	for i, f := range files {
		items[i] = fileItem{name: f}
	}
	l := list.New(items, list.NewDefaultDelegate(), width, height)
	l.Title = "Memory Files"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	return l
}
