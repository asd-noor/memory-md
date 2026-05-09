package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// Run starts the memory-md TUI for the given memory directory.
func Run(memDir string) error {
	if info, err := os.Stat(memDir); err != nil || !info.IsDir() {
		return fmt.Errorf("MEMORY_MD_DIR %q is not a valid directory", memDir)
	}
	m := newModel(memDir)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
