package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotFilesCopiesMarkdownFilesByDefault(t *testing.T) {
	memDir := t.TempDir()
	mustWriteFile(t, filepath.Join(memDir, "auth.md"), "# Authentication\n")
	mustWriteFile(t, filepath.Join(memDir, "notes.txt"), "ignore\n")
	mustMkdir(t, filepath.Join(memDir, "nested"))
	mustWriteFile(t, filepath.Join(memDir, "nested", "infra.md"), "nested\n")

	snapDir, err := snapshotFiles(memDir, false)
	if err != nil {
		t.Fatalf("snapshotFiles(copy): %v", err)
	}

	assertFileExists(t, filepath.Join(memDir, "auth.md"))
	assertFileExists(t, filepath.Join(snapDir, "auth.md"))
	assertFileMissing(t, filepath.Join(snapDir, "notes.txt"))
	assertFileMissing(t, filepath.Join(snapDir, "infra.md"))
}

func TestSnapshotFilesMovesMarkdownFilesWithMoveFlag(t *testing.T) {
	memDir := t.TempDir()
	mustWriteFile(t, filepath.Join(memDir, "auth.md"), "# Authentication\n")
	mustWriteFile(t, filepath.Join(memDir, "project.md"), "# Project\n")
	mustWriteFile(t, filepath.Join(memDir, "notes.txt"), "ignore\n")

	snapDir, err := snapshotFiles(memDir, true)
	if err != nil {
		t.Fatalf("snapshotFiles(move): %v", err)
	}

	assertFileMissing(t, filepath.Join(memDir, "auth.md"))
	assertFileMissing(t, filepath.Join(memDir, "project.md"))
	assertFileExists(t, filepath.Join(memDir, "notes.txt"))
	assertFileExists(t, filepath.Join(snapDir, "auth.md"))
	assertFileExists(t, filepath.Join(snapDir, "project.md"))
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s to exist: %v", path, err)
	}
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		if err == nil {
			t.Fatalf("expected file %s to be missing", path)
		}
		t.Fatalf("stat %s: %v", path, err)
	}
}
