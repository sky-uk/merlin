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

// ServerStatus of a server.
type ServerStatus bool

const (
	// ServerUp if server is healthy.
	ServerUp ServerStatus = true
	// ServerDown if server is unhealthy.
	ServerDown ServerStatus = false
)

// TransitionFunc is called on a health state change.
type TransitionFunc func(state ServerStatus)

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

type checkKey struct {
	serviceID string
	key       *types.RealServer_Key
}

type checker struct {
	checks map[checkKey]*check
	sync.Mutex
}

type check struct {
	// immutable state
	serverIP    string
	healthCheck *types.RealServer_HealthCheck
	stopCh      chan struct{}

	// mutable state
	state *checkState
}

// checkState keeps track of the current status of a health check for a specific real server.
type checkState struct {
	// status represents the current state, either up or down.
	status ServerStatus
	// transitionCount is a state variable, whose meaning changes depending on state:
	//   * up   - count is the number of failed health checks
	//   * down - count is the number of successful health checks
	transitionCount uint32
	// transitionFn is called when the status changes.
	transitionFn TransitionFunc
	sync.Mutex
}

// New creates a new checker.
func New() Checker {
	return &checker{
		checks: make(map[checkKey]*check),
	}
}

func (c *checker) IsDown(serviceID string, key *types.RealServer_Key) bool {
	c.Lock()
	defer c.Unlock()

	checkKey := checkKey{serviceID: serviceID, key: key}
	check, ok := c.checks[checkKey]
	if !ok {
		panic(fmt.Sprintf("bug: health check not added yet: %v", checkKey))
	}
	if check.healthCheck.Endpoint.GetValue() == "" {
		return false
	}

	check.state.Lock()
	defer check.state.Unlock()
	return check.state.status == ServerDown
}

func validateHealthCheck(check *types.RealServer_HealthCheck) error {
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

func (c *checker) SetHealthCheck(serviceID string, key *types.RealServer_Key, healthCheck *types.RealServer_HealthCheck,
	fn TransitionFunc) error {

	if err := validateHealthCheck(healthCheck); err != nil {
		return fmt.Errorf("invalid healthcheck %v: %v", healthCheck, err)
	}

	if fn == nil {
		fn = func(_ ServerStatus) {}
	}

	c.Lock()
	defer c.Unlock()

	state := &checkState{
		status:          ServerDown,
		transitionCount: 0,
		transitionFn:    fn,
	}

	checkKey := checkKey{serviceID: serviceID, key: key}
	if origCheck, ok := c.checks[checkKey]; ok {

		origCheck.state.Lock()
		origCheck.state.transitionFn = fn
		origCheck.state.Unlock()

		// Don't replace health check if it's the same.
		if proto.Equal(origCheck.healthCheck, healthCheck) {
			return nil
		}

		// Stop any existing health check goroutine.
		close(origCheck.stopCh)

		// Copy prior state into new state.
		origCheck.state.Lock()
		state.status = origCheck.state.status
		state.transitionCount = origCheck.state.transitionCount
		origCheck.state.Unlock()
	}

	check := &check{
		serverIP:    key.Ip,
		healthCheck: healthCheck,
		state:       state,
		stopCh:      make(chan struct{}),
	}

	c.checks[checkKey] = check
	check.startBackgroundHealthCheck()
	return nil
}

func (c *check) startBackgroundHealthCheck() {
	if c.healthCheck.Endpoint.GetValue() == "" {
		// endpoint is empty, don't health check
		return
	}
	go c.healthCheckLoop()
}

func (c *checker) RemHealthCheck(serviceID string, key *types.RealServer_Key) {
	c.Lock()
	defer c.Unlock()

	checkKey := checkKey{serviceID: serviceID, key: key}
	check, ok := c.checks[checkKey]
	if !ok {
		// nothing to remove
		return
	}
	close(check.stopCh)
	delete(c.checks, checkKey)
}

func (c *checker) Stop() {
	c.Lock()
	var keys []checkKey
	for key := range c.checks {
		keys = append(keys, key)
	}
	c.Unlock()

	for _, key := range keys {
		c.RemHealthCheck(key.serviceID, key.key)
	}
}

// check a real server and update its status
func (c *check) healthCheckLoop() {
	log.Infof("Starting health check for %s: [%v]", c.serverIP, c.healthCheck.PrettyString())

	// perform first health check immediately
	c.performHealthCheck()

	checkPeriod, _ := ptypes.Duration(c.healthCheck.Period)
	t := time.NewTicker(checkPeriod)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			c.performHealthCheck()
		case <-c.stopCh:
			log.Infof("Stopped health check for %s", c.serverIP)
			return
		}
	}
}

func (c *check) performHealthCheck() {
	checkURL, err := url.Parse(c.healthCheck.Endpoint.GetValue())
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
	timeout, err := ptypes.Duration(c.healthCheck.Timeout)
	if err != nil {
		panic(err)
	}
	client := http.Client{
		Transport: tr,
		Timeout:   timeout,
	}

	serverURL, err := url.Parse(fmt.Sprintf("http://%s:%s%s", c.serverIP, checkURL.Port(), checkURL.Path))
	if err != nil {
		panic(err)
	}

	resp, err := client.Get(serverURL.String())
	if err != nil {
		log.Infof("%s inaccessible: %v", serverURL, err)
		c.markServerDown()
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || 300 <= resp.StatusCode {
		body, _ := ioutil.ReadAll(resp.Body)
		log.Infof("%s returned %d: %s", serverURL, resp.StatusCode, string(body))
		c.markServerDown()
		return
	}
	c.markServerUp()
}

func (c *check) markServerDown() {
	c.state.resetOrIncrement(ServerUp, ServerDown, c.healthCheck.DownThreshold)
}

func (c *check) markServerUp() {
	c.state.resetOrIncrement(ServerDown, ServerUp, c.healthCheck.UpThreshold)
}

func (s *checkState) resetOrIncrement(incrementStatus, nextStatus ServerStatus, transitionThreshold uint32) {
	s.Lock()
	defer s.Unlock()
	if s.status != incrementStatus {
		s.transitionCount = 0
		return
	}

	s.transitionCount++
	if s.transitionCount >= transitionThreshold {
		s.status = nextStatus
		s.transitionCount = 0
		s.transitionFn(s.status)
	}
}
