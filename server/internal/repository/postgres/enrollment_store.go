package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/idle-zero/arxveil/server/internal/enrollment"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EnrollmentStore struct {
	pool *pgxpool.Pool
}

func NewEnrollmentStore(pool *pgxpool.Pool) *EnrollmentStore {
	return &EnrollmentStore{pool: pool}
}

func (s *EnrollmentStore) Enroll(ctx context.Context, input enrollment.StoredEnrollment) (machineID, agentID string, err error) {
	transaction, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", "", fmt.Errorf("begin enrollment transaction: %w", err)
	}
	defer transaction.Rollback(ctx)

	// TODO review what to set in the name right now just duplicating info
	err = transaction.QueryRow(ctx, `
		INSERT INTO machines (
			name,
			hostname,
			operating_system,
			os_version,
			architecture,
			agent_version
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, input.Hostname, input.Hostname, input.OperatingSystem, input.OSVersion, input.Architecture, input.AgentVersion).Scan(&machineID)
	if err != nil {
		return "", "", fmt.Errorf("create machine: %w", err)
	}

	capabilities, err := json.Marshal(input.Capabilities)
	if err != nil {
		return "", "", fmt.Errorf("encode capabilities: %w", err)
	}
	err = transaction.QueryRow(ctx, `
		INSERT INTO agent_identities (machine_id, agent_id, credential_hash, capabilities)
		VALUES ($1, gen_random_uuid(), $2, $3)
		RETURNING agent_id
	`, machineID, input.CredentialHash, capabilities).Scan(&agentID)
	if err != nil {
		return "", "", fmt.Errorf("create agent identity: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return "", "", fmt.Errorf("commit enrollment transaction: %w", err)
	}

	return machineID, agentID, nil
}
