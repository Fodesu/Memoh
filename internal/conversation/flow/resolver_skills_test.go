package flow

import "testing"

func TestNormalizeGatewaySkill_Fallbacks(t *testing.T) {
	got, ok := normalizeGatewaySkill(SkillEntry{
		Name: "  demo-skill  ",
	})
	if !ok {
		t.Fatal("expected valid skill")
	}
	if got.Name != "demo-skill" {
		t.Fatalf("expected trimmed name demo-skill, got %q", got.Name)
	}
	if got.Description != "demo-skill" {
		t.Fatalf("expected description fallback to name, got %q", got.Description)
	}
	if got.Content != "demo-skill" {
		t.Fatalf("expected content fallback to description, got %q", got.Content)
	}
}

func TestNormalizeGatewaySkill_RejectsEmptyName(t *testing.T) {
	_, ok := normalizeGatewaySkill(SkillEntry{
		Name:        "   ",
		Description: "desc",
		Content:     "content",
	})
	if ok {
		t.Fatal("expected invalid skill when name is empty")
	}
}
