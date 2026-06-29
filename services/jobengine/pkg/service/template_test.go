package service

import (
	"os"
	"testing"
)

func TestResolveBasicVars(t *testing.T) {
	vars := &TemplateVars{
		Source:     "/tmp/input.docx",
		SourceName: "input.docx",
		SourceExt:  ".docx",
		Target:     "/tmp/output.pdf",
		TargetDir:  "/tmp/outdir",
		User:       UserInfo{ID: "u1", DisplayName: "Alice", Email: "alice@test.com"},
		Space:      SpaceInfo{ID: "s1", Name: "Innere Verwaltung"},
		Resource:   ResourceInfo{ID: "r1", Name: "brief.docx", Path: "/docs/brief.docx"},
		Options:    map[string]string{"engine": "xelatex", "quality": "high"},
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"{{source}}", "/tmp/input.docx"},
		{"{{target}}", "/tmp/output.pdf"},
		{"{{target_dir}}", "/tmp/outdir"},
		{"{{source.name}}", "input.docx"},
		{"{{source.nameWithoutExt}}", "input"},
		{"{{source.ext}}", ".docx"},
		{"{{user.id}}", "u1"},
		{"{{user.displayName}}", "Alice"},
		{"{{space.name}}", "Innere Verwaltung"},
		{"{{resource.path}}", "/docs/brief.docx"},
		{"{{options.engine}}", "xelatex"},
		{"{{options.quality}}", "high"},
		{"{{options.missing}}", "{{options.missing}}"},
		{"--var={{user.displayName}}", "--var=Alice"},
		{"-o {{target}} {{source}}", "-o /tmp/output.pdf /tmp/input.docx"},
	}

	for _, tt := range tests {
		result := Resolve(tt.input, vars)
		if result != tt.expected {
			t.Errorf("Resolve(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestResolveEnvVars(t *testing.T) {
	os.Setenv("TEST_COLLABORA_URL", "https://collabora.test")
	defer os.Unsetenv("TEST_COLLABORA_URL")

	vars := &TemplateVars{}

	result := Resolve("${TEST_COLLABORA_URL}/convert", vars)
	if result != "https://collabora.test/convert" {
		t.Errorf("got %q, want https://collabora.test/convert", result)
	}

	result = Resolve("${MISSING_VAR|https://fallback.test}/api", vars)
	if result != "https://fallback.test/api" {
		t.Errorf("got %q, want https://fallback.test/api", result)
	}
}

func TestSanitizeValue(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal", "normal"},
		{"file.txt", "file.txt"},
		{"hello world", "hello world"},
		{"; rm -rf /", "_ rm -rf /"},
		{"$(whoami)", "_whoami_"},
		{"`id`", "_id_"},
		{"foo|bar", "foo_bar"},
		{"a&b", "a_b"},
		{"a\\b", "a_b"},
		{"test\ninjection", "test_injection"},
		{"a{b}c", "a_b_c"},
	}

	for _, tt := range tests {
		result := SanitizeValue(tt.input)
		if result != tt.expected {
			t.Errorf("SanitizeValue(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestResolveUserInputSanitized(t *testing.T) {
	vars := &TemplateVars{
		Source:     "/tmp/safe",
		Target:     "/tmp/out",
		SourceName: "; rm -rf /",
		User:       UserInfo{DisplayName: "$(whoami)"},
	}

	result := Resolve("--author={{user.displayName}}", vars)
	if result != "--author=_whoami_" {
		t.Errorf("got %q, want --author=_whoami_", result)
	}

	result = Resolve("{{source.name}}", vars)
	if result != "_ rm -rf /" {
		t.Errorf("got %q, want sanitized", result)
	}

	// Internal paths should NOT be sanitized
	result = Resolve("{{source}}", vars)
	if result != "/tmp/safe" {
		t.Errorf("source path should not be sanitized, got %q", result)
	}
}

func TestResolveArgs(t *testing.T) {
	vars := &TemplateVars{
		Source: "/tmp/in.md",
		Target: "/tmp/out.pdf",
		User:   UserInfo{DisplayName: "Bob"},
	}

	args := []string{"{{source}}", "-o", "{{target}}", "--author={{user.displayName}}"}
	result := ResolveArgs(args, vars)

	expected := []string{"/tmp/in.md", "-o", "/tmp/out.pdf", "--author=Bob"}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, v, expected[i])
		}
	}
}
