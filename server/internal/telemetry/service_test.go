package telemetry

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

type fakeStore struct {
	sample Sample
	err    error
	calls  int
}

func (s *fakeStore) Record(_ context.Context, sample Sample) error {
	s.calls++
	s.sample = sample
	return s.err
}

func validSample() Sample {
	return Sample{
		MachineID:        "machine-id",
		CollectedAt:      time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC),
		CPUUsagePercent:  42.5,
		MemoryTotalBytes: 16_000,
		MemoryUsedBytes:  8_000,
		DiskTotalBytes:   1_000_000,
		DiskUsedBytes:    250_000,
		UptimeSeconds:    3_600,
	}
}

func TestRecordValidatesAndPersistsSample(t *testing.T) {
	store := &fakeStore{}
	sample := validSample()

	if err := New(store).Record(context.Background(), sample); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if store.calls != 1 || store.sample != sample {
		t.Errorf("Store.Record() calls = %d, sample = %+v", store.calls, store.sample)
	}
}

func TestRecordRejectsInvalidSamples(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Sample)
	}{
		{name: "missing machine ID", mutate: func(sample *Sample) { sample.MachineID = " \t" }},
		{name: "missing collected at", mutate: func(sample *Sample) { sample.CollectedAt = time.Time{} }},
		{name: "negative CPU usage", mutate: func(sample *Sample) { sample.CPUUsagePercent = -0.1 }},
		{name: "CPU usage above 100", mutate: func(sample *Sample) { sample.CPUUsagePercent = 100.1 }},
		{name: "CPU usage is NaN", mutate: func(sample *Sample) { sample.CPUUsagePercent = math.NaN() }},
		{name: "CPU usage is infinite", mutate: func(sample *Sample) { sample.CPUUsagePercent = math.Inf(1) }},
		{name: "negative memory total", mutate: func(sample *Sample) { sample.MemoryTotalBytes = -1 }},
		{name: "negative memory used", mutate: func(sample *Sample) { sample.MemoryUsedBytes = -1 }},
		{name: "memory used exceeds total", mutate: func(sample *Sample) { sample.MemoryUsedBytes = sample.MemoryTotalBytes + 1 }},
		{name: "negative disk total", mutate: func(sample *Sample) { sample.DiskTotalBytes = -1 }},
		{name: "negative disk used", mutate: func(sample *Sample) { sample.DiskUsedBytes = -1 }},
		{name: "disk used exceeds total", mutate: func(sample *Sample) { sample.DiskUsedBytes = sample.DiskTotalBytes + 1 }},
		{name: "negative uptime", mutate: func(sample *Sample) { sample.UptimeSeconds = -1 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{}
			sample := validSample()
			test.mutate(&sample)

			err := New(store).Record(context.Background(), sample)
			if !errors.Is(err, ErrInvalidSample) {
				t.Fatalf("Record() error = %v, want ErrInvalidSample", err)
			}
			if store.calls != 0 {
				t.Errorf("Store.Record() calls = %d, want 0", store.calls)
			}
		})
	}
}

func TestRecordWrapsStoreError(t *testing.T) {
	wantErr := errors.New("database unavailable")
	err := New(&fakeStore{err: wantErr}).Record(context.Background(), validSample())
	if !errors.Is(err, wantErr) {
		t.Errorf("Record() error = %v, want wrapped %v", err, wantErr)
	}
}
