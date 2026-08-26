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
	"github.com/idle-zero/arxveil/server/internal/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	mu               sync.Mutex
	session          presence.Session
	openErr          error
	activityErr      error
	closeErr         error
	credentials      []presence.Credentials
	activitySessions []presence.Session
	closedSessions   []presence.Session
}

func (p *fakePresenceTracker) OpenSession(_ context.Context, credentials presence.Credentials) (presence.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.credentials = append(p.credentials, credentials)
	return p.session, p.openErr
}

func (p *fakePresenceTracker) RecordActivity(_ context.Context, session presence.Session) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.activitySessions = append(p.activitySessions, session)
	return p.activityErr
}

func (p *fakePresenceTracker) CloseSession(_ context.Context, session presence.Session) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closedSessions = append(p.closedSessions, session)
	return p.closeErr
}

type presenceTrackerCalls struct {
	credentials      []presence.Credentials
	activitySessions []presence.Session
	closedSessions   []presence.Session
}

func (p *fakePresenceTracker) calls() presenceTrackerCalls {
	p.mu.Lock()
	defer p.mu.Unlock()

	return presenceTrackerCalls{
		credentials:      append([]presence.Credentials(nil), p.credentials...),
		activitySessions: append([]presence.Session(nil), p.activitySessions...),
		closedSessions:   append([]presence.Session(nil), p.closedSessions...),
	}
}

type fakeTelemetryRecorder struct {
	mu      sync.Mutex
	samples []telemetry.Sample
	err     error
}

func (r *fakeTelemetryRecorder) Record(_ context.Context, sample telemetry.Sample) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.samples = append(r.samples, sample)
	return r.err
}

func (r *fakeTelemetryRecorder) recordedSamples() []telemetry.Sample {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]telemetry.Sample(nil), r.samples...)
}

func newTestAgentServiceClient(t *testing.T, tracker *fakePresenceTracker, telemetryRecorders ...*fakeTelemetryRecorder) agentv1.AgentServiceClient {
	t.Helper()

	telemetryRecorder := &fakeTelemetryRecorder{}
	if len(telemetryRecorders) > 0 {
		telemetryRecorder = telemetryRecorders[0]
	}

	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(grpcServer, NewServer(&fakeEnroller{}, tracker, telemetryRecorder, discardLogger()))
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

func telemetryMessage(collectedAt time.Time) *agentv1.AgentMessage {
	return &agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Telemetry{
			Telemetry: &agentv1.Telemetry{
				CollectedAt:      timestamppb.New(collectedAt),
				CpuUsagePercent:  42.5,
				MemoryTotalBytes: 16_000,
				MemoryUsedBytes:  8_000,
				DiskTotalBytes:   1_000_000,
				DiskUsedBytes:    250_000,
				UptimeSeconds:    3_600,
			},
		},
	}
}

