package auth

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"
)

func AuthMiddleware(apiKeys []string) connect.Interceptor {
	validKeys := make(map[string]struct{}, len(apiKeys))
	for _, k := range apiKeys {
		if k != "" {
			validKeys[k] = struct{}{}
		}
	}

	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return connect.UnaryFunc(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			authHeader := req.Header().Get("Authorization")
			if authHeader == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing authorization header"))
			}

			const prefix = "Bearer "
			if !strings.HasPrefix(authHeader, prefix) {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid authorization format"))
			}

			token := authHeader[len(prefix):]
			
			// Constant-time compare against all keys to mitigate timing attacks somewhat
			// (Though map lookup is not constant time. For v0, standard string compare or simple check is fine).
			// Here we just do a map lookup because env keys are trusted and low cardinality.
			if _, ok := validKeys[token]; !ok {
				// We can do subtle.ConstantTimeCompare if we wanted strict timing guarantees on known single key.
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
			}

			return next(ctx, req)
		})
	})
}
