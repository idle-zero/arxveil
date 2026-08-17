package agentgrpc

import (
	"context"
	"errors"

	"github.com/idle-zero/arxveil/server/internal/enrollment"
	agentv1 "github.com/idle-zero/arxveil/server/internal/gen/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Enroller interface {
	Enroll(context.Context, enrollment.Input) (enrollment.EnrolledAgent, error)
}

type Server struct {
	agentv1.UnimplementedAgentServiceServer
	enroller Enroller
}

func NewServer(enroller Enroller) *Server {
	return &Server{enroller: enroller}
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
