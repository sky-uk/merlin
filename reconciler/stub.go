package reconciler

import (
	log "github.com/sirupsen/logrus"
)

type stub struct{}

// NewStub returns a reconciler which doesn't do anything.
func NewStub() Reconciler {
	return &stub{}
}

func (s *stub) Start() error {
	log.Debug("stub-reconciler: Start()")
	return nil
}

func (s *stub) Stop() {
	log.Debug("stub-reconciler: Stop()")
}

func (s *stub) Sync() {
	log.Debug("stub-reconciler: Sync()")
}
