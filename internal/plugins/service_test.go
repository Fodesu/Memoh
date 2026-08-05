package plugins

import (
	"strings"
	"testing"
)

func TestValidateManifestRejectsIncompleteMetadata(t *testing.T) {
	valid := Manifest{
		SchemaVersion: "1", ID: "notion", Name: "Notion", Version: "1.0.0",
		Description: "Notion workflows", Author: Author{Name: "Memoh"},
	}
	if err := ValidateManifest(valid); err != nil {
		t.Fatalf("ValidateManifest(valid) error = %v", err)
	}
	valid.Description = ""
	if err := ValidateManifest(valid); err == nil {
		t.Fatal("ValidateManifest() accepted incomplete metadata")
	}
}

func TestValidatePackageReferencesRequiresNamespacedUniqueIdentity(t *testing.T) {
	reference := PackageReference{RegistryID: "memoh", PackageID: "github"}
	if err := ValidatePackageReferences([]PackageReference{reference}); err != nil {
		t.Fatalf("ValidatePackageReferences(valid) error = %v", err)
	}
	if got := PackageReferenceIdentity(reference); got != "memoh/github" {
		t.Fatalf("PackageReferenceIdentity() = %q", got)
	}
	if err := ValidatePackageReferences([]PackageReference{reference, reference}); err == nil {
		t.Fatal("ValidatePackageReferences() accepted a duplicate reference")
	}
	dotted := PackageReference{RegistryID: "openai.api", PackageID: "documents.v2"}
	if err := ValidatePackageReferences([]PackageReference{dotted}); err != nil {
		t.Fatalf("ValidatePackageReferences(dotted) error = %v", err)
	}
	reference.RegistryID = "Not Valid"
	if err := ValidatePackageReferences([]PackageReference{reference}); err == nil {
		t.Fatal("ValidatePackageReferences() accepted an invalid Registry ID")
	}
	for _, invalid := range []PackageReference{
		{RegistryID: "user", PackageID: "github"},
		{RegistryID: "memoh", PackageID: "github..v2"},
		{RegistryID: "memoh", PackageID: "nul.txt"},
	} {
		if err := ValidatePackageReferences([]PackageReference{invalid}); err == nil {
			t.Fatalf("ValidatePackageReferences() accepted invalid reference %+v", invalid)
		}
	}
}

func TestValidateInstalledPackagesRequiresPinnedManifestPackages(t *testing.T) {
	references := []PackageReference{{RegistryID: "memoh", PackageID: "notion"}}
	installed := []InstalledPackage{{RegistryID: "memoh", PackageID: "notion", Revision: strings.Repeat("b", 64)}}
	if err := validateInstalledPackages(references, installed); err != nil {
		t.Fatalf("validateInstalledPackages(valid) error = %v", err)
	}
	if err := validateInstalledPackages(references, nil); err == nil {
		t.Fatal("validateInstalledPackages() accepted a missing Package")
	}
	installed[0].Revision = "not-a-digest"
	if err := validateInstalledPackages(references, installed); err == nil {
		t.Fatal("validateInstalledPackages() accepted an invalid revision")
	}
}

func TestValidateInstalledSkillsRequiresEveryReferencedPackage(t *testing.T) {
	packages := []PackageReference{{RegistryID: "memoh", PackageID: "notion"}}
	if err := validateInstalledSkills(packages, nil); err == nil {
		t.Fatal("validateInstalledSkills() accepted a Package without installed Skills")
	}
	skill := InstalledSkill{RegistryID: "memoh", PackageID: "notion", SkillID: "meeting"}
	if err := validateInstalledSkills(packages, []InstalledSkill{skill}); err != nil {
		t.Fatalf("validateInstalledSkills(valid) error = %v", err)
	}
	skill.PackageID = "other"
	if err := validateInstalledSkills(packages, []InstalledSkill{skill}); err == nil {
		t.Fatal("validateInstalledSkills() accepted a Skill outside the referenced Package")
	}
}
