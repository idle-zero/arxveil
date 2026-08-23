package agentgrpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/idle-zero/arxveil/server/internal/enrollment"
	agentv1 "github.com/idle-zero/arxveil/server/internal/gen/agent/v1"
	"github.com/idle-zero/arxveil/server/internal/presence"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
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

type fakePresenceTracker struct {
	mu                sync.Mutex
	session           presence.Session
	openErr           error
	heartbeatErr      error
	closeErr          error
	credentials       []presence.Credentials
	heartbeatSessions []presence.Session
	closedSessions    []presence.Session
}

func (p *fakePresenceTracker) OpenSession(_ context.Context, credentials presence.Credentials) (presence.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.credentials = append(p.credentials, credentials)
	return p.session, p.openErr
}

func (p *fakePresenceTracker) RecordHeartbeat(_ context.Context, session presence.Session) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.heartbeatSessions = append(p.heartbeatSessions, session)
	return p.heartbeatErr
}

func (p *fakePresenceTracker) CloseSession(_ context.Context, session presence.Session) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closedSessions = append(p.closedSessions, session)
	return p.closeErr
}

type presenceTrackerCalls struct {
	credentials       []presence.Credentials
	heartbeatSessions []presence.Session
	closedSessions    []presence.Session
}

func (p *fakePresenceTracker) calls() presenceTrackerCalls {
	p.mu.Lock()
	defer p.mu.Unlock()

	return presenceTrackerCalls{
		credentials:       append([]presence.Credentials(nil), p.credentials...),
		heartbeatSessions: append([]presence.Session(nil), p.heartbeatSessions...),
		closedSessions:    append([]presence.Session(nil), p.closedSessions...),
	}
}

func newTestAgentServiceClient(t *testing.T, tracker *fakePresenceTracker) agentv1.AgentServiceClient {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(grpcServer, NewServer(&fakeEnroller{}, tracker, discardLogger()))
	go func() {
		_ = grpcServer.Serve(listener)
	}()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})

	connection, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("DialContext() error = %v", err)
	}
	t.Cleanup(func() {
		_ = connection.Close()
	})

	return agentv1.NewAgentServiceClient(connection)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func authenticateMessage(agentID, secret string) *agentv1.AgentMessage {
	return &agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Authenticate{
			Authenticate: &agentv1.Authenticate{
				AgentId:     agentID,
				AgentSecret: secret,
			},
		},
	}
}

func heartbeatMessage() *agentv1.AgentMessage {
	return &agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Heartbeat{
			Heartbeat: &agentv1.Heartbeat{},
		},
	}
}

func TestEnrollMapsRequestAndResponse(t *testing.T) {
	enroller := &fakeEnroller{result: enrollment.EnrolledAgent{
		MachineID: "machine-id",
		AgentID:   "agent-id",
		Secret:    "agent-secret",
	}}
	server := NewServer(enroller, &fakePresenceTracker{}, discardLogger())

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
			server := NewServer(&fakeEnroller{err: test.err}, &fakePresenceTracker{}, discardLogger())
			_, err := server.Enroll(context.Background(), &agentv1.EnrollRequest{})
			if got := status.Code(err); got != test.wantCode {
				t.Errorf("status code = %s, want %s", got, test.wantCode)
			}
		})
	}
}

