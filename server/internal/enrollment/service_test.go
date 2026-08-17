package enrollment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
)

type fakeStore struct {
	enrollInput     StoredEnrollment
	enrollMachineID string
	enrollAgentID   string
	enrollErr       error
}

func (s *fakeStore) Enroll(_ context.Context, input StoredEnrollment) (string, string, error) {
	s.enrollInput = input
	return s.enrollMachineID, s.enrollAgentID, s.enrollErr
}

func TestEnrollHashesCredentialAndNormalizesMetadata(t *testing.T) {
	store := &fakeStore{enrollMachineID: "machine-id", enrollAgentID: "agent-id"}
	service := newService(store, bytes.NewReader(bytes.Repeat([]byte{7}, credentialBytes)))

	result, err := service.Enroll(context.Background(), Input{
		Hostname:        " agent-01 ",
		OperatingSystem: " linux ",
		OSVersion:       " 6.12 ",
		Architecture:    " amd64 ",
		AgentVersion:    " 0.1.0 ",
		Capabilities:    []string{" telemetry ", "telemetry"},
	})
	if err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}

	if result.MachineID != "machine-id" || result.AgentID != "agent-id" || result.Secret == "" {
		t.Fatalf("Enroll() result = %+v", result)
	}
	wantCredentialHash := sha256.Sum256([]byte(result.Secret))
	if !bytes.Equal(store.enrollInput.CredentialHash, wantCredentialHash[:]) {
		t.Error("Enroll() stored an unexpected credential hash")
	}
	if store.enrollInput.Hostname != "agent-01" || store.enrollInput.OperatingSystem != "linux" {
		t.Errorf("Enroll() did not normalize metadata: %+v", store.enrollInput)
	}
	if len(store.enrollInput.Capabilities) != 1 || store.enrollInput.Capabilities[0] != "telemetry" {
		t.Errorf("Enroll() capabilities = %#v, want [telemetry]", store.enrollInput.Capabilities)
	}
}

func TestEnrollRejectsInvalidInput(t *testing.T) {
	service := New(&fakeStore{})
	_, err := service.Enroll(context.Background(), Input{})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Enroll() error = %v, want ErrInvalidInput", err)
	}
}

func validInput() Input {
	return Input{
		Hostname:        "agent-01",
		OperatingSystem: "linux",
		OSVersion:       "6.12",
		Architecture:    "amd64",
		AgentVersion:    "0.1.0",
	}
}
