package presence

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Credentials struct {
	AgentID string
	Secret  string
}

type Identity struct {
	MachineID      string
	AgentID        string
	CredentialHash []byte
}

type Session struct {
	MachineID   string
	AgentID     string
	ConnectedAt time.Time
}

type Store interface {
	FindActiveIdentity(context.Context, string) (Identity, bool, error)
	StartSession(context.Context, string) (time.Time, error)
	TouchSession(context.Context, Session) error
	EndSession(context.Context, Session) error
}

var ErrUnauthenticated = errors.New("unauthenticated")

type Service struct {
	store Store
}

func New(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) OpenSession(ctx context.Context, credentials Credentials) (Session, error) {
	if !isUUID(credentials.AgentID) {
		return Session{}, ErrUnauthenticated
	}
	if strings.TrimSpace(credentials.Secret) == "" {
		return Session{}, ErrUnauthenticated
	}

	identity, found, err := s.store.FindActiveIdentity(ctx, credentials.AgentID)
	if err != nil {
		return Session{}, fmt.Errorf("find active identity: %w", err)
	}
	if !found {
		return Session{}, ErrUnauthenticated
	}
	secretHash := sha256.Sum256([]byte(credentials.Secret))
	if !hmac.Equal(secretHash[:], []byte(identity.CredentialHash)) {
		return Session{}, ErrUnauthenticated
	}

	connectionTime, err := s.store.StartSession(ctx, identity.MachineID)
	if err != nil {
		return Session{}, fmt.Errorf("start presence session: %w", err)
	}

	return Session{
		AgentID:     identity.AgentID,
		MachineID:   identity.MachineID,
		ConnectedAt: connectionTime,
	}, nil
}

func (s *Service) RecordHeartbeat(ctx context.Context, session Session) error {
	if err := s.store.TouchSession(ctx, session); err != nil {
		return fmt.Errorf("record heartbeat: %w", err)
	}
	return nil
}

func (s *Service) CloseSession(ctx context.Context, session Session) error {
	if err := s.store.EndSession(ctx, session); err != nil {
		return fmt.Errorf("end presence session: %w", err)
	}
	return nil
}

func isUUID(s string) bool {
	return uuid.Validate(s) == nil
}
