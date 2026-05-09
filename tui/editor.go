package tui

import (
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textarea"
)

// listFiles returns all .md file names (without .md) in dir.
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

// newEditor creates a configured textarea.Model for use as the editor.
func newEditor(width, height int) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Select a file from the sidebar (Tab to switch focus)"
	ta.ShowLineNumbers = true
	ta.SetWidth(width)
	ta.SetHeight(height)
	return ta
}
