-- name: CreateHistory :one
INSERT INTO history (messages, skills, timestamp, "user")
VALUES ($1, $2, $3, $4)
RETURNING id, messages, skills, timestamp, "user";

-- name: ListHistoryByUserSince :many
SELECT id, messages, skills, timestamp, "user"
FROM history
WHERE "user" = $1 AND timestamp >= $2
ORDER BY timestamp ASC;

-- name: GetHistoryByID :one
SELECT id, messages, skills, timestamp, "user"
FROM history
WHERE id = $1;

-- name: ListHistoryByUser :many
SELECT id, messages, skills, timestamp, "user"
FROM history
WHERE "user" = $1
ORDER BY timestamp DESC
LIMIT $2;

-- name: DeleteHistoryByID :exec
DELETE FROM history
WHERE id = $1;

-- name: DeleteHistoryByUser :exec
DELETE FROM history
WHERE "user" = $1;

