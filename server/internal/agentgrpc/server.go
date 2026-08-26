package agentgrpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/idle-zero/arxveil/server/internal/enrollment"
	agentv1 "github.com/idle-zero/arxveil/server/internal/gen/agent/v1"
	"github.com/idle-zero/arxveil/server/internal/presence"
	"github.com/idle-zero/arxveil/server/internal/telemetry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const sessionCloseTimeout = 5 * time.Second

type Enroller interface {
	Enroll(context.Context, enrollment.Input) (enrollment.EnrolledAgent, error)
}

type PresenceTracker interface {
	OpenSession(context.Context, presence.Credentials) (presence.Session, error)
	RecordActivity(context.Context, presence.Session) error
	CloseSession(context.Context, presence.Session) error
}

type TelemetryRecorder interface {
	Record(context.Context, telemetry.Sample) error
}

type Server struct {
	agentv1.UnimplementedAgentServiceServer
	enroller          Enroller
	presenceTracker   PresenceTracker
	telemetryRecorder TelemetryRecorder
	logger            *slog.Logger
}

func NewServer(enroller Enroller, presenceTracker PresenceTracker, telemetryRecorder TelemetryRecorder, logger *slog.Logger) *Server {
	return &Server{
		enroller:          enroller,
		presenceTracker:   presenceTracker,
		telemetryRecorder: telemetryRecorder,
		logger:            logger,
	}
}

func (s *Server) Enroll(ctx context.Context, request *agentv1.EnrollRequest) (*agentv1.EnrollResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "enrollment request is required")
	}

	result, err := s.enroller.Enroll(ctx, enrollment.Input{
		Hostname:        request.GetHostname(),
		OperatingSystem: request.GetOperatingSystem(),
		OSVersion:       request.GetOsVersion(),
		Architecture:    request.GetArchitecture(),
		AgentVersion:    request.GetAgentVersion(),
		Capabilities:    request.GetCapabilities(),
	})
	if err != nil {
		switch {
		case errors.Is(err, enrollment.ErrInvalidInput):
			return nil, status.Error(codes.InvalidArgument, "invalid enrollment request")
		default:
			return nil, status.Error(codes.Internal, "enrollment failed")
		}
	}

	return &agentv1.EnrollResponse{
		MachineId:   result.MachineID,
		AgentId:     result.AgentID,
		AgentSecret: result.Secret,
	}, nil
}

func (s *Server) StreamUpdates(stream agentv1.AgentService_StreamUpdatesServer) error {
	message, err := stream.Recv()
	if errors.Is(err, io.EOF) {
		return status.Error(codes.InvalidArgument, "authenticate message is required")
	}
	if err != nil {
		return err
	}
	if message == nil || message.GetAuthenticate() == nil {
		return status.Error(codes.InvalidArgument, "first stream message must authenticate the agent")
	}

	authenticate := message.GetAuthenticate()
	session, err := s.presenceTracker.OpenSession(stream.Context(), presence.Credentials{
		AgentID: authenticate.GetAgentId(),
		Secret:  authenticate.GetAgentSecret(),
	})
	if err != nil {
		switch {
		case errors.Is(err, presence.ErrUnauthenticated):
			return status.Error(codes.Unauthenticated, "agent authentication failed")
		default:
			return status.Error(codes.Internal, "authenticate agent stream")
		}
	}

	s.logger.Info("agent stream authenticated", "machine_id", session.MachineID, "agent_id", session.AgentID)

	closed := false
	closeSession := func() error {
		if closed {
			return nil
		}
		closed = true

		ctx, cancel := context.WithTimeout(context.Background(), sessionCloseTimeout)
		defer cancel()
		if err := s.presenceTracker.CloseSession(ctx, session); err != nil {
			s.logger.Error("close agent stream", "machine_id", session.MachineID, "agent_id", session.AgentID, "error", err)
			return err
		}

		s.logger.Info("agent stream closed", "machine_id", session.MachineID, "agent_id", session.AgentID)
		return nil
	}
	defer func() {
		_ = closeSession()
	}()

	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if err := closeSession(); err != nil {
				return status.Error(codes.Internal, "close agent stream")
			}
			return stream.SendAndClose(&emptypb.Empty{})
		}
		if err != nil {
			return err
		}
		if message == nil {
			return status.Error(codes.InvalidArgument, "stream message payload is required")
		}

		switch {
		case message.GetHeartbeat() != nil:
			if err := s.presenceTracker.RecordActivity(stream.Context(), session); err != nil {
				return status.Error(codes.Internal, "record agent activity")
			}
		case message.GetAuthenticate() != nil:
			return status.Error(codes.InvalidArgument, "agent stream is already authenticated")
		case message.GetTelemetry() != nil:
			sample, err := telemetrySample(session, message.GetTelemetry())
			if err != nil {
				return status.Error(codes.InvalidArgument, "invalid telemetry update")
			}
			if err := s.telemetryRecorder.Record(stream.Context(), sample); err != nil {
				if errors.Is(err, telemetry.ErrInvalidSample) {
					return status.Error(codes.InvalidArgument, "invalid telemetry update")
				}
				return status.Error(codes.Internal, "record telemetry update")
			}
			if err := s.presenceTracker.RecordActivity(stream.Context(), session); err != nil {
				return status.Error(codes.Internal, "record agent activity")
			}
		default:
			return status.Error(codes.InvalidArgument, "stream message payload is required")
		}
	}
}

func telemetrySample(session presence.Session, update *agentv1.Telemetry) (telemetry.Sample, error) {
	if update == nil || update.GetCollectedAt() == nil {
		return telemetry.Sample{}, errors.New("telemetry collection time is required")
	}
	if err := update.GetCollectedAt().CheckValid(); err != nil {
		return telemetry.Sample{}, err
	}

	return telemetry.Sample{
		MachineID:        session.MachineID,
		CollectedAt:      update.GetCollectedAt().AsTime(),
		CPUUsagePercent:  update.GetCpuUsagePercent(),
		MemoryTotalBytes: update.GetMemoryTotalBytes(),
		MemoryUsedBytes:  update.GetMemoryUsedBytes(),
		DiskTotalBytes:   update.GetDiskTotalBytes(),
		DiskUsedBytes:    update.GetDiskUsedBytes(),
		UptimeSeconds:    update.GetUptimeSeconds(),
	}, nil
}
