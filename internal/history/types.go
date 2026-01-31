package history

import "time"

type Record struct {
	ID        string                   `json:"id"`
	Messages  []map[string]interface{} `json:"messages"`
	Skills    []string                 `json:"skills"`
	Timestamp time.Time                `json:"timestamp"`
	UserID    string                   `json:"user_id"`
}

type CreateRequest struct {
	Messages []map[string]interface{} `json:"messages"`
	Skills   []string                 `json:"skills,omitempty"`
}

type ListResponse struct {
	Items []Record `json:"items"`
}

