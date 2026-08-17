package enrollment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	credentialBytes  = 32
	maxHostname      = 255
	maxMetadataValue = 255
	maxCapabilities  = 32
	maxCapability    = 64
)

var ErrInvalidInput = errors.New("invalid enrollment input")

// Input holds agent-supplied enrollment metadata.
type Input struct {
	Hostname        string
	OperatingSystem string
	OSVersion       string
	Architecture    string
	AgentVersion    string
	Capabilities    []string
}

// EnrolledAgent holds the permanent agent identity.
type EnrolledAgent struct {
	MachineID string
	AgentID   string
	Secret    string
}

// StoredEnrollment contains data persisted during enrollment.
type StoredEnrollment struct {
	CredentialHash  []byte
	Hostname        string
	OperatingSystem string
	OSVersion       string
	Architecture    string
	AgentVersion    string
	Capabilities    []string
}

// Store persists enrollment data.
type Store interface {
	Enroll(context.Context, StoredEnrollment) (machineID, agentID string, err error)
}

type Service struct {
	store  Store
	random io.Reader
}

func New(store Store) *Service {
	return newService(store, rand.Reader)
}

func newService(store Store, random io.Reader) *Service {
	return &Service{
		store:  store,
		random: random,
	}
}

func (s *Service) Enroll(ctx context.Context, input Input) (EnrolledAgent, error) {
	input, err := validateInput(input)
	if err != nil {
		return EnrolledAgent{}, err
	}

	secret, err := randomCredential(s.random)
	if err != nil {
		return EnrolledAgent{}, fmt.Errorf("generate agent credential: %w", err)
	}

	credentialHash := sha256.Sum256([]byte(secret))
	machineID, agentID, err := s.store.Enroll(ctx, StoredEnrollment{
		CredentialHash:  credentialHash[:],
		Hostname:        input.Hostname,
		OperatingSystem: input.OperatingSystem,
		OSVersion:       input.OSVersion,
		Architecture:    input.Architecture,
		AgentVersion:    input.AgentVersion,
		Capabilities:    input.Capabilities,
	})
	if err != nil {
		return EnrolledAgent{}, err
	}

	return EnrolledAgent{
		MachineID: machineID,
		AgentID:   agentID,
		Secret:    secret,
	}, nil
}

func validateInput(input Input) (Input, error) {
	var err error
	if input.Hostname, err = requiredMetadata("hostname", input.Hostname, maxHostname); err != nil {
		return Input{}, err
	}
	if input.OperatingSystem, err = requiredMetadata("operating system", input.OperatingSystem, maxMetadataValue); err != nil {
		return Input{}, err
	}
	if input.OSVersion, err = requiredMetadata("OS version", input.OSVersion, maxMetadataValue); err != nil {
		return Input{}, err
	}
	if input.Architecture, err = requiredMetadata("architecture", input.Architecture, maxMetadataValue); err != nil {
		return Input{}, err
	}
	if input.AgentVersion, err = requiredMetadata("agent version", input.AgentVersion, maxMetadataValue); err != nil {
		return Input{}, err
	}

	if len(input.Capabilities) > maxCapabilities {
		return Input{}, fmt.Errorf("%w: too many capabilities", ErrInvalidInput)
	}

	capabilities := make([]string, 0, len(input.Capabilities))
	seen := make(map[string]struct{}, len(input.Capabilities))
	for _, capability := range input.Capabilities {
		capability = strings.TrimSpace(capability)
		if capability == "" || len(capability) > maxCapability {
			return Input{}, fmt.Errorf("%w: capability must be between 1 and %d characters", ErrInvalidInput, maxCapability)
		}
		if _, exists := seen[capability]; exists {
			continue
		}
		seen[capability] = struct{}{}
		capabilities = append(capabilities, capability)
	}
	input.Capabilities = capabilities

	return input, nil
}

func requiredMetadata(name, value string, maximum int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximum {
		return "", fmt.Errorf("%w: %s must be between 1 and %d characters", ErrInvalidInput, name, maximum)
	}
	return value, nil
}

func randomCredential(random io.Reader) (string, error) {
	bytes := make([]byte, credentialBytes)
	if _, err := io.ReadFull(random, bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
