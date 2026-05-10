package orchestrationexec

import "strings"

type orchestrationPromptSection struct {
	name  string
	value any
}

func buildOrchestrationUserPrompt(kind string, sections ...orchestrationPromptSection) string {
	var sb strings.Builder
	sb.WriteString(`<orchestration-context kind="`)
	sb.WriteString(kind)
	sb.WriteString(`">`)
	for _, section := range sections {
		if strings.TrimSpace(section.name) == "" {
			continue
		}
		sb.WriteString("\n<")
		sb.WriteString(section.name)
		sb.WriteString(">\n")
		sb.WriteString(mustJSON(section.value))
		sb.WriteString("\n</")
		sb.WriteString(section.name)
		sb.WriteString(">")
	}
	sb.WriteString("\n</orchestration-context>")
	return sb.String()
}
