package postgres

import (
	"context"
	"fmt"

	"github.com/idle-zero/arxveil/server/internal/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TelemetryStore struct {
	pool *pgxpool.Pool
}

func NewTelemetryStore(pool *pgxpool.Pool) *TelemetryStore {
	return &TelemetryStore{pool: pool}
}

func (s *TelemetryStore) Record(ctx context.Context, sample telemetry.Sample) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO telemetry_samples (
			machine_id,
			collected_at,
			cpu_usage_percent,
			memory_total_bytes,
			memory_used_bytes,
			disk_total_bytes,
			disk_used_bytes,
			uptime_seconds
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (machine_id, collected_at) DO UPDATE
		SET
			received_at = clock_timestamp(),
			cpu_usage_percent = EXCLUDED.cpu_usage_percent,
			memory_total_bytes = EXCLUDED.memory_total_bytes,
			memory_used_bytes = EXCLUDED.memory_used_bytes,
			disk_total_bytes = EXCLUDED.disk_total_bytes,
			disk_used_bytes = EXCLUDED.disk_used_bytes,
			uptime_seconds = EXCLUDED.uptime_seconds
	`,
		sample.MachineID,
		sample.CollectedAt,
		sample.CPUUsagePercent,
		sample.MemoryTotalBytes,
		sample.MemoryUsedBytes,
		sample.DiskTotalBytes,
		sample.DiskUsedBytes,
		sample.UptimeSeconds,
	)
	if err != nil {
		return fmt.Errorf("upsert telemetry sample: %w", err)
	}

	return nil
}
