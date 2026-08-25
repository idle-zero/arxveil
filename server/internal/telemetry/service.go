package telemetry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type Sample struct {
	MachineID        string
	CollectedAt      time.Time
	CPUUsagePercent  float64
	MemoryTotalBytes int64
	MemoryUsedBytes  int64
	DiskTotalBytes   int64
	DiskUsedBytes    int64
	UptimeSeconds    int64
}

type Store interface {
	Record(context.Context, Sample) error
}

var ErrInvalidSample = errors.New("invalid telemetry sample")

type Service struct {
	store Store
}

func New(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Record(ctx context.Context, sample Sample) error {
	if err := validateSample(sample); err != nil {
		return err
	}

	if err := s.store.Record(ctx, sample); err != nil {
		return fmt.Errorf("record telemetry sample: %w", err)
	}

	return nil
}

func validateSample(sample Sample) error {
	if strings.TrimSpace(sample.MachineID) == "" {
		return fmt.Errorf("%w: machine ID is required", ErrInvalidSample)
	}
	if sample.CollectedAt.IsZero() {
		return fmt.Errorf("%w: collected at is required", ErrInvalidSample)
	}
	if math.IsNaN(sample.CPUUsagePercent) || math.IsInf(sample.CPUUsagePercent, 0) || sample.CPUUsagePercent < 0 || sample.CPUUsagePercent > 100 {
		return fmt.Errorf("%w: CPU usage percent must be between zero and 100", ErrInvalidSample)
	}
	if sample.MemoryTotalBytes < 0 {
		return fmt.Errorf("%w: memory total bytes must not be negative", ErrInvalidSample)
	}
	if sample.MemoryUsedBytes < 0 || sample.MemoryUsedBytes > sample.MemoryTotalBytes {
		return fmt.Errorf("%w: memory used bytes must be between zero and memory total bytes", ErrInvalidSample)
	}
	if sample.DiskTotalBytes < 0 {
		return fmt.Errorf("%w: disk total bytes must not be negative", ErrInvalidSample)
	}
	if sample.DiskUsedBytes < 0 || sample.DiskUsedBytes > sample.DiskTotalBytes {
		return fmt.Errorf("%w: disk used bytes must be between zero and disk total bytes", ErrInvalidSample)
	}
	if sample.UptimeSeconds < 0 {
		return fmt.Errorf("%w: uptime seconds must not be negative", ErrInvalidSample)
	}

	return nil
}
