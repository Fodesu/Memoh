package agent

import (
	"embed"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	skillset "github.com/memohai/memoh/internal/skills"
)

//go:embed prompts/*.md
var promptsFS embed.FS

var (
	systemChatTmpl                      string
	systemDiscussTmpl                   string
	systemHeartbeatTmpl                 string
	systemScheduleTmpl                  string
	systemSubagentTmpl                  string
	systemOrchestrationAttemptTmpl      string
	systemOrchestrationVerificationTmpl string
	scheduleTmpl                        string
	heartbeatTmpl                       string

	MemoryExtractPrompt string
	MemoryUpdatePrompt  string

	includes map[string]string
)

var includeRe = regexp.MustCompile(`\{\{include:(\w+)\}\}`)

func init() {
	systemChatTmpl = mustReadPrompt("prompts/system_chat.md")
	systemDiscussTmpl = mustReadPrompt("prompts/system_discuss.md")
	systemHeartbeatTmpl = mustReadPrompt("prompts/system_heartbeat.md")
	systemScheduleTmpl = mustReadPrompt("prompts/system_schedule.md")
	systemSubagentTmpl = mustReadPrompt("prompts/system_subagent.md")
	systemOrchestrationAttemptTmpl = mustReadPrompt("prompts/system_orchestration_attempt.md")
	systemOrchestrationVerificationTmpl = mustReadPrompt("prompts/system_orchestration_verification.md")
	scheduleTmpl = mustReadPrompt("prompts/schedule.md")
	heartbeatTmpl = mustReadPrompt("prompts/heartbeat.md")
	MemoryExtractPrompt = mustReadPrompt("prompts/memory_extract.md")
	MemoryUpdatePrompt = mustReadPrompt("prompts/memory_update.md")

	includes = map[string]string{
		"_memory":        mustReadPrompt("prompts/_memory.md"),
		"_tools":         mustReadPrompt("prompts/_tools.md"),
		"_contacts":      mustReadPrompt("prompts/_contacts.md"),
		"_identities":    mustReadPrompt("prompts/_identities.md"),
		"_schedule_task": mustReadPrompt("prompts/_schedule_task.md"),
		"_subagent":      mustReadPrompt("prompts/_subagent.md"),
	}

	systemChatTmpl = resolveIncludes(systemChatTmpl)
	systemDiscussTmpl = resolveIncludes(systemDiscussTmpl)
	systemHeartbeatTmpl = resolveIncludes(systemHeartbeatTmpl)
	systemScheduleTmpl = resolveIncludes(systemScheduleTmpl)
	systemSubagentTmpl = resolveIncludes(systemSubagentTmpl)
	systemOrchestrationAttemptTmpl = resolveIncludes(systemOrchestrationAttemptTmpl)
	systemOrchestrationVerificationTmpl = resolveIncludes(systemOrchestrationVerificationTmpl)
}

func mustReadPrompt(name string) string {
	data, err := promptsFS.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("failed to read embedded prompt %s: %v", name, err))
	}
	return string(data)
}

// resolveIncludes replaces {{include:_name}} placeholders with the content of the named fragment.
func resolveIncludes(tmpl string) string {
	return includeRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		sub := includeRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		content, ok := includes[sub[1]]
		if !ok {
			return match
		}
		return strings.TrimSpace(content)
	})
}

// render replaces all {{key}} placeholders in tmpl with values from vars.
func render(tmpl string, vars map[string]string) string {
	result := tmpl
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{{"+k+"}}", v)
	}
	return strings.TrimSpace(result)
}

func selectSystemTemplate(sessionType string) string {
	switch sessionType {
	case "discuss":
		return systemDiscussTmpl
	case "heartbeat":
		return systemHeartbeatTmpl
	case "schedule":
		return systemScheduleTmpl
	case "subagent":
		return systemSubagentTmpl
	case "orchestration_attempt":
		return systemOrchestrationAttemptTmpl
	case "orchestration_verification":
		return systemOrchestrationVerificationTmpl
	default:
		return systemChatTmpl
	}
}

func OrchestrationAttemptSystemPrompt() string {
	return GenerateSystemPrompt(SystemPromptParams{SessionType: "orchestration_attempt"})
}

func OrchestrationVerificationSystemPrompt() string {
	return GenerateSystemPrompt(SystemPromptParams{SessionType: "orchestration_verification"})
}

