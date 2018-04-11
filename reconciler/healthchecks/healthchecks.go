package healthchecks

import (
	"sync"
	"time"

	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"errors"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/merlin/types"
)

// HealthState of a server.
type HealthState bool

const (
	// ServerUp if server is healthy.
	ServerUp HealthState = true
	// ServerDown if server is unhealthy.
	ServerDown HealthState = false
)

// TransitionFunc is called on a health state change.
type TransitionFunc func(state HealthState)

// Checker checks if the real servers of a virtual service are up.
type Checker interface {
	// IsDown returns true if the server is down.
	IsDown(serviceID string, key *types.RealServer_Key) bool
	// SetHealthCheck on the given server, replacing any existing health check.
	SetHealthCheck(serviceID string, key *types.RealServer_Key, check *types.RealServer_HealthCheck,
		fn TransitionFunc) error
	// RemHealthCheck for the given server.
	RemHealthCheck(serviceID string, key *types.RealServer_Key)
	// Stop all health checks. Use at shutdown.
	Stop()
}

type healthCheckStateKey struct {
	id  string
	key types.RealServer_Key
}

type checker struct {
	healthCheckStates map[healthCheckStateKey]*healthCheckState
	sync.Mutex
}

type healthCheckState struct {
	// immutable state
	serverIP    string
	healthCheck *types.RealServer_HealthCheck
	stopCh      chan struct{}

	// mutable state
	status       *healthCheckStatus
	transitionFn TransitionFunc
	// Guards status and transitionFn which can change asynchronously.
	sync.Mutex
}

// healthCheckStatus keeps track of the current status of a health check for a specific real server.
// state field represents the current state, either up or down.
// count field is a state variable, whose meaning changes depending on state:
// * up   - count is the number of failed health checks
// * down - count is the number of successful health checks
type healthCheckStatus struct {
	state HealthState
	count uint32
}

// New creates a new checker.
func New() Checker {
	return &checker{
		healthCheckStates: make(map[healthCheckStateKey]*healthCheckState),
	}
}

func (c *checker) IsDown(serviceID string, key *types.RealServer_Key) bool {
	c.Lock()
	defer c.Unlock()

	stateKey := healthCheckStateKey{id: serviceID, key: *key}
	state, ok := c.healthCheckStates[stateKey]
	if !ok {
		panic(fmt.Sprintf("bug: health check not added yet: %v", stateKey))
	}
	if state.healthCheck.Endpoint.GetValue() == "" {
		return false
	}

	state.Lock()
	defer state.Unlock()
	return state.status.state == ServerDown
}

func validateCheck(check *types.RealServer_HealthCheck) error {
	if check == nil {
		return errors.New("check should be non-nil")
	}
	if check.Endpoint.GetValue() == "" {
		return nil
	}
	u, err := url.Parse(check.Endpoint.GetValue())
	if err != nil {
		return fmt.Errorf("endpoint not a URL: %v", err)
	}
	switch u.Scheme {
	case "http":
		// supported
	default:
		return fmt.Errorf("unsupported scheme %s", u.Scheme)
	}
	return nil
}

func (c *checker) SetHealthCheck(serviceID string, key *types.RealServer_Key, check *types.RealServer_HealthCheck,
	fn TransitionFunc) error {

	if err := validateCheck(check); err != nil {
		return fmt.Errorf("invalid healthcheck %v: %v", check, err)
	}

	if fn == nil {
		fn = func(_ HealthState) {}
	}

	c.Lock()
	defer c.Unlock()

	status := &healthCheckStatus{
		state: ServerDown,
		count: 0,
	}

	stateKey := healthCheckStateKey{id: serviceID, key: *key}
	if origState, ok := c.healthCheckStates[stateKey]; ok {

		origState.Lock()
		origState.transitionFn = fn
		origState.Unlock()

		// Don't replace health check if it's the same.
		if proto.Equal(origState.healthCheck, check) {
			return nil
		}

		// Stop any existing health check goroutine.
		if origState.stopCh != nil {
			close(origState.stopCh)
		}

		// Copy prior state into new state.
		origState.Lock()
		status.state = origState.status.state
		status.count = origState.status.count
		origState.Unlock()
	}

	state := &healthCheckState{
		serverIP:     key.Ip,
		healthCheck:  check,
		status:       status,
		transitionFn: fn,
	}

	c.healthCheckStates[stateKey] = state
	state.startBackgroundHealthCheck()
	return nil
}

