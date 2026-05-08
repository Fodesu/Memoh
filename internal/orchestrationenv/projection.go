package orchestrationenv

import "github.com/memohai/memoh/internal/db/postgres/sqlc"

// projectResource turns the sqlc row into the package's Resource
// projection. Keeping projection helpers in one place means every
// caller sees the same shape regardless of which sqlc method
// produced the row.
func projectResource(row sqlc.OrchestrationEnvResource) Resource {
	return Resource{
		ID:           uuidString(row.ID),
		TenantID:     row.TenantID,
		OwnerSubject: row.OwnerSubject,
		Kind:         row.Kind,
		Name:         row.Name,
		Config:       decodeObject(row.Config),
		Capacity:     int(row.Capacity),
		Status:       row.Status,
		Metadata:     decodeObject(row.Metadata),
		CreatedAt:    timeFromPg(row.CreatedAt),
		UpdatedAt:    timeFromPg(row.UpdatedAt),
	}
}

func projectSession(row sqlc.OrchestrationEnvSession) Session {
	return Session{
		ID:              uuidString(row.ID),
		TenantID:        row.TenantID,
		ResourceID:      uuidString(row.ResourceID),
		Status:          row.Status,
		LeaseHolderKind: row.LeaseHolderKind,
		LeaseHolderID:   row.LeaseHolderID,
		LeaseToken:      row.LeaseToken,
		LeaseEpoch:      row.LeaseEpoch,
		LeaseAcquiredAt: timeFromPg(row.LeaseAcquiredAt),
		LeaseExpiresAt:  optionalTime(row.LeaseExpiresAt),
		RunID:           uuidString(row.RunID),
		TaskID:          uuidString(row.TaskID),
		AttemptID:       uuidString(row.AttemptID),
		RuntimeHandle:   decodeObject(row.RuntimeHandle),
		Metadata:        decodeObject(row.Metadata),
		ReleasedAt:      optionalTime(row.ReleasedAt),
		CreatedAt:       timeFromPg(row.CreatedAt),
		UpdatedAt:       timeFromPg(row.UpdatedAt),
	}
}

func projectReservation(row sqlc.OrchestrationEnvLeaseReservation) Reservation {
	return Reservation{
		ID:                 uuidString(row.ID),
		TenantID:           row.TenantID,
		ResourceID:         uuidString(row.ResourceID),
		RequesterKind:      row.RequesterKind,
		RequesterID:        row.RequesterID,
		RunID:              uuidString(row.RunID),
		TaskID:             uuidString(row.TaskID),
		AttemptID:          uuidString(row.AttemptID),
		Priority:           int(row.Priority),
		Status:             row.Status,
		CommittedSessionID: uuidString(row.CommittedSessionID),
		RequestedAt:        timeFromPg(row.RequestedAt),
		ExpiresAt:          optionalTime(row.ExpiresAt),
		CommittedAt:        optionalTime(row.CommittedAt),
		AbortedAt:          optionalTime(row.AbortedAt),
		Metadata:           decodeObject(row.Metadata),
		CreatedAt:          timeFromPg(row.CreatedAt),
		UpdatedAt:          timeFromPg(row.UpdatedAt),
	}
}

func projectBinding(row sqlc.OrchestrationEnvBinding) Binding {
	return Binding{
		ID:                  uuidString(row.ID),
		TenantID:            row.TenantID,
		RunID:               uuidString(row.RunID),
		TaskID:              uuidString(row.TaskID),
		AttemptID:           uuidString(row.AttemptID),
		SessionID:           uuidString(row.SessionID),
		Purpose:             row.Purpose,
		Status:              row.Status,
		HeldForCheckpointID: uuidString(row.HeldForCheckpointID),
		Metadata:            decodeObject(row.Metadata),
		ReleasedAt:          optionalTime(row.ReleasedAt),
		CreatedAt:           timeFromPg(row.CreatedAt),
		UpdatedAt:           timeFromPg(row.UpdatedAt),
	}
}

func projectSnapshot(row sqlc.OrchestrationEnvSnapshot) Snapshot {
	return Snapshot{
		ID:          uuidString(row.ID),
		TenantID:    row.TenantID,
		SessionID:   uuidString(row.SessionID),
		RunID:       uuidString(row.RunID),
		TaskID:      uuidString(row.TaskID),
		AttemptID:   uuidString(row.AttemptID),
		Kind:        row.Kind,
		EffectClass: row.EffectClass,
		RuntimeRef:  decodeObject(row.RuntimeRef),
		Digest:      row.Digest,
		SizeBytes:   row.SizeBytes,
		Metadata:    decodeObject(row.Metadata),
		CreatedAt:   timeFromPg(row.CreatedAt),
	}
}