func TestStreamUpdatesAuthenticatesRecordsHeartbeatAndClosesSession(t *testing.T) {
	session := presence.Session{
		MachineID:   "machine-id",
		AgentID:     "agent-id",
		ConnectedAt: time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC),
	}
	tracker := &fakePresenceTracker{session: session}
	client := newTestAgentServiceClient(t, tracker)

	stream, err := client.StreamUpdates(context.Background())
	if err != nil {
		t.Fatalf("StreamUpdates() error = %v", err)
	}
	if err := stream.Send(authenticateMessage("agent-id", "agent-secret")); err != nil {
		t.Fatalf("Send(authenticate) error = %v", err)
	}
	if err := stream.Send(heartbeatMessage()); err != nil {
		t.Fatalf("Send(heartbeat) error = %v", err)
	}
	if _, err := stream.CloseAndRecv(); err != nil {
		t.Fatalf("CloseAndRecv() error = %v", err)
	}

	calls := tracker.calls()
	if len(calls.credentials) != 1 || calls.credentials[0] != (presence.Credentials{AgentID: "agent-id", Secret: "agent-secret"}) {
		t.Errorf("OpenSession() credentials = %+v", calls.credentials)
	}
	if len(calls.heartbeatSessions) != 1 || calls.heartbeatSessions[0] != session {
		t.Errorf("RecordHeartbeat() sessions = %+v", calls.heartbeatSessions)
	}
	if len(calls.closedSessions) != 1 || calls.closedSessions[0] != session {
		t.Errorf("CloseSession() sessions = %+v", calls.closedSessions)
	}
}

func TestStreamUpdatesRejectsInvalidFirstMessage(t *testing.T) {
	tests := []struct {
		name    string
		message *agentv1.AgentMessage
	}{
		{name: "heartbeat", message: heartbeatMessage()},
		{
			name: "telemetry",
			message: &agentv1.AgentMessage{
				Payload: &agentv1.AgentMessage_Telemetry{Telemetry: &agentv1.Telemetry{}},
			},
		},
		{name: "empty payload", message: &agentv1.AgentMessage{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tracker := &fakePresenceTracker{}
			client := newTestAgentServiceClient(t, tracker)
			stream, err := client.StreamUpdates(context.Background())
			if err != nil {
				t.Fatalf("StreamUpdates() error = %v", err)
			}
			if err := stream.Send(test.message); err != nil {
				t.Fatalf("Send() error = %v", err)
			}
			_, err = stream.CloseAndRecv()
			if got := status.Code(err); got != codes.InvalidArgument {
				t.Errorf("CloseAndRecv() status = %s, want %s", got, codes.InvalidArgument)
			}
			if calls := tracker.calls(); len(calls.credentials) != 0 || len(calls.closedSessions) != 0 {
				t.Errorf("presence calls = %+v, want none", calls)
			}
		})
	}
}

func TestStreamUpdatesRejectsEmptyStream(t *testing.T) {
	tracker := &fakePresenceTracker{}
	client := newTestAgentServiceClient(t, tracker)
	stream, err := client.StreamUpdates(context.Background())
	if err != nil {
		t.Fatalf("StreamUpdates() error = %v", err)
	}

	_, err = stream.CloseAndRecv()
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Errorf("CloseAndRecv() status = %s, want %s", got, codes.InvalidArgument)
	}
	if calls := tracker.calls(); len(calls.credentials) != 0 || len(calls.closedSessions) != 0 {
		t.Errorf("presence calls = %+v, want none", calls)
	}
}

func TestStreamUpdatesMapsAuthenticationError(t *testing.T) {
	tests := []struct {
		name     string
		openErr  error
		wantCode codes.Code
	}{
		{name: "unauthenticated", openErr: presence.ErrUnauthenticated, wantCode: codes.Unauthenticated},
		{name: "unexpected failure", openErr: errors.New("database unavailable"), wantCode: codes.Internal},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tracker := &fakePresenceTracker{openErr: test.openErr}
			client := newTestAgentServiceClient(t, tracker)
			stream, err := client.StreamUpdates(context.Background())
			if err != nil {
				t.Fatalf("StreamUpdates() error = %v", err)
			}
			if err := stream.Send(authenticateMessage("agent-id", "agent-secret")); err != nil {
				t.Fatalf("Send(authenticate) error = %v", err)
			}
			_, err = stream.CloseAndRecv()
			if got := status.Code(err); got != test.wantCode {
				t.Errorf("CloseAndRecv() status = %s, want %s", got, test.wantCode)
			}
			if calls := tracker.calls(); len(calls.closedSessions) != 0 {
				t.Errorf("CloseSession() calls = %+v, want none", calls.closedSessions)
			}
		})
	}
}

