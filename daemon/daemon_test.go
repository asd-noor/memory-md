package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidationErrorsForPathReturnsNilForValidFile(t *testing.T) {
	memDir := t.TempDir()
	filePath := filepath.Join(memDir, "auth.md")
	content := "# Auth\n\n## API Keys\n\nKeys are hashed.\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("write auth.md: %v", err)
	}

	errors := validationErrorsForPath(memDir, "auth/api-keys")
	if len(errors) != 0 {
		t.Fatalf("expected no validation errors, got %v", errors)
	}
}

func TestValidationErrorsForPathReturnsFormattedIssues(t *testing.T) {
	memDir := t.TempDir()
	filePath := filepath.Join(memDir, "auth.md")
	content := "## API Keys\n\nFirst body.\n\n## API Keys\n\nSecond body.\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("write auth.md: %v", err)
	}

	errors := validationErrorsForPath(memDir, "auth/api-keys")
	if len(errors) != 1 {
		t.Fatalf("expected 1 validation error, got %d (%v)", len(errors), errors)
	}
	want := "auth:5: duplicate path: auth/api-keys (also at line 1)"
	if errors[0] != want {
		t.Fatalf("unexpected validation error\nwant: %q\n got: %q", want, errors[0])
	}
}
