package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakePinger struct {
	err error
}

func (p fakePinger) Ping(context.Context) error {
	return p.err
}

func TestHealthRoutes(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		databaseErr error
		wantStatus  int
	}{
		{
			name:        "live endpoint does not depend on database",
			path:        "/health/live",
			databaseErr: errors.New("database unavailable"),
			wantStatus:  http.StatusOK,
		},
		{
			name:       "ready endpoint succeeds when database is reachable",
			path:       "/health/ready",
			wantStatus: http.StatusOK,
		},
		{
			name:        "ready endpoint fails when database is unavailable",
			path:        "/health/ready",
			databaseErr: errors.New("database unavailable"),
			wantStatus:  http.StatusServiceUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := NewRouter(fakePinger{err: test.databaseErr})
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}
