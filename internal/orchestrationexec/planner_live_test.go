package orchestrationexec

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	sdk "github.com/memohai/twilight-ai/sdk"

	agentpkg "github.com/memohai/memoh/internal/agent"
	"github.com/memohai/memoh/internal/models"
	"github.com/memohai/memoh/internal/orchestration"
)

const (
	livePlannerAPIKeyEnv     = "MEMOH_ORCHESTRATION_PLANNER_TEST_API_KEY"
	livePlannerBaseURLEnv    = "MEMOH_ORCHESTRATION_PLANNER_TEST_BASE_URL"
	livePlannerModelEnv      = "MEMOH_ORCHESTRATION_PLANNER_TEST_MODEL"
	livePlannerClientTypeEnv = "MEMOH_ORCHESTRATION_PLANNER_TEST_CLIENT_TYPE"
)

func TestLivePlannerUsesBrowserEnvPreconditions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live planner test in short mode")
	}
	apiKey := strings.TrimSpace(os.Getenv(livePlannerAPIKeyEnv))
	baseURL := strings.TrimSpace(os.Getenv(livePlannerBaseURLEnv))
	modelID := strings.TrimSpace(os.Getenv(livePlannerModelEnv))
	clientType := strings.TrimSpace(os.Getenv(livePlannerClientTypeEnv))
	if clientType == "" {
		clientType = string(models.ClientTypeOpenAICompletions)
	}
	if apiKey == "" || baseURL == "" || modelID == "" {
		t.Skipf("set %s, %s, and %s to run live planner test", livePlannerAPIKeyEnv, livePlannerBaseURLEnv, livePlannerModelEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	input := orchestration.StartRunPlanningInput{
		Run: orchestration.Run{
			ID:             "run-live-browser-planner",
			OwnerSubject:   "live-planner-test",
			Goal:           "Create a small plan with one task that uses the browser environment to open https://example.com and report the page title, then summarize the observation.",
			Input:          map[string]any{},
			OutputSchema:   map[string]any{},
			SourceMetadata: map[string]any{},
		},
		RootTask: orchestration.Task{
			ID:            "root-live-browser-planner",
			Goal:          "Use browser Env to open https://example.com, observe the title, and report what happened.",
			Inputs:        map[string]any{},
			WorkerProfile: orchestration.DefaultRootWorkerProfile,
		},
	}
	cfg := agentpkg.RunConfig{
		Model: models.NewSDKChatModel(models.SDKModelConfig{
			ModelID:    modelID,
			ClientType: clientType,
			APIKey:     apiKey,
			BaseURL:    baseURL,
		}),
		System:           startRunPlannerSystemPrompt,
		Messages:         []sdk.Message{sdk.UserMessage(buildStartRunPlannerPrompt(input))},
		SupportsToolCall: true,
	}

	plan, err := (&Runtime{}).generatePlannerToolPlan(ctx, cfg, rootPlannerSubmitPlanToolName, false, true, testEnvResourceCatalog())
	if err != nil {
		t.Fatalf("generatePlannerToolPlan() error = %v", err)
	}
	foundBrowserEnv := false
	if plan.RootTask != nil && plan.RootTask.EnvPreconditions.Required && plan.RootTask.EnvPreconditions.Kind == orchestration.EnvPreconditionsKindBrowser {
		foundBrowserEnv = true
		if plan.RootTask.EnvPreconditions.ResourceName == "" {
			t.Fatalf("root task browser env missing resource_name: %#v", plan.RootTask.EnvPreconditions)
		}
	}
	for index, task := range plan.ChildTasks {
		if strings.EqualFold(strings.TrimSpace(task.WorkerProfile), "browser-capable") {
			t.Fatalf("child task %d used invalid worker_profile %q; browser must be env_preconditions", index, task.WorkerProfile)
		}
		if task.EnvPreconditions.Required && task.EnvPreconditions.Kind == orchestration.EnvPreconditionsKindBrowser {
			foundBrowserEnv = true
			if task.EnvPreconditions.ResourceName == "" {
				t.Fatalf("child task %d browser env missing resource_name: %#v", index, task.EnvPreconditions)
			}
		}
	}
	if !foundBrowserEnv {
		t.Fatalf("planner did not produce a browser env-bound root or child task: root=%#v child_tasks=%#v", plan.RootTask, plan.ChildTasks)
	}
}
