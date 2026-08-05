package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/memohai/memoh/internal/db"
	"github.com/memohai/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/memohai/memoh/internal/db/store"
	"github.com/memohai/memoh/internal/skillpackages"
	skillset "github.com/memohai/memoh/internal/skills"
	"github.com/memohai/memoh/internal/workspace/bridge"
)

type BridgeProvider struct {
	Provider bridge.Provider
}

type Service struct {
	queries dbstore.Queries
	bridges bridge.Provider
	logger  *slog.Logger
}

func NewService(log *slog.Logger, queries dbstore.Queries, bridges BridgeProvider) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		queries: queries,
		bridges: bridges.Provider,
		logger:  log.With(slog.String("service", "plugins")),
	}
}

func (s *Service) List(ctx context.Context, botID string) ([]Installation, error) {
	botUUID, err := db.ParseUUID(botID)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListBotPluginInstallations(ctx, botUUID)
	if err != nil {
		return nil, err
	}
	items := make([]Installation, 0, len(rows))
	for _, row := range rows {
		item, err := s.normalizeInstallation(ctx, row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, botID, installationID string) (Installation, error) {
	row, err := s.getRow(ctx, botID, installationID)
	if err != nil {
		return Installation{}, err
	}
	return s.normalizeInstallation(ctx, row)
}

// InstalledPluginRelease returns the immutable release currently owned by a
// bot/plugin identity. An installed Plugin without release metadata is still
// reported as installed so callers cannot mistake it for a new installation.
func (s *Service) InstalledPluginRelease(ctx context.Context, botID, pluginID string) (string, bool, error) {
	state, installed, err := s.InstalledPluginState(ctx, botID, pluginID)
	return state.ReleaseRevision, installed, err
}

// InstalledPluginState returns both the immutable release revision and the
// mutable installation generation represented by updated_at.
func (s *Service) InstalledPluginState(
	ctx context.Context,
	botID, pluginID string,
) (InstalledPluginState, bool, error) {
	botUUID, err := db.ParseUUID(botID)
	if err != nil {
		return InstalledPluginState{}, false, err
	}
	rows, err := s.queries.ListBotPluginInstallations(ctx, botUUID)
	if err != nil {
		return InstalledPluginState{}, false, err
	}
	for _, row := range rows {
		if row.PluginID != pluginID {
			continue
		}
		metadata, err := decodeJSONMap(row.Metadata)
		if err != nil {
			return InstalledPluginState{}, false, err
		}
		revision, _ := metadata["release_revision"].(string)
		return InstalledPluginState{
			ReleaseRevision: strings.TrimSpace(revision),
			UpdatedAt:       timeFromPg(row.UpdatedAt),
		}, true, nil
	}
	return InstalledPluginState{}, false, nil
}

func (s *Service) Install(ctx context.Context, botID string, req InstallPlan) (Installation, error) {
	var result Installation
	removals, err := s.prepareObsoletePackageRemovals(ctx, botID, req)
	if err != nil {
		return Installation{}, err
	}
	bundleRemoval, err := s.prepareObsoleteBundleRemoval(ctx, botID, req)
	if err != nil {
		return Installation{}, errors.Join(err, removals.rollback(ctx))
	}
	if err := s.inTransaction(ctx, func(txService *Service) error {
		var installErr error
		result, installErr = txService.install(ctx, botID, req)
		return installErr
	}); err != nil {
		return Installation{}, errors.Join(err, removals.rollback(ctx), bundleRemoval.rollback(ctx))
	}
	if err := removals.commit(ctx); err != nil {
		s.logger.Warn("cleanup obsolete Plugin Packages failed", slog.String("bot_id", botID), slog.String("plugin_id", req.Manifest.ID), slog.Any("error", err))
	}
	if err := bundleRemoval.commit(ctx); err != nil {
		s.logger.Warn("cleanup obsolete Plugin bundle failed", slog.String("bot_id", botID), slog.String("plugin_id", req.Manifest.ID), slog.Any("error", err))
	}
	return result, nil
}

func (s *Service) install(
	ctx context.Context,
	botID string,
	req InstallPlan,
) (Installation, error) {
	if s.queries == nil {
		return Installation{}, errors.New("plugin service is not configured")
	}
	botUUID, err := db.ParseUUID(botID)
	if err != nil {
		return Installation{}, err
	}
	manifest := normalizeManifest(req.Manifest)
	if err := ValidateManifest(manifest); err != nil {
		return Installation{}, err
	}
	if err := validateInstalledSkills(manifest.Packages, req.InstalledSkills); err != nil {
		return Installation{}, err
	}
	if err := validateInstalledPackages(manifest.Packages, req.InstalledPackages); err != nil {
		return Installation{}, err
	}
	if err := validateReleaseMetadata(req.Release); err != nil {
		return Installation{}, err
	}
	if manifest.Name == "" {
		manifest.Name = manifest.ID
	}
	configPayload, err := encodeJSON(map[string]any{})
	if err != nil {
		return Installation{}, err
	}
	metadata := manifestMetadata(manifest)
	if req.Release.Revision != "" {
		metadata["release_revision"] = req.Release.Revision
		metadata["plugin_artifact_digest"] = req.Release.ArtifactDigest
	}
	metadataPayload, err := encodeJSON(metadata)
	if err != nil {
		return Installation{}, err
	}
	manifestPayload, err := encodeJSON(manifest)
	if err != nil {
		return Installation{}, err
	}

	workspaceTargetID := strings.TrimSpace(req.WorkspaceTargetID)
	if workspaceTargetID == "" {
		workspaceTargetID = "native"
	}
	row, err := s.queries.CreateBotPluginInstallation(ctx, sqlc.CreateBotPluginInstallationParams{
		BotID:             botUUID,
		PluginID:          manifest.ID,
		PluginName:        manifest.Name,
		Version:           manifest.Version,
		Status:            StatusReady,
		Enabled:           true,
		Config:            configPayload,
		Metadata:          metadataPayload,
		Manifest:          manifestPayload,
		WorkspaceTargetID: workspaceTargetID,
	})
	if err != nil {
		return Installation{}, err
	}

	if err := s.queries.DeleteBotPluginResources(ctx, row.ID); err != nil {
		return Installation{}, err
	}
	if req.ReplacePackages {
		packageRequirements := make([]skillpackages.Requirement, 0, len(req.InstalledPackages))
		for _, pkg := range req.InstalledPackages {
			packageRequirements = append(packageRequirements, skillpackages.Requirement{
				RegistryID: pkg.RegistryID, PackageID: pkg.PackageID, Revision: pkg.Revision,
			})
		}
		if _, err := skillpackages.ReplacePluginReferences(ctx, s.queries, botUUID, row.ID, req.WorkspaceTargetID, packageRequirements); err != nil {
			return Installation{}, err
		}
	}

	for _, resource := range req.InstalledSkills {
		dirPath, err := skillset.SkillDirForIDs(resource.RegistryID, resource.PackageID, resource.SkillID)
		if err != nil {
			return Installation{}, fmt.Errorf("installed Plugin Skill %q is invalid", InstalledSkillIdentity(resource))
		}
		identity := InstalledSkillIdentity(resource)
		metadata := map[string]any{
			"registry_id": resource.RegistryID,
			"package_id":  resource.PackageID,
			"skill_id":    resource.SkillID,
		}
		if workspaceTargetID := strings.TrimSpace(req.WorkspaceTargetID); workspaceTargetID != "" {
			metadata["workspace_target_id"] = workspaceTargetID
		}
		if _, err := s.queries.UpsertBotPluginResource(ctx, sqlc.UpsertBotPluginResourceParams{
			InstallationID: row.ID,
			ResourceType:   "skill",
			ResourceKey:    identity,
			ResourceID:     path.Join(dirPath, "SKILL.md"),
			Status:         "installed",
			Metadata:       mustJSON(metadata),
		}); err != nil {
			return Installation{}, err
		}
	}
	return s.normalizeInstallation(ctx, row)
}

func (s *Service) SetEnabled(ctx context.Context, botID, installationID string, enabled bool) (Installation, error) {
	var result Installation
	err := s.inTransaction(ctx, func(txService *Service) error {
		var updateErr error
		result, updateErr = txService.setEnabled(ctx, botID, installationID, enabled)
		return updateErr
	})
	return result, err
}

func (s *Service) setEnabled(ctx context.Context, botID, installationID string, enabled bool) (Installation, error) {
	row, err := s.getRow(ctx, botID, installationID)
	if err != nil {
		return Installation{}, err
	}
	if !enabled {
		updated, err := s.updateStatus(ctx, botID, installationID, StatusDisabled, false)
		if err != nil {
			return Installation{}, err
		}
		return s.normalizeInstallation(ctx, updated)
	}

	if row.Status == StatusUninstalled {
		return Installation{}, errors.New("plugin is uninstalled")
	}
	updated, err := s.updateStatus(ctx, botID, installationID, StatusReady, true)
	if err != nil {
		return Installation{}, err
	}
	return s.normalizeInstallation(ctx, updated)
}

func (s *Service) Uninstall(ctx context.Context, botID, installationID string) (Installation, error) {
	var result Installation
	row, err := s.getRow(ctx, botID, installationID)
	if err != nil {
		return Installation{}, err
	}
	packageRemovals, err := s.prepareUnownedPackageRemovals(ctx, botID, row)
	if err != nil {
		return Installation{}, err
	}
	var bundleRemoval *pluginBundleRemoval
	if row.Status != StatusUninstalled {
		bundleRemoval, err = s.preparePluginBundleRemoval(ctx, botID, row)
		if err != nil {
			return Installation{}, errors.Join(err, packageRemovals.rollback(ctx))
		}
	}
	if err := s.inTransaction(ctx, func(txService *Service) error {
		var uninstallErr error
		result, uninstallErr = txService.uninstall(ctx, botID, installationID)
		return uninstallErr
	}); err != nil {
		return Installation{}, errors.Join(
			err,
			packageRemovals.rollback(ctx),
			bundleRemoval.rollback(ctx),
		)
	}
	if err := packageRemovals.commit(ctx); err != nil {
		s.logger.Warn(
			"cleanup unowned Plugin Packages failed",
			slog.String("bot_id", botID),
			slog.String("plugin_id", row.PluginID),
			slog.Any("error", err),
		)
	}
	if err := bundleRemoval.commit(ctx); err != nil {
		s.logger.Warn(
			"cleanup removed Plugin bundle failed",
			slog.String("bot_id", botID),
			slog.String("plugin_id", row.PluginID),
			slog.Any("error", err),
		)
	}
	return result, nil
}

func (s *Service) uninstall(
	ctx context.Context,
	botID, installationID string,
) (Installation, error) {
	row, err := s.getRow(ctx, botID, installationID)
	if err != nil {
		return Installation{}, err
	}
	if err := s.queries.DeleteBotPluginResources(ctx, row.ID); err != nil {
		return Installation{}, err
	}
	botUUID, err := db.ParseUUID(botID)
	if err != nil {
		return Installation{}, err
	}
	if _, err := skillpackages.ReplacePluginReferences(ctx, s.queries, botUUID, row.ID, "", nil); err != nil {
		return Installation{}, err
	}
	updated, err := s.updateStatus(ctx, botID, installationID, StatusUninstalled, false)
	if err != nil {
		return Installation{}, err
	}
	return s.normalizeInstallation(ctx, updated)
}

func (s *Service) Purge(ctx context.Context, botID, installationID string) error {
	row, err := s.getRow(ctx, botID, installationID)
	if err != nil {
		return err
	}
	packageRemovals, err := s.prepareUnownedPackageRemovals(ctx, botID, row)
	if err != nil {
		return err
	}
	var bundleRemoval *pluginBundleRemoval
	if row.Status != StatusUninstalled {
		bundleRemoval, err = s.preparePluginBundleRemoval(ctx, botID, row)
		if err != nil {
			return errors.Join(err, packageRemovals.rollback(ctx))
		}
	}
	if err := s.inTransaction(ctx, func(txService *Service) error {
		return txService.purge(ctx, botID, installationID)
	}); err != nil {
		return errors.Join(
			err,
			packageRemovals.rollback(ctx),
			bundleRemoval.rollback(ctx),
		)
	}
	if err := packageRemovals.commit(ctx); err != nil {
		s.logger.Warn(
			"cleanup unowned Plugin Packages failed",
			slog.String("bot_id", botID),
			slog.String("plugin_id", row.PluginID),
			slog.Any("error", err),
		)
	}
	if err := bundleRemoval.commit(ctx); err != nil {
		s.logger.Warn(
			"cleanup purged Plugin bundle failed",
			slog.String("bot_id", botID),
			slog.String("plugin_id", row.PluginID),
			slog.Any("error", err),
		)
	}
	return nil
}

func (s *Service) purge(
	ctx context.Context,
	botID, installationID string,
) error {
	row, err := s.getRow(ctx, botID, installationID)
	if err != nil {
		return err
	}
	if err := s.queries.DeleteBotPluginResources(ctx, row.ID); err != nil {
		return err
	}
	botUUID, err := db.ParseUUID(botID)
	if err != nil {
		return err
	}
	if _, err := skillpackages.ReplacePluginReferences(ctx, s.queries, botUUID, row.ID, "", nil); err != nil {
		return err
	}
	installationUUID, err := db.ParseUUID(installationID)
	if err != nil {
		return err
	}
	if err := s.queries.DeleteBotPluginInstallation(ctx, sqlc.DeleteBotPluginInstallationParams{
		BotID: botUUID,
		ID:    installationUUID,
	}); err != nil {
		return err
	}
	return nil
}

func (s *Service) getRow(ctx context.Context, botID, installationID string) (sqlc.BotPluginInstallation, error) {
	botUUID, err := db.ParseUUID(botID)
	if err != nil {
		return sqlc.BotPluginInstallation{}, err
	}
	installationUUID, err := db.ParseUUID(installationID)
	if err != nil {
		return sqlc.BotPluginInstallation{}, err
	}
	return s.queries.GetBotPluginInstallationByID(ctx, sqlc.GetBotPluginInstallationByIDParams{
		BotID: botUUID,
		ID:    installationUUID,
	})
}

func (s *Service) updateStatus(ctx context.Context, botID, installationID, status string, enabled bool) (sqlc.BotPluginInstallation, error) {
	botUUID, err := db.ParseUUID(botID)
	if err != nil {
		return sqlc.BotPluginInstallation{}, err
	}
	installationUUID, err := db.ParseUUID(installationID)
	if err != nil {
		return sqlc.BotPluginInstallation{}, err
	}
	return s.queries.UpdateBotPluginInstallationStatus(ctx, sqlc.UpdateBotPluginInstallationStatusParams{
		BotID:   botUUID,
		ID:      installationUUID,
		Status:  status,
		Enabled: enabled,
	})
}

func (s *Service) normalizeInstallation(ctx context.Context, row sqlc.BotPluginInstallation) (Installation, error) {
	manifest, err := decodeManifest(row.Manifest)
	if err != nil {
		return Installation{}, err
	}
	metadata, err := decodeJSONMap(row.Metadata)
	if err != nil {
		return Installation{}, err
	}
	resources, err := s.queries.ListBotPluginResources(ctx, row.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Installation{}, err
	}
	outResources := make([]Resource, 0, len(resources))
	for _, resource := range resources {
		item, err := normalizeResource(resource)
		if err != nil {
			return Installation{}, err
		}
		outResources = append(outResources, item)
	}
	return Installation{
		ID:                row.ID.String(),
		BotID:             row.BotID.String(),
		PluginID:          row.PluginID,
		PluginName:        row.PluginName,
		Version:           row.Version,
		Status:            row.Status,
		Enabled:           row.Enabled,
		Metadata:          metadata,
		Manifest:          manifest,
		Resources:         outResources,
		WorkspaceTargetID: row.WorkspaceTargetID,
		InstalledAt:       timeFromPg(row.InstalledAt),
		UpdatedAt:         timeFromPg(row.UpdatedAt),
	}, nil
}

func normalizeResource(row sqlc.BotPluginResource) (Resource, error) {
	metadata, err := decodeJSONMap(row.Metadata)
	if err != nil {
		return Resource{}, err
	}
	return Resource{
		ID:         row.ID.String(),
		Type:       row.ResourceType,
		Key:        row.ResourceKey,
		ResourceID: row.ResourceID,
		Status:     row.Status,
		Metadata:   metadata,
		CreatedAt:  timeFromPg(row.CreatedAt),
		UpdatedAt:  timeFromPg(row.UpdatedAt),
	}, nil
}

func normalizeManifest(manifest Manifest) Manifest {
	manifest.SchemaVersion = strings.TrimSpace(manifest.SchemaVersion)
	manifest.ID = strings.TrimSpace(manifest.ID)
	manifest.Name = strings.TrimSpace(manifest.Name)
	manifest.Version = strings.TrimSpace(manifest.Version)
	manifest.Description = strings.TrimSpace(manifest.Description)
	manifest.Author.Name = strings.TrimSpace(manifest.Author.Name)
	manifest.Author.Email = strings.TrimSpace(manifest.Author.Email)
	manifest.Homepage = strings.TrimSpace(manifest.Homepage)
	manifest.Install = normalizeInstallCommands(manifest.Install)
	for i := range manifest.Packages {
		manifest.Packages[i].RegistryID = strings.TrimSpace(manifest.Packages[i].RegistryID)
		manifest.Packages[i].PackageID = strings.TrimSpace(manifest.Packages[i].PackageID)
	}
	return manifest
}

func NormalizeManifest(manifest Manifest) Manifest {
	return normalizeManifest(manifest)
}

func normalizeInstallCommands(commands []string) InstallCommands {
	out := make([]string, 0, len(commands))
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		out = append(out, command)
	}
	return InstallCommands(out)
}

func manifestMetadata(manifest Manifest) map[string]any {
	return map[string]any{
		"icon":         manifest.Icon,
		"tags":         manifest.Tags,
		"capabilities": manifest.Capabilities,
		"homepage":     manifest.Homepage,
	}
}

func encodeJSON(value any) ([]byte, error) {
	if value == nil {
		value = map[string]any{}
	}
	return json.Marshal(value)
}

func mustJSON(value any) []byte {
	payload, _ := encodeJSON(value)
	return payload
}

func decodeJSONMap(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func decodeManifest(raw []byte) (Manifest, error) {
	if len(raw) == 0 {
		return Manifest{}, nil
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, err
	}
	return normalizeManifest(manifest), nil
}

func timeFromPg(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return db.TimeFromPg(value)
}

var artifactDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != "1" {
		return errors.New("plugin manifest schema_version must be 1")
	}
	if !skillset.IsValidPluginID(manifest.ID) {
		return errors.New("plugin manifest id is invalid")
	}
	if manifest.Name == "" || manifest.Version == "" || manifest.Description == "" || manifest.Author.Name == "" {
		return errors.New("plugin manifest metadata is incomplete")
	}
	if manifest.Icon != nil {
		switch manifest.Icon.Kind {
		case "builtin":
			if strings.TrimSpace(manifest.Icon.Name) == "" {
				return errors.New("plugin builtin icon name is required")
			}
		case "external_url":
			if strings.TrimSpace(manifest.Icon.URL) == "" {
				return errors.New("plugin external icon URL is required")
			}
		default:
			return errors.New("plugin icon kind is invalid")
		}
	}
	return ValidatePackageReferences(manifest.Packages)
}

func PackageReferenceIdentity(reference PackageReference) string {
	return reference.RegistryID + "/" + reference.PackageID
}

func ValidatePackageReferences(references []PackageReference) error {
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		identity := PackageReferenceIdentity(reference)
		if !skillset.IsValidRegistryID(reference.RegistryID) ||
			!skillset.IsValidRegistryComponent(reference.PackageID) {
			return fmt.Errorf("plugin Package reference %q is invalid", identity)
		}
		if _, ok := seen[identity]; ok {
			return fmt.Errorf("plugin Package reference %q is duplicated", identity)
		}
		seen[identity] = struct{}{}
	}
	return nil
}

