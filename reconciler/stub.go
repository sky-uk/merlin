package reconciler

type stub struct{}

// NewStub returns a reconciler which doesn't do anything.
func NewStub() Reconciler {
	return &stub{}
}

func (s *stub) Start() error {
	return nil
}

func (s *stub) Stop() {
	// do nothing
}

func (s *stub) Sync() {
	// do nothing
}
