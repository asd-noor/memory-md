package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kujtimiihoxha/vimtea"
)

// saveRequestMsg is dispatched by the :w command so the parent model can
// perform the actual file-system write.
type saveRequestMsg struct{}

// listFiles returns all .md file names (without .md extension) in dir.
func listFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			files = append(files, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	return files
}

// loadFile reads a .md file from dir and returns its content.
func loadFile(dir, name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, name+".md"))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// saveFile writes content to a .md file in dir.
func saveFile(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0644)
}

// newEditor creates a vimtea.Editor pre-loaded with content.
// Command mode is disabled — `:` is intercepted by the parent model which
// shows its own floating command input instead.
func newEditor(content string) vimtea.Editor {
	ed := vimtea.NewEditor(
		vimtea.WithContent(content),
		vimtea.WithEnableStatusBar(true),
		vimtea.WithEnableModeCommand(false),
		vimtea.WithFileName("file.md"),
	)
	return ed
}
