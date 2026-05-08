package orchestrationenv

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// encodeObject turns a map into a JSONB-friendly byte slice. nil maps
// become "{}" so the Postgres column always sees a valid JSON object.
func encodeObject(value map[string]any) []byte {
	if value == nil {
		return []byte("{}")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

// decodeObject is the inverse of encodeObject. Failed decodes become
// the empty map so callers never trip on a nil dereference; the
// alternative (returning the underlying error) would force every
// projection helper to handle a corrupt-row case that should never
// happen in practice.
func decodeObject(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func timeToPg(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func timeFromPg(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

func uuidString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return value.String()
}
