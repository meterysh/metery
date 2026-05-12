package service

import (
	"github.com/meterysh/metery/internal/store"
)

// Service implements all ConnectRPC service handlers.
// The methods are split across domain-specific files (customer.go, meter.go, etc.).
type Service struct {
	store *store.Store
}

func NewService(s *store.Store) *Service {
	return &Service{store: s}
}

