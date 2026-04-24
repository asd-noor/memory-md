package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateFileWritesTitleAndDescription(t *testing.T) {
	memDir := t.TempDir()
	eng := &Engine{memDir: memDir}

	if err := eng.CreateFile("auth", "Authentication", "Covers auth-related decisions."); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(memDir, "auth.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "# Authentication\n\nCovers auth-related decisions.\n"
	if string(got) != want {
		t.Fatalf("unexpected file contents\nwant: %q\n got: %q", want, string(got))
	}
}

func TestCreateFileWritesTitleOnlyWhenDescriptionEmpty(t *testing.T) {
	memDir := t.TempDir()
	eng := &Engine{memDir: memDir}

	if err := eng.CreateFile("auth", "Authentication", ""); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(memDir, "auth.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "# Authentication\n"
	if string(got) != want {
		t.Fatalf("unexpected file contents\nwant: %q\n got: %q", want, string(got))
	}
}

func TestCreateFileRequiresNonEmptyTitle(t *testing.T) {
	memDir := t.TempDir()
	eng := &Engine{memDir: memDir}

	if err := eng.CreateFile("auth", "   ", "desc"); err == nil {
		t.Fatal("expected error for empty title")
	}
}
