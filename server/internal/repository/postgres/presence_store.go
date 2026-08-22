package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/idle-zero/arxveil/server/internal/presence"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PresenceStore struct {
	pool *pgxpool.Pool
}

func NewPresenceStore(pool *pgxpool.Pool) *PresenceStore {
	return &PresenceStore{pool: pool}
}

func (ps *PresenceStore) FindActiveIdentity(ctx context.Context, agentID string) (presence.Identity, bool, error) {
	var identity presence.Identity
	err := ps.pool.QueryRow(ctx, `
		SELECT machine_id, agent_id, credential_hash
		FROM agent_identities
		WHERE agent_id = $1 AND revoked_at IS NULL
	`, agentID).Scan(&identity.MachineID, &identity.AgentID, &identity.CredentialHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return presence.Identity{}, false, nil
	}
	if err != nil {
		return presence.Identity{}, false, fmt.Errorf("query active agent identity: %w", err)
	}

	return identity, true, nil
}

func (ps *PresenceStore) StartSession(ctx context.Context, machineID string) (time.Time, error) {
	transaction, err := ps.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return time.Time{}, fmt.Errorf("begin presence session transaction: %w", err)
	}
	defer transaction.Rollback(ctx)

	var (
		connectedAt    time.Time
		previousStatus string
	)
	err = transaction.QueryRow(ctx, `
		WITH current_machine AS (
			SELECT id, status
			FROM machines
			WHERE id = $1
			FOR UPDATE
		), updated_machine AS (
			UPDATE machines AS machine
			SET
				status = 'online',
				last_connected_at = clock_timestamp(),
				last_seen_at = clock_timestamp(),
				updated_at = clock_timestamp()
			FROM current_machine
			WHERE machine.id = current_machine.id
			RETURNING machine.last_connected_at, current_machine.status
		)
		SELECT last_connected_at, status
		FROM updated_machine
	`, machineID).Scan(&connectedAt, &previousStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, fmt.Errorf("start presence session: machine not found")
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("mark machine online: %w", err)
	}

	if previousStatus != "online" {
		_, err = transaction.Exec(ctx, `
			INSERT INTO machine_presence_events (machine_id, status)
			VALUES ($1, 'online')
		`, machineID)
		if err != nil {
			return time.Time{}, fmt.Errorf("record machine online event: %w", err)
		}
	}

	if err := transaction.Commit(ctx); err != nil {
		return time.Time{}, fmt.Errorf("commit presence session transaction: %w", err)
	}

	return connectedAt, nil
}

func (ps *PresenceStore) TouchSession(ctx context.Context, session presence.Session) error {
	_, err := ps.pool.Exec(ctx, `
		UPDATE machines
		SET
			last_seen_at = clock_timestamp(),
			updated_at = clock_timestamp()
		WHERE id = $1 AND last_connected_at = $2
	`, session.MachineID, session.ConnectedAt)
	if err != nil {
		return fmt.Errorf("update machine heartbeat: %w", err)
	}

	return nil
}

func (ps *PresenceStore) EndSession(ctx context.Context, session presence.Session) error {
	transaction, err := ps.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin end presence session transaction: %w", err)
	}
	defer transaction.Rollback(ctx)

	var machineID string
	err = transaction.QueryRow(ctx, `
		UPDATE machines
		SET
			status = 'offline',
			last_disconnected_at = clock_timestamp(),
			updated_at = clock_timestamp()
		WHERE id = $1
			AND last_connected_at = $2
			AND status = 'online'
		RETURNING id
	`, session.MachineID, session.ConnectedAt).Scan(&machineID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := transaction.Commit(ctx); err != nil {
			return fmt.Errorf("commit unchanged presence session: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("mark machine offline: %w", err)
	}

	_, err = transaction.Exec(ctx, `
		INSERT INTO machine_presence_events (machine_id, status)
		VALUES ($1, 'offline')
	`, machineID)
	if err != nil {
		return fmt.Errorf("record machine offline event: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit end presence session transaction: %w", err)
	}

	return nil
}
