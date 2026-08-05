package plugins

import (
	"time"
)

const (
	StatusReady       = "ready"
	StatusDisabled    = "disabled"
	StatusUninstalled = "uninstalled"
)

type Icon struct {
	Kind string `json:"kind,omitempty"`
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

type Author struct {
	Name  string `json:"name" validate:"required"`
	Email string `json:"email,omitempty"`
}

type PackageReference struct {
	RegistryID string `json:"registry_id" validate:"required"`
	PackageID  string `json:"package_id" validate:"required"`
}

type InstalledSkill struct {
	RegistryID string `json:"registry_id"`
	PackageID  string `json:"package_id"`
	SkillID    string `json:"skill_id"`
}

type InstalledPackage struct {
	RegistryID string `json:"registry_id"`
	PackageID  string `json:"package_id"`
	Revision   string `json:"revision"`
}

type ReleaseMetadata struct {
	Revision       string `json:"revision,omitempty"`
	ArtifactDigest string `json:"artifact_digest,omitempty"`
}

type Manifest struct {
	SchemaVersion string             `json:"schema_version" validate:"required"`
	ID            string             `json:"id" validate:"required"`
	Name          string             `json:"name" validate:"required"`
	Version       string             `json:"version" validate:"required"`
	Description   string             `json:"description" validate:"required"`
	Author        Author             `json:"author" validate:"required"`
	Icon          *Icon              `json:"icon,omitempty"`
	Homepage      string             `json:"homepage,omitempty"`
	Tags          []string           `json:"tags,omitempty"`
	Capabilities  []string           `json:"capabilities,omitempty"`
	Packages      []PackageReference `json:"packages,omitempty"`
}

type InstallPlan struct {
	Manifest          Manifest           `json:"manifest"`
	InstalledSkills   []InstalledSkill   `json:"-"`
	InstalledPackages []InstalledPackage `json:"-"`
	ReplacePackages   bool               `json:"-"`
	Release           ReleaseMetadata    `json:"-"`
	WorkspaceTargetID string             `json:"-"`
}

type Resource struct {
	ID         string         `json:"id"`
	Type       string         `json:"resource_type"`
	Key        string         `json:"resource_key"`
	ResourceID string         `json:"resource_id"`
	Status     string         `json:"status"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type Installation struct {
	ID                string         `json:"id"`
	BotID             string         `json:"bot_id"`
	PluginID          string         `json:"plugin_id"`
	PluginName        string         `json:"plugin_name"`
	Version           string         `json:"version"`
	Status            string         `json:"status"`
	Enabled           bool           `json:"enabled"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	Manifest          Manifest       `json:"manifest"`
	Resources         []Resource     `json:"resources,omitempty"`
	WorkspaceTargetID string         `json:"workspace_target_id,omitempty"`
	InstalledAt       time.Time      `json:"installed_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// InstalledPluginState identifies the mutable installation state used by
// Supermarket compare-and-set requests.
type InstalledPluginState struct {
	ReleaseRevision string
	UpdatedAt       time.Time
}

type ListResponse struct {
	Items []Installation `json:"items"`
}
