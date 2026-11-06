package devtools

import "context"

type statusReader func(context.Context) (WSLState, error)

// RefreshWSLStatus resolves the initial backend and reads its current state.
func RefreshWSLStatus(ctx context.Context, backend string, resolve bool) (string, WSLState, bool, error) {
	return refreshWSLStatus(ctx, backend, resolve, NSLStatus, LimaStatus)
}

func refreshWSLStatus(ctx context.Context, backend string, resolve bool, nslStatus, limaStatus statusReader) (string, WSLState, bool, error) {
	resolved := backend != BackendNSL
	if backend == BackendNSL && resolve {
		nsl, nslErr := nslStatus(ctx)
		lima, limaErr := limaStatus(ctx)
		if nslErr == nil && limaErr == nil {
			backend = ResolveBackend(backend, nsl.Exists, lima.Exists)
			resolved = true
		}
		if backend == BackendLima {
			return backend, lima, resolved, limaErr
		}
		return backend, nsl, resolved, nslErr
	}
	if backend == BackendLima {
		state, err := limaStatus(ctx)
		return backend, state, resolved, err
	}
	state, err := nslStatus(ctx)
	return backend, state, resolved, err
}