func InstalledSkillIdentity(skill InstalledSkill) string {
	return skill.RegistryID + "/" + skill.PackageID + "/" + skill.SkillID
}

func validateInstalledSkills(packages []PackageReference, skills []InstalledSkill) error {
	allowed := make(map[string]struct{}, len(packages))
	counts := make(map[string]int, len(packages))
	for _, reference := range packages {
		identity := PackageReferenceIdentity(reference)
		allowed[identity] = struct{}{}
		counts[identity] = 0
	}
	seen := make(map[string]struct{}, len(skills))
	for _, skill := range skills {
		identity := InstalledSkillIdentity(skill)
		if !skillset.IsValidRegistryID(skill.RegistryID) ||
			!skillset.IsValidRegistryComponent(skill.PackageID) ||
			!skillset.IsValidRegistryComponent(skill.SkillID) {
			return fmt.Errorf("installed Plugin Skill %q is invalid", identity)
		}
		packageIdentity := PackageReferenceIdentity(PackageReference{
			RegistryID: skill.RegistryID, PackageID: skill.PackageID,
		})
		if _, ok := allowed[packageIdentity]; !ok {
			return fmt.Errorf("installed Plugin Skill %q does not belong to a referenced Package", identity)
		}
		if _, ok := seen[identity]; ok {
			return fmt.Errorf("installed Plugin Skill %q is duplicated", identity)
		}
		seen[identity] = struct{}{}
		counts[packageIdentity]++
	}
	for identity, count := range counts {
		if count == 0 {
			return fmt.Errorf("plugin Package %q installed no Skills", identity)
		}
	}
	return nil
}

