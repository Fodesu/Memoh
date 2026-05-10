package orchestrationexec

import (
	"context"
	"fmt"
	"sort"
	"strings"

	sdk "github.com/memohai/twilight-ai/sdk"

	sqlc "github.com/memohai/memoh/internal/db/postgres/sqlc"
)

const listEnvResourcesToolName = "list_env_resources"

type listEnvResourcesInput struct{}

type envResourceCatalog struct {
	enforce bool
	byName  map[string]envResourceCatalogItem
}

type envResourceCatalogItem struct {
	Name   string
	Kind   string
	Status string
}

func (r *Runtime) loadEnvResourceCatalog(ctx context.Context, tenantID string) (envResourceCatalog, error) {
	tenantID = strings.TrimSpace(tenantID)
	if r == nil || r.queries == nil || tenantID == "" {
		return envResourceCatalog{}, nil
	}
	rows, err := r.queries.ListOrchestrationEnvResourcesByTenant(ctx, tenantID)
	if err != nil {
		return envResourceCatalog{}, fmt.Errorf("list env resources: %w", err)
	}
	return newEnvResourceCatalog(rows), nil
}

func newEnvResourceCatalog(rows []sqlc.OrchestrationEnvResource) envResourceCatalog {
	catalog := envResourceCatalog{
		enforce: true,
		byName:  make(map[string]envResourceCatalogItem, len(rows)),
	}
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		catalog.byName[name] = envResourceCatalogItem{
			Name:   name,
			Kind:   strings.TrimSpace(row.Kind),
			Status: strings.TrimSpace(row.Status),
		}
	}
	return catalog
}

func validateEnvResourceReference(catalog envResourceCatalog, kind string, resourceName string) error {
	if !catalog.enforce {
		return nil
	}
	resource, ok := catalog.byName[resourceName]
	if !ok {
		return fmt.Errorf("env_preconditions.resource_name %q must reference an existing env resource", resourceName)
	}
	if resource.Status != "active" {
		return fmt.Errorf("env_preconditions.resource_name %q references a %s env resource", resourceName, resource.Status)
	}
	if resource.Kind != kind {
		return fmt.Errorf("env_preconditions.resource_name %q has kind %q, not %q", resourceName, resource.Kind, kind)
	}
	return nil
}

func (c envResourceCatalog) PromptItems() []map[string]any {
	if !c.enforce {
		return nil
	}
	items := make([]map[string]any, 0, len(c.byName))
	names := make([]string, 0, len(c.byName))
	for _, resource := range c.byName {
		if resource.Status != "active" {
			continue
		}
		names = append(names, resource.Name)
	}
	sort.Strings(names)
	for _, name := range names {
		resource := c.byName[name]
		items = append(items, map[string]any{
			"name":   resource.Name,
			"kind":   resource.Kind,
			"status": resource.Status,
		})
	}
	return items
}

func newListEnvResourcesTool(catalog envResourceCatalog) sdk.Tool {
	return sdk.NewTool[listEnvResourcesInput](
		listEnvResourcesToolName,
		"List active orchestration environment resources that may be referenced by env_preconditions.resource_name.",
		func(_ *sdk.ToolExecContext, _ listEnvResourcesInput) (any, error) {
			return map[string]any{
				"resources": catalog.PromptItems(),
			}, nil
		},
	)
}
