package parser

import (
	"strings"
	"testing"
)

// ── Slugify ───────────────────────────────────────────────────────────────────

func TestSlugify(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"API Keys", "api-keys"},
		{"Token Refresh Policy", "token-refresh-policy"},
		{"Rotation Policy", "rotation-policy"},
		{"  leading spaces  ", "leading-spaces"},
		{"Hello, World!", "hello-world"},
		{"foo--bar", "foo--bar"},
		{"CamelCase", "camelcase"},
		{"123 numbers", "123-numbers"},
	}
	for _, tc := range cases {
		got := Slugify(tc.in)
		if got != tc.want {
			t.Errorf("Slugify(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

// ── Parse ─────────────────────────────────────────────────────────────────────

const exampleMD = `# Authentication

Covers all auth-related decisions. Tags: security, tokens.

## API Keys

Keys are hashed with bcrypt before storage.

### Rotation Policy

Keys rotate every 90 days.
`

func TestParse_BasicExample(t *testing.T) {
	r := Parse("/notes/auth.md", []byte(exampleMD))

	if r.Title != "Authentication" {
		t.Errorf("Title = %q; want %q", r.Title, "Authentication")
	}
	if !strings.Contains(r.Description, "Covers all auth-related decisions") {
		t.Errorf("Description = %q; want it to contain preamble", r.Description)
	}
	if len(r.Sections) != 2 {
		t.Fatalf("len(Sections) = %d; want 2", len(r.Sections))
	}

	apiKeys := r.Sections[0]
	if apiKeys.Path != "auth/api-keys" {
		t.Errorf("Sections[0].Path = %q; want %q", apiKeys.Path, "auth/api-keys")
	}
	if apiKeys.Level != 2 {
		t.Errorf("Sections[0].Level = %d; want 2", apiKeys.Level)
	}
	if apiKeys.HeadingTxt != "API Keys" {
		t.Errorf("Sections[0].HeadingTxt = %q; want %q", apiKeys.HeadingTxt, "API Keys")
	}
	if !strings.Contains(apiKeys.Content, "bcrypt") {
		t.Errorf("Sections[0].Content = %q; want it to contain 'bcrypt'", apiKeys.Content)
	}
	// Content must NOT include child section text.
	if strings.Contains(apiKeys.Content, "90 days") {
		t.Errorf("Sections[0].Content must not include child section text")
	}

	rot := r.Sections[1]
	if rot.Path != "auth/api-keys/rotation-policy" {
		t.Errorf("Sections[1].Path = %q; want %q", rot.Path, "auth/api-keys/rotation-policy")
	}
	if rot.Level != 3 {
		t.Errorf("Sections[1].Level = %d; want 3", rot.Level)
	}
	if !strings.Contains(rot.Content, "90 days") {
		t.Errorf("Sections[1].Content = %q; want it to contain '90 days'", rot.Content)
	}
}

func TestParse_ByteOffsets(t *testing.T) {
	src := []byte(exampleMD)
	r := Parse("/notes/auth.md", src)

	for _, sec := range r.Sections {
		// HeadingStartByte must point to '#'.
		if src[sec.HeadingStartByte] != '#' {
			t.Errorf("section %q: source[HeadingStartByte] = %q; want '#'",
				sec.Path, src[sec.HeadingStartByte])
		}
		// StartByte >= HeadingStartByte.
		if sec.StartByte < sec.HeadingStartByte {
			t.Errorf("section %q: StartByte %d < HeadingStartByte %d",
				sec.Path, sec.StartByte, sec.HeadingStartByte)
		}
		// EndByte > StartByte.
		if sec.EndByte <= sec.StartByte {
			t.Errorf("section %q: EndByte %d <= StartByte %d",
				sec.Path, sec.EndByte, sec.StartByte)
		}
		// EndByte <= fileSize.
		if sec.EndByte > int64(len(src)) {
			t.Errorf("section %q: EndByte %d > fileSize %d",
				sec.Path, sec.EndByte, len(src))
		}
	}

	// Parent EndByte must be >= child EndByte.
	apiKeys := r.Sections[0]
	rot := r.Sections[1]
	if apiKeys.EndByte < rot.EndByte {
		t.Errorf("parent EndByte %d < child EndByte %d", apiKeys.EndByte, rot.EndByte)
	}
}

func TestParse_LineNumbers(t *testing.T) {
	r := Parse("/notes/auth.md", []byte(exampleMD))
	// # Authentication is on line 1.
	if len(r.Headings) == 0 || r.Headings[0].Line != 1 {
		t.Errorf("Title heading line = %d; want 1", r.Headings[0].Line)
	}
	// Check that ## API Keys line > 1.
	if r.Sections[0].Line <= 1 {
		t.Errorf("Sections[0].Line = %d; want > 1", r.Sections[0].Line)
	}
	// ### Rotation Policy must be after ## API Keys.
	if r.Sections[1].Line <= r.Sections[0].Line {
		t.Errorf("Sections[1].Line %d not after Sections[0].Line %d",
			r.Sections[1].Line, r.Sections[0].Line)
	}
}

func TestParse_NoTitle(t *testing.T) {
	src := `## Only Section

Body text.
`
	r := Parse("/notes/misc.md", []byte(src))
	if r.Title != "" {
		t.Errorf("Title = %q; want empty", r.Title)
	}
	if r.Description != "" {
		t.Errorf("Description = %q; want empty", r.Description)
	}
	if len(r.Sections) != 1 {
		t.Fatalf("len(Sections) = %d; want 1", len(r.Sections))
	}
	if r.Sections[0].Path != "misc/only-section" {
		t.Errorf("Path = %q; want %q", r.Sections[0].Path, "misc/only-section")
	}
}

func TestParse_SectionID(t *testing.T) {
	r := Parse("/notes/auth.md", []byte(exampleMD))
	if len(r.Sections[0].ID) != 16 {
		t.Errorf("ID length = %d; want 16", len(r.Sections[0].ID))
	}
	// IDs for different paths must differ.
	if r.Sections[0].ID == r.Sections[1].ID {
		t.Errorf("IDs must be unique per path")
	}
}

func TestParse_DuplicateSlugsLastWins(t *testing.T) {
	src := `## API Keys

First content.

## API Keys

Second content.
`
	r := Parse("/notes/auth.md", []byte(src))
	if len(r.Sections) != 1 {
		t.Fatalf("len(Sections) = %d; want 1 (last wins)", len(r.Sections))
	}
	if !strings.Contains(r.Sections[0].Content, "Second content") {
		t.Errorf("Content = %q; last-wins should keep second occurrence", r.Sections[0].Content)
	}
}

func TestParse_FencedCodeBlockHeadingIgnored(t *testing.T) {
	src := "## Real Section\n\nBody.\n\n```\n## Not a heading\n```\n"
	r := Parse("/notes/x.md", []byte(src))
	if len(r.Sections) != 1 {
		t.Errorf("len(Sections) = %d; want 1 (## inside code fence ignored)", len(r.Sections))
	}
}

func TestParse_SetextIgnored(t *testing.T) {
	// Setext headings must be treated as plain paragraphs, not sections.
	src := "## ATX Section\n\nBody.\n\nSetext Heading\n==============\n\nMore body.\n"
	r := Parse("/notes/x.md", []byte(src))
	if len(r.Sections) != 1 {
		t.Errorf("len(Sections) = %d; want 1 (setext heading treated as body)", len(r.Sections))
	}
	if !strings.Contains(r.Sections[0].Content, "Setext Heading") {
		t.Errorf("setext text should appear in Content, got: %q", r.Sections[0].Content)
	}
}

// ── ValidateFile ──────────────────────────────────────────────────────────────

func TestValidate_Clean(t *testing.T) {
	r := Parse("/notes/auth.md", []byte(exampleMD))
	issues := ValidateFile(r)
	if len(issues) != 0 {
		t.Errorf("expected no issues; got %v", issues)
	}
}

func TestValidate_MultipleH1(t *testing.T) {
	src := "# First\n\n# Second\n\n## Section\n\nBody.\n"
	r := Parse("/notes/x.md", []byte(src))
	issues := ValidateFile(r)
	found := false
	for _, iss := range issues {
		if strings.Contains(iss.Message, "multiple #") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'multiple # headings' issue; got %v", issues)
	}
}

func TestValidate_H1AfterH2(t *testing.T) {
	src := "## Section\n\nBody.\n\n# Title After\n"
	r := Parse("/notes/x.md", []byte(src))
	issues := ValidateFile(r)
	found := false
	for _, iss := range issues {
		if strings.Contains(iss.Message, "# heading must appear before") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected '# heading must appear before' issue; got %v", issues)
	}
}

func TestValidate_LevelSkip(t *testing.T) {
	// ## directly to #### — skips ###
	src := "## Section\n\nBody.\n\n#### Deep\n\nContent.\n"
	r := Parse("/notes/x.md", []byte(src))
	issues := ValidateFile(r)
	found := false
	for _, iss := range issues {
		if strings.Contains(iss.Message, "skips") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected level-skip issue; got %v", issues)
	}
}

func TestValidate_NoDuplicatePaths(t *testing.T) {
	src := "## Section\n\nBody.\n\n## Other\n\nBody.\n"
	r := Parse("/notes/x.md", []byte(src))
	issues := ValidateFile(r)
	if len(issues) != 0 {
		t.Errorf("expected no issues; got %v", issues)
	}
}

func TestValidate_DuplicatePaths(t *testing.T) {
	src := "## API Keys\n\nFirst.\n\n## API Keys\n\nSecond.\n"
	r := Parse("/notes/auth.md", []byte(src))
	issues := ValidateFile(r)
	found := false
	for _, iss := range issues {
		if strings.Contains(iss.Message, "duplicate path") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected duplicate-path issue; got %v", issues)
	}
}
