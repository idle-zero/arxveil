package agentgrpc

import (
	"context"
	"errors"
	"testing"

	"github.com/idle-zero/arxveil/server/internal/enrollment"
	agentv1 "github.com/idle-zero/arxveil/server/internal/gen/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeEnroller struct {
	input  enrollment.Input
	result enrollment.EnrolledAgent
	err    error
}

func (e *fakeEnroller) Enroll(_ context.Context, input enrollment.Input) (enrollment.EnrolledAgent, error) {
	e.input = input
	return e.result, e.err
}

func TestEnrollMapsRequestAndResponse(t *testing.T) {
	enroller := &fakeEnroller{result: enrollment.EnrolledAgent{
		MachineID: "machine-id",
		AgentID:   "agent-id",
		Secret:    "agent-secret",
	}}
	server := NewServer(enroller)

	response, err := server.Enroll(context.Background(), &agentv1.EnrollRequest{
		Hostname:        "agent-01",
		OperatingSystem: "linux",
		OsVersion:       "6.12",
		Architecture:    "amd64",
		AgentVersion:    "0.1.0",
		Capabilities:    []string{"telemetry"},
	})
	if err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}
	if response.GetMachineId() != "machine-id" || response.GetAgentId() != "agent-id" || response.GetAgentSecret() != "agent-secret" {
		t.Errorf("Enroll() response = %+v", response)
	}
	if enroller.input.Hostname != "agent-01" || enroller.input.OperatingSystem != "linux" {
		t.Errorf("Enroll() input = %+v", enroller.input)
	}
}

func TestEnrollMapsKnownErrorsToGRPCStatus(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{name: "invalid input", err: enrollment.ErrInvalidInput, wantCode: codes.InvalidArgument},
		{name: "unexpected failure", err: errors.New("database unavailable"), wantCode: codes.Internal},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := NewServer(&fakeEnroller{err: test.err})
			_, err := server.Enroll(context.Background(), &agentv1.EnrollRequest{})
			if got := status.Code(err); got != test.wantCode {
				t.Errorf("status code = %s, want %s", got, test.wantCode)
			}
		})
	}
}
