package settings

const (
	DefaultMaxContextLoadTime = 24 * 60
	DefaultLanguage           = "auto"
)

type Settings struct {
	ChatModelID        string `json:"chat_model_id" validate:"required"`
	MemoryModelID      string `json:"memory_model_id" validate:"required"`
	EmbeddingModelID   string `json:"embedding_model_id" validate:"required"`
	MaxContextLoadTime int    `json:"max_context_load_time" validate:"required"`
	Language           string `json:"language" validate:"required"`
	AllowGuest         bool   `json:"allow_guest" validate:"required"`
}

type UpsertRequest struct {
	ChatModelID        string `json:"chat_model_id,omitempty"`
	MemoryModelID      string `json:"memory_model_id,omitempty"`
	EmbeddingModelID   string `json:"embedding_model_id,omitempty"`
	MaxContextLoadTime *int   `json:"max_context_load_time,omitempty"`
	Language           string `json:"language,omitempty"`
	AllowGuest         *bool  `json:"allow_guest,omitempty"`
}
