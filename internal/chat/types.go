package chat

import "encoding/json"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type GatewayMessage map[string]interface{}

type AgentSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

type ChatRequest struct {
	UserID             string           `json:"-"`
	Token              string           `json:"-"`
	Query              string           `json:"query"`
	Model              string           `json:"model,omitempty"`
	Provider           string           `json:"provider,omitempty"`
	MaxContextLoadTime int              `json:"max_context_load_time,omitempty"`
	Locale             string           `json:"locale,omitempty"`
	Language           string           `json:"language,omitempty"`
	MaxSteps           int              `json:"max_steps,omitempty"`
	Platforms          []string         `json:"platforms,omitempty"`
	CurrentPlatform    string           `json:"current_platform,omitempty"`
	Messages           []GatewayMessage `json:"messages,omitempty"`
	Skills             []AgentSkill     `json:"skills,omitempty"`
	UseSkills          []string         `json:"use_skills,omitempty"`
}

type ChatResponse struct {
	Messages []GatewayMessage `json:"messages"`
	Skills   []string         `json:"skills,omitempty"`
	Model    string           `json:"model,omitempty"`
	Provider string           `json:"provider,omitempty"`
}

type StreamChunk = json.RawMessage

type SchedulePayload struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Pattern     string `json:"pattern"`
	MaxCalls    *int   `json:"maxCalls,omitempty"`
	Command     string `json:"command"`
}
