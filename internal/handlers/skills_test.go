package handlers

import "testing"

func TestParseSkillFile_NoFrontmatterFallbacks(t *testing.T) {
	raw := "# Use this skill\n\nDo something useful."
	got := parseSkillFile(raw, "plain-skill")

	if got.Name != "plain-skill" {
		t.Fatalf("expected name plain-skill, got %q", got.Name)
	}
	if got.Description != "plain-skill" {
		t.Fatalf("expected description plain-skill, got %q", got.Description)
	}
	if got.Content != raw {
		t.Fatalf("expected content to keep original markdown, got %q", got.Content)
	}
}

func TestParseSkillFile_FrontmatterDescriptionFallback(t *testing.T) {
	raw := "---\nname: hello-skill\n---\n\nBody content"
	got := parseSkillFile(raw, "fallback")

	if got.Name != "hello-skill" {
		t.Fatalf("expected frontmatter name hello-skill, got %q", got.Name)
	}
	if got.Description != "hello-skill" {
		t.Fatalf("expected description fallback to name, got %q", got.Description)
	}
	if got.Content != "Body content" {
		t.Fatalf("expected content Body content, got %q", got.Content)
	}
}

func TestParseSkillFile_EmptyBodyFallbacksToDescription(t *testing.T) {
	raw := "---\nname: hello-skill\ndescription: say hello\n---\n"
	got := parseSkillFile(raw, "fallback")

	if got.Content != "say hello" {
		t.Fatalf("expected content fallback to description, got %q", got.Content)
	}
}