func validateReleaseMetadata(release ReleaseMetadata) error {
	if release.Revision == "" && release.ArtifactDigest == "" {
		return nil
	}
	if !artifactDigestPattern.MatchString(release.Revision) ||
		!artifactDigestPattern.MatchString(release.ArtifactDigest) {
		return errors.New("plugin release metadata is invalid")
	}
	return nil
}

func validateInstalledPackages(references []PackageReference, installed []InstalledPackage) error {
	if len(references) != len(installed) {
		return errors.New("installed Plugin Packages do not match the manifest")
	}
	expected := make(map[string]struct{}, len(references))
	for _, reference := range references {
		expected[PackageReferenceIdentity(reference)] = struct{}{}
	}
	seen := make(map[string]struct{}, len(installed))
	for _, pkg := range installed {
		identity := PackageReferenceIdentity(PackageReference{RegistryID: pkg.RegistryID, PackageID: pkg.PackageID})
		if _, ok := expected[identity]; !ok || !artifactDigestPattern.MatchString(pkg.Revision) {
			return fmt.Errorf("installed Plugin Package %q is invalid", identity)
		}
		if _, exists := seen[identity]; exists {
			return fmt.Errorf("installed Plugin Package %q is duplicated", identity)
		}
		seen[identity] = struct{}{}
	}
	return nil
}
