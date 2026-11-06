package devtools

import (
	"context"
	"errors"
	"testing"
)

func TestRefreshWSLStatus(t *testing.T) {
	failure := errors.New("inventory failed")
	tests := []struct {
		name              string
		backend           string
		resolve           bool
		nsl, lima         WSLState
		nslErr, limaErr   error
		wantBackend       string
		wantState         WSLState
		wantErr           error
		wantResolved      bool
		wantNSL, wantLima int
	}{
		{name: "default NSL", backend: BackendNSL, resolve: true, nsl: WSLState{Exists: true, Running: true}, wantBackend: BackendNSL, wantState: WSLState{Exists: true, Running: true}, wantResolved: true, wantNSL: 1, wantLima: 1},
		{name: "NSL failure", backend: BackendNSL, resolve: true, nslErr: failure, wantBackend: BackendNSL, wantErr: failure, wantNSL: 1, wantLima: 1},
		{name: "Lima failure", backend: BackendNSL, resolve: true, nsl: WSLState{Exists: true}, limaErr: failure, wantBackend: BackendNSL, wantState: WSLState{Exists: true}, wantNSL: 1, wantLima: 1},
		{name: "select Lima", backend: BackendNSL, resolve: true, lima: WSLState{Exists: true, Running: true}, wantBackend: BackendLima, wantState: WSLState{Exists: true, Running: true}, wantResolved: true, wantNSL: 1, wantLima: 1},
		{name: "resolved NSL", backend: BackendNSL, nsl: WSLState{Exists: true}, wantBackend: BackendNSL, wantState: WSLState{Exists: true}, wantNSL: 1},
		{name: "selected Lima", backend: BackendLima, lima: WSLState{Exists: true}, wantBackend: BackendLima, wantState: WSLState{Exists: true}, wantResolved: true, wantLima: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nslCalls, limaCalls := 0, 0
			nsl := func(context.Context) (WSLState, error) { nslCalls++; return tt.nsl, tt.nslErr }
			lima := func(context.Context) (WSLState, error) { limaCalls++; return tt.lima, tt.limaErr }
			backend, state, err, resolved := refreshWSLStatus(context.Background(), tt.backend, tt.resolve, nsl, lima)
			if backend != tt.wantBackend || state != tt.wantState || !errors.Is(err, tt.wantErr) || resolved != tt.wantResolved || nslCalls != tt.wantNSL || limaCalls != tt.wantLima {
				t.Errorf("got backend=%q state=%+v err=%v resolved=%v calls=%d/%d; want backend=%q state=%+v err=%v resolved=%v calls=%d/%d", backend, state, err, resolved, nslCalls, limaCalls, tt.wantBackend, tt.wantState, tt.wantErr, tt.wantResolved, tt.wantNSL, tt.wantLima)
			}
		})
	}
}
