package tui

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

// Run starts the TUI for the given memory directory.
func Run(memDir string) error {
	info, err := os.Stat(memDir)
	if err != nil {
		return fmt.Errorf("memory dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("memory dir is not a directory: %s", memDir)
	}

	m := newModel(memDir)
	p := tea.NewProgram(m)
	_, err = p.Run()
	return err
}