func TestEnrollMapsRequestAndResponse(t *testing.T) {
	enroller := &fakeEnroller{result: enrollment.EnrolledAgent{
		MachineID: "machine-id",
		AgentID:   "agent-id",
		Secret:    "agent-secret",
	}}
	server := NewServer(enroller, &fakePresenceTracker{}, &fakeTelemetryRecorder{}, discardLogger())

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
			server := NewServer(&fakeEnroller{err: test.err}, &fakePresenceTracker{}, &fakeTelemetryRecorder{}, discardLogger())
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
	if len(calls.activitySessions) != 1 || calls.activitySessions[0] != session {
		t.Errorf("RecordActivity() sessions = %+v", calls.activitySessions)
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

func TestStreamUpdatesRecordsTelemetryAndActivity(t *testing.T) {
	session := presence.Session{MachineID: "machine-id", AgentID: "agent-id", ConnectedAt: time.Now()}
	tracker := &fakePresenceTracker{session: session}
	recorder := &fakeTelemetryRecorder{}
	client := newTestAgentServiceClient(t, tracker, recorder)
	stream, err := client.StreamUpdates(context.Background())
	if err != nil {
		t.Fatalf("StreamUpdates() error = %v", err)
	}
	if err := stream.Send(authenticateMessage("agent-id", "agent-secret")); err != nil {
		t.Fatalf("Send(authenticate) error = %v", err)
	}
	collectedAt := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	if err := stream.Send(telemetryMessage(collectedAt)); err != nil {
		t.Fatalf("Send(telemetry) error = %v", err)
	}
	if _, err := stream.CloseAndRecv(); err != nil {
		t.Fatalf("CloseAndRecv() error = %v", err)
	}

	samples := recorder.recordedSamples()
	if len(samples) != 1 {
		t.Fatalf("TelemetryRecorder.Record() calls = %d, want 1", len(samples))
	}
	sample := samples[0]
	if sample.MachineID != session.MachineID || !sample.CollectedAt.Equal(collectedAt) || sample.CPUUsagePercent != 42.5 || sample.MemoryTotalBytes != 16_000 || sample.MemoryUsedBytes != 8_000 || sample.DiskTotalBytes != 1_000_000 || sample.DiskUsedBytes != 250_000 || sample.UptimeSeconds != 3_600 {
		t.Errorf("TelemetryRecorder.Record() sample = %+v", sample)
	}
	if calls := tracker.calls(); len(calls.activitySessions) != 1 || calls.activitySessions[0] != session {
		t.Errorf("RecordActivity() sessions = %+v", calls.activitySessions)
	}
	if calls := tracker.calls(); len(calls.closedSessions) != 1 || calls.closedSessions[0] != session {
		t.Errorf("CloseSession() sessions = %+v", calls.closedSessions)
	}
}

func TestStreamUpdatesMapsTelemetryErrors(t *testing.T) {
	tests := []struct {
		name        string
		message     *agentv1.AgentMessage
		recorderErr error
		wantCode    codes.Code
		wantSamples int
	}{
		{
			name:        "missing collection time",
			message:     &agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Telemetry{Telemetry: &agentv1.Telemetry{}}},
			wantCode:    codes.InvalidArgument,
			wantSamples: 0,
		},
		{
			name:        "invalid sample",
			message:     telemetryMessage(time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)),
			recorderErr: telemetry.ErrInvalidSample,
			wantCode:    codes.InvalidArgument,
			wantSamples: 1,
		},
		{
			name:        "storage failure",
			message:     telemetryMessage(time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)),
			recorderErr: errors.New("database unavailable"),
			wantCode:    codes.Internal,
			wantSamples: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := presence.Session{MachineID: "machine-id", AgentID: "agent-id", ConnectedAt: time.Now()}
			tracker := &fakePresenceTracker{session: session}
			recorder := &fakeTelemetryRecorder{err: test.recorderErr}
			client := newTestAgentServiceClient(t, tracker, recorder)
			stream, err := client.StreamUpdates(context.Background())
			if err != nil {
				t.Fatalf("StreamUpdates() error = %v", err)
			}
			if err := stream.Send(authenticateMessage("agent-id", "agent-secret")); err != nil {
				t.Fatalf("Send(authenticate) error = %v", err)
			}
			if err := stream.Send(test.message); err != nil {
				t.Fatalf("Send(telemetry) error = %v", err)
			}
			_, err = stream.CloseAndRecv()
			if got := status.Code(err); got != test.wantCode {
				t.Errorf("CloseAndRecv() status = %s, want %s", got, test.wantCode)
			}
			if samples := recorder.recordedSamples(); len(samples) != test.wantSamples {
				t.Errorf("TelemetryRecorder.Record() calls = %d, want %d", len(samples), test.wantSamples)
			}
			if calls := tracker.calls(); len(calls.activitySessions) != 0 {
				t.Errorf("RecordActivity() sessions = %+v, want none", calls.activitySessions)
			}
			if calls := tracker.calls(); len(calls.closedSessions) != 1 || calls.closedSessions[0] != session {
				t.Errorf("CloseSession() sessions = %+v", calls.closedSessions)
			}
		})
	}
}

func TestStreamUpdatesMapsActivityAndCloseErrors(t *testing.T) {
	tests := []struct {
		name        string
		tracker     *fakePresenceTracker
		sendMessage *agentv1.AgentMessage
	}{
		{
			name: "activity",
			tracker: &fakePresenceTracker{
				session:     presence.Session{MachineID: "machine-id", AgentID: "agent-id", ConnectedAt: time.Now()},
				activityErr: errors.New("database unavailable"),
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
