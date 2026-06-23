// Package service implements Oplog's application logic on top of a
// persistence.Store. It owns concerns the store deliberately omits:
// ID generation, timestamps, validation, and focus/session orchestration.
//
// The store stays a pure persistence boundary; entries arrive there fully
// formed.
package service

import (
	"time"

	"github.com/optimuspaul/personal-oplog/internal/id"
	"github.com/optimuspaul/personal-oplog/internal/persistence"
)

// Service coordinates journal and focus operations over a Store.
type Service struct {
	store persistence.Store
	now   func() time.Time
	newID func() string
}

// Option customizes a Service. The defaults (wall clock, ULID generator)
// are correct for production; options exist mainly for deterministic tests.
type Option func(*Service)

// WithClock overrides the time source used to stamp entries and sessions.
func WithClock(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

// WithIDGenerator overrides the identifier source for entries and sessions.
func WithIDGenerator(newID func() string) Option {
	return func(s *Service) { s.newID = newID }
}

// New constructs a Service backed by store.
func New(store persistence.Store, opts ...Option) *Service {
	s := &Service{
		store: store,
		now:   time.Now,
		newID: id.New,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