func (s *healthCheckState) startBackgroundHealthCheck() {
	if s.healthCheck.Endpoint.GetValue() == "" {
		// endpoint is empty, don't health check
		return
	}
	stopCh := make(chan struct{})
	s.stopCh = stopCh
	go s.healthCheckLoop()
}

func (c *checker) RemHealthCheck(serviceID string, key *types.RealServer_Key) {
	c.Lock()
	defer c.Unlock()

	stateKey := healthCheckStateKey{id: serviceID, key: *key}
	state, ok := c.healthCheckStates[stateKey]
	if !ok {
		// nothing to remove
		return
	}

	if state.stopCh != nil {
		close(state.stopCh)
	}
	delete(c.healthCheckStates, stateKey)
}

func (c *checker) Stop() {
	c.Lock()
	var keys []healthCheckStateKey
	for key := range c.healthCheckStates {
		keys = append(keys, key)
	}
	c.Unlock()

	for _, key := range keys {
		c.RemHealthCheck(key.id, &key.key)
	}
}

// healthCheck a real server and update its status
func (s *healthCheckState) healthCheckLoop() {
	log.Infof("Starting health check for %s: %v", s.serverIP, s.healthCheck)

	// perform first health check immediately
	s.performHealthCheck()

	per, _ := ptypes.Duration(s.healthCheck.Period)
	t := time.NewTicker(per)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.performHealthCheck()
		case <-s.stopCh:
			log.Infof("Stopped health check for %s", s.serverIP)
			return
		}
	}
}

func (s *healthCheckState) performHealthCheck() {
	checkURL, err := url.Parse(s.healthCheck.Endpoint.GetValue())
	if err != nil {
		panic(err)
	}
	if checkURL.Scheme != "http" {
		panic("unsupported health check scheme " + checkURL.Scheme)
	}

	// Create a custom transport so we don't reuse prior connections - which might hide connectivity problems.
	tr := &http.Transport{
		DisableKeepAlives: true,
	}
	timeout, err := ptypes.Duration(s.healthCheck.Timeout)
	if err != nil {
		panic(err)
	}
	client := http.Client{
		Transport: tr,
		Timeout:   timeout,
	}

	serverURL, err := url.Parse(fmt.Sprintf("http://%s:%s%s", s.serverIP, checkURL.Port(), checkURL.Path))
	if err != nil {
		panic(err)
	}

	resp, err := client.Get(serverURL.String())
	if err != nil {
		log.Infof("%s inaccessible: %v", serverURL, err)
		s.incrementDown()
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || 300 <= resp.StatusCode {
		body, _ := ioutil.ReadAll(resp.Body)
		log.Infof("%s returned %d: %s", serverURL, resp.StatusCode, string(body))
		s.incrementDown()
		return
	}
	s.incrementUp()
}

func (s *healthCheckState) incrementUp() {
	s.Lock()
	defer s.Unlock()
	status := s.status
	switch status.state {
	case ServerDown:
		status.count++
		if status.count >= s.healthCheck.UpThreshold {
			status.state = ServerUp
			status.count = 0
			s.transitionFn(status.state)
		}
	case ServerUp:
		status.count = 0
	}
}

func (s *healthCheckState) incrementDown() {
	s.Lock()
	defer s.Unlock()
	status := s.status
	switch status.state {
	case ServerDown:
		status.count = 0
	case ServerUp:
		status.count++
		if status.count >= s.healthCheck.DownThreshold {
			status.state = ServerDown
			status.count = 0
			s.transitionFn(status.state)
		}
	}
}
