package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"

	"connectrpc.com/connect"
)

func AuthMiddleware(apiKeys []string) connect.Interceptor {
	var validKeys [][]byte
	for _, k := range apiKeys {
		if k != "" {
			validKeys = append(validKeys, []byte(k))
		}
	}

	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return connect.UnaryFunc(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			authHeader := req.Header().Get("Authorization")
			if authHeader == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
			}

			const prefix = "Bearer "
			if !strings.HasPrefix(authHeader, prefix) {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
			}

			token := []byte(authHeader[len(prefix):])
			for _, key := range validKeys {
				if subtle.ConstantTimeCompare(token, key) == 1 {
					return next(ctx, req)
				}
			}
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
		})
	})
}