func TestStreamUpdatesRejectsSecondAuthenticationAndClosesSession(t *testing.T) {
	session := presence.Session{MachineID: "machine-id", AgentID: "agent-id", ConnectedAt: time.Now()}
	tracker := &fakePresenceTracker{session: session}
	client := newTestAgentServiceClient(t, tracker)
	stream, err := client.StreamUpdates(context.Background())
	if err != nil {
		t.Fatalf("StreamUpdates() error = %v", err)
	}
	if err := stream.Send(authenticateMessage("agent-id", "agent-secret")); err != nil {
		t.Fatalf("Send(first authenticate) error = %v", err)
	}
	if err := stream.Send(authenticateMessage("agent-id", "agent-secret")); err != nil {
		t.Fatalf("Send(second authenticate) error = %v", err)
	}
	_, err = stream.CloseAndRecv()
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Errorf("CloseAndRecv() status = %s, want %s", got, codes.InvalidArgument)
	}
	if calls := tracker.calls(); len(calls.closedSessions) != 1 || calls.closedSessions[0] != session {
		t.Errorf("CloseSession() sessions = %+v", calls.closedSessions)
	}
}

func TestStreamUpdatesRejectsTelemetryAndClosesSession(t *testing.T) {
	session := presence.Session{MachineID: "machine-id", AgentID: "agent-id", ConnectedAt: time.Now()}
	tracker := &fakePresenceTracker{session: session}
	client := newTestAgentServiceClient(t, tracker)
	stream, err := client.StreamUpdates(context.Background())
	if err != nil {
		t.Fatalf("StreamUpdates() error = %v", err)
	}
	if err := stream.Send(authenticateMessage("agent-id", "agent-secret")); err != nil {
		t.Fatalf("Send(authenticate) error = %v", err)
	}
	if err := stream.Send(&agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Telemetry{Telemetry: &agentv1.Telemetry{}},
	}); err != nil {
		t.Fatalf("Send(telemetry) error = %v", err)
	}
	_, err = stream.CloseAndRecv()
	if got := status.Code(err); got != codes.Unimplemented {
		t.Errorf("CloseAndRecv() status = %s, want %s", got, codes.Unimplemented)
	}
	if calls := tracker.calls(); len(calls.closedSessions) != 1 || calls.closedSessions[0] != session {
		t.Errorf("CloseSession() sessions = %+v", calls.closedSessions)
	}
}

func TestStreamUpdatesMapsHeartbeatAndCloseErrors(t *testing.T) {
	tests := []struct {
		name        string
		tracker     *fakePresenceTracker
		sendMessage *agentv1.AgentMessage
	}{
		{
			name: "heartbeat",
			tracker: &fakePresenceTracker{
				session:      presence.Session{MachineID: "machine-id", AgentID: "agent-id", ConnectedAt: time.Now()},
				heartbeatErr: errors.New("database unavailable"),
			},
			sendMessage: heartbeatMessage(),
		},
		{
			name: "close",
			tracker: &fakePresenceTracker{
				session:  presence.Session{MachineID: "machine-id", AgentID: "agent-id", ConnectedAt: time.Now()},
				closeErr: errors.New("database unavailable"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newTestAgentServiceClient(t, test.tracker)
			stream, err := client.StreamUpdates(context.Background())
			if err != nil {
				t.Fatalf("StreamUpdates() error = %v", err)
			}
			if err := stream.Send(authenticateMessage("agent-id", "agent-secret")); err != nil {
				t.Fatalf("Send(authenticate) error = %v", err)
			}
			if test.sendMessage != nil {
				if err := stream.Send(test.sendMessage); err != nil {
					t.Fatalf("Send() error = %v", err)
				}
			}
			_, err = stream.CloseAndRecv()
			if got := status.Code(err); got != codes.Internal {
				t.Errorf("CloseAndRecv() status = %s, want %s", got, codes.Internal)
			}
			if calls := test.tracker.calls(); len(calls.closedSessions) != 1 {
				t.Errorf("CloseSession() calls = %+v, want one", calls.closedSessions)
			}
		})
	}
}