// GenerateSystemPrompt builds the complete system prompt from files, skills, and context.
func GenerateSystemPrompt(params SystemPromptParams) string {
	home := "/data"
	now := params.Now
	if now.IsZero() {
		now = TimeNow()
	}
	timezoneName := strings.TrimSpace(params.Timezone)
	if timezoneName == "" {
		timezoneName = "UTC"
	}

	readToolDesc := "- `read`: read file content"
	if params.SupportsImageInput {
		readToolDesc += " (also supports images: PNG, JPEG, GIF, WebP)"
	}
	basicTools := []string{readToolDesc}
	basicTools = append(basicTools,
		"- `write`: write file content",
		"- `list`: list directory entries",
		"- `edit`: replace exact text in a file",
		"- `exec`: execute command",
	)

	displayTools := buildDisplayToolsSection(params.DisplayEnabled)

	skillsSection := buildSkillsSection(params.Skills)

	fileSections := ""
	var fileSectionsSb strings.Builder
	for _, f := range params.Files {
		if f.Content == "" {
			continue
		}
		fileSectionsSb.WriteString("\n\n" + formatSystemFile(f))
	}
	fileSections += fileSectionsSb.String()

	tmpl := selectSystemTemplate(params.SessionType)

	return render(tmpl, map[string]string{
		"home":                      home,
		"currentTime":               now.Format(time.RFC3339),
		"timezone":                  timezoneName,
		"basicTools":                strings.Join(basicTools, "\n"),
		"displayTools":              displayTools,
		"skillsSection":             skillsSection,
		"platformIdentitiesSection": strings.TrimSpace(params.PlatformIdentitiesSection),
		"fileSections":              fileSections,
	})
}

// SystemPromptParams holds all inputs for system prompt generation.
type SystemPromptParams struct {
	SessionType               string
	Skills                    []SkillEntry
	Files                     []SystemFile
	Now                       time.Time
	Timezone                  string
	SupportsImageInput        bool
	DisplayEnabled            bool
	PlatformIdentitiesSection string
}

func buildDisplayToolsSection(enabled bool) string {
	if !enabled {
		return ""
	}
	return strings.TrimSpace(`
## Workspace browser & desktop

This bot has a headed workspace display (Chrome on a virtual desktop). Use GUI tools only when the task needs on-screen interaction:

- **Browser** (browser_observe, browser_action): Web pages in Chrome. Observe before acting; prefer element refs from snapshot over CSS selectors.
- **Computer** (computer_observe, computer_action): Whole-desktop fallback for native dialogs, non-browser apps, or when the browser path fails. Start with computer_observe snapshot to get an accessibility tree with element refs, then drive computer_action with those refs; raw coordinates are a last-resort fallback.
- **browser_remote_session**: Only when running Playwright or other CDP automation inside the workspace is clearly better than the browser tools above.
- **Screenshots**: Both browser_observe and computer_observe save screenshots to a workspace path; they are not attached to the conversation. Read the returned path with the file read tool when you need the image.
`)
}

// GenerateSchedulePrompt builds the user message for a scheduled task trigger.
func GenerateSchedulePrompt(s Schedule) string {
	maxCallsStr := "Unlimited"
	if s.MaxCalls != nil {
		maxCallsStr = strconv.Itoa(*s.MaxCalls)
	}
	return render(scheduleTmpl, map[string]string{
		"name":        s.Name,
		"description": s.Description,
		"maxCalls":    maxCallsStr,
		"pattern":     s.Pattern,
		"command":     s.Command,
	})
}

// GenerateHeartbeatPrompt builds the user message for a heartbeat trigger.
func GenerateHeartbeatPrompt(interval int, checklist string, now time.Time, lastHeartbeatAt string) string {
	checklistSection := ""
	if strings.TrimSpace(checklist) != "" {
		checklistSection = "\n## HEARTBEAT.md (checklist)\n\n" + strings.TrimSpace(checklist) + "\n"
	}
	lastHB := strings.TrimSpace(lastHeartbeatAt)
	if lastHB == "" {
		lastHB = "never (first heartbeat)"
	}
	return render(heartbeatTmpl, map[string]string{
		"interval":         strconv.Itoa(interval),
		"timeNow":          now.Format(time.RFC3339),
		"lastHeartbeat":    lastHB,
		"checklistSection": checklistSection,
	})
}

func buildSkillsSection(skills []SkillEntry) string {
	if len(skills) == 0 {
		return ""
	}
	sorted := make([]SkillEntry, len(skills))
	copy(sorted, skills)
	slices.SortFunc(sorted, func(a, b SkillEntry) int {
		return strings.Compare(a.Name, b.Name)
	})
	var sb strings.Builder
	sb.WriteString("## Skills\n\n")
	sb.WriteString("Memoh-managed skills are stored in `" + skillset.ManagedDir() + "/`. ")
	sb.WriteString("Compatible external skill directories inside the bot container may also be discovered automatically. ")
	sb.WriteString("Each skill is a `SKILL.md` file inside a named subdirectory.\n\n")
	sb.WriteString("Call `use_skill` with the skill name to load its full instructions before following them. ")
	sb.WriteString("Only activate a skill when it is relevant to the current task.\n\n")
	sb.WriteString(strconv.Itoa(len(sorted)))
	sb.WriteString(" skill(s) available:\n")
	for _, s := range sorted {
		sb.WriteString("- **" + s.Name + "**: " + s.Description + "\n")
	}
	return sb.String()
}

func formatSystemFile(file SystemFile) string {
	return fmt.Sprintf("## %s\n\n%s", file.Filename, file.Content)
}
