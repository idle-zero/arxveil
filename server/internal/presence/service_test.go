package presence

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

const validAgentID = "018f53c9-7b2c-7d48-91d0-5a69a34d6266"

type fakeStore struct {
	identity       Identity
	found          bool
	findErr        error
	connectedAt    time.Time
	startErr       error
	touchErr       error
	endErr         error
	lookedUpAgent  string
	startedMachine string
	touchedSession Session
	endedSession   Session
	touchCalls     int
	endCalls       int
}

func (s *fakeStore) FindActiveIdentity(_ context.Context, agentID string) (Identity, bool, error) {
	s.lookedUpAgent = agentID
	return s.identity, s.found, s.findErr
}

func (s *fakeStore) StartSession(_ context.Context, machineID string) (time.Time, error) {
	s.startedMachine = machineID
	return s.connectedAt, s.startErr
}

func (s *fakeStore) TouchSession(_ context.Context, session Session) error {
	s.touchCalls++
	s.touchedSession = session
	return s.touchErr
}

func (s *fakeStore) EndSession(_ context.Context, session Session) error {
	s.endCalls++
	s.endedSession = session
	return s.endErr
}

func TestOpenSessionAuthenticatesAndStartsSession(t *testing.T) {
	secret := "agent-secret"
	hash := sha256.Sum256([]byte(secret))
	connectedAt := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{
		identity: Identity{
			MachineID:      "machine-id",
			AgentID:        validAgentID,
			CredentialHash: hash[:],
		},
		found:       true,
		connectedAt: connectedAt,
	}

	session, err := New(store).OpenSession(context.Background(), Credentials{
		AgentID: validAgentID,
		Secret:  secret,
	})
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}

	if store.lookedUpAgent != validAgentID {
		t.Errorf("FindActiveIdentity() agent ID = %q, want %q", store.lookedUpAgent, validAgentID)
	}
	if store.startedMachine != "machine-id" {
		t.Errorf("StartSession() machine ID = %q, want %q", store.startedMachine, "machine-id")
	}
	if session.MachineID != "machine-id" || session.AgentID != validAgentID || !session.ConnectedAt.Equal(connectedAt) {
		t.Errorf("OpenSession() session = %+v", session)
	}
}

func TestOpenSessionRejectsUnauthenticatedCredentials(t *testing.T) {
	secret := "agent-secret"
	hash := sha256.Sum256([]byte(secret))

	tests := []struct {
		name        string
		credentials Credentials
		identity    Identity
		found       bool
	}{
		{
			name: "malformed agent ID",
			credentials: Credentials{
				AgentID: "not-a-uuid",
				Secret:  secret,
			},
		},
		{
			name: "blank secret",
			credentials: Credentials{
				AgentID: validAgentID,
				Secret:  " \t ",
			},
		},
		{
			name: "unknown identity",
			credentials: Credentials{
				AgentID: validAgentID,
				Secret:  secret,
			},
		},
		{
			name: "incorrect secret",
			credentials: Credentials{
				AgentID: validAgentID,
				Secret:  "incorrect-secret",
			},
			identity: Identity{
				MachineID:      "machine-id",
				AgentID:        validAgentID,
				CredentialHash: hash[:],
			},
			found: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{
				identity: test.identity,
				found:    test.found,
			}

			_, err := New(store).OpenSession(context.Background(), test.credentials)
			if !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("OpenSession() error = %v, want ErrUnauthenticated", err)
			}
			if store.startedMachine != "" {
				t.Errorf("StartSession() machine ID = %q, want no call", store.startedMachine)
			}
		})
	}
}

func TestOpenSessionReturnsStoreErrors(t *testing.T) {
	tests := []struct {
		name  string
		store *fakeStore
	}{
		{
			name:  "identity lookup",
			store: &fakeStore{findErr: errors.New("database unavailable")},
		},
		{
			name: "start session",
			store: func() *fakeStore {
				secretHash := sha256.Sum256([]byte("agent-secret"))
				return &fakeStore{
					identity: Identity{
						MachineID:      "machine-id",
						AgentID:        validAgentID,
						CredentialHash: secretHash[:],
					},
					found:    true,
					startErr: errors.New("database unavailable"),
				}
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(test.store).OpenSession(context.Background(), Credentials{
				AgentID: validAgentID,
				Secret:  "agent-secret",
			})
			if err == nil {
				t.Fatal("OpenSession() error = nil, want an error")
			}
			if errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("OpenSession() error = %v, must not be ErrUnauthenticated", err)
			}
		})
	}
}

func TestRecordActivity(t *testing.T) {
	session := Session{
		MachineID:   "machine-id",
		AgentID:     validAgentID,
		ConnectedAt: time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC),
	}
	store := &fakeStore{}

	if err := New(store).RecordActivity(context.Background(), session); err != nil {
		t.Fatalf("RecordActivity() error = %v", err)
	}
	if store.touchCalls != 1 || store.touchedSession != session {
		t.Errorf("TouchSession() calls = %d, session = %+v", store.touchCalls, store.touchedSession)
	}
}

func TestRecordActivityReturnsStoreError(t *testing.T) {
	wantErr := errors.New("database unavailable")
	err := New(&fakeStore{touchErr: wantErr}).RecordActivity(context.Background(), Session{})
	if !errors.Is(err, wantErr) {
		t.Errorf("RecordActivity() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestCloseSession(t *testing.T) {
	session := Session{
		MachineID:   "machine-id",
		AgentID:     validAgentID,
		ConnectedAt: time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC),
	}
	store := &fakeStore{}

	if err := New(store).CloseSession(context.Background(), session); err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if store.endCalls != 1 || store.endedSession != session {
		t.Errorf("EndSession() calls = %d, session = %+v", store.endCalls, store.endedSession)
	}
}

func TestCloseSessionReturnsStoreError(t *testing.T) {
	wantErr := errors.New("database unavailable")
	err := New(&fakeStore{endErr: wantErr}).CloseSession(context.Background(), Session{})
	if !errors.Is(err, wantErr) {
		t.Errorf("CloseSession() error = %v, want wrapped %v", err, wantErr)
	}
}
