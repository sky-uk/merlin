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

// Checker checks if the real servers of a virtual service are up.
type Checker interface {
	// IsDown returns true if the server is down.
	IsDown(serviceID string, key *types.RealServer_Key) bool
	// SetHealthCheck on the given server, replacing any existing health check.
	SetHealthCheck(serviceID string, key *types.RealServer_Key, check *types.RealServer_HealthCheck) error
	// RemHealthCheck for the given server.
	RemHealthCheck(serviceID string, key *types.RealServer_Key)
	// Stop all health checks. Use at shutdown.
	Stop()
}

type infoKey struct {
	id  string
	key types.RealServer_Key
}

type checker struct {
	infos map[infoKey]*checkInfo
	sync.Mutex
}

type checkInfo struct {
	serverIP    string
	healthCheck *types.RealServer_HealthCheck
	status      *checkStatus
	stopCh      chan struct{}
}

type healthState bool

const (
	serverUp   healthState = true
	serverDown healthState = false
)

// checkStatus keeps track of the current status of a health check for a specific real server.
// state field represents the current state, either up or down.
// count field is a state variable, whose meaning changes depending on state:
// * up   - count is the number of failed health checks
// * down - count is the number of successful health checks
type checkStatus struct {
	state healthState
	count uint32
	sync.Mutex
}

// New creates a new checker.
func New() Checker {
	return &checker{infos: make(map[infoKey]*checkInfo)}
}

func (c *checker) IsDown(serviceID string, key *types.RealServer_Key) bool {
	c.Lock()
	defer c.Unlock()

	infoKey := infoKey{id: serviceID, key: *key}
	info, ok := c.infos[infoKey]
	if !ok {
		panic(fmt.Sprintf("bug: health check not added yet: %v", infoKey))
	}
	if info.healthCheck.Endpoint.GetValue() == "" {
		return false
	}

	info.status.Lock()
	defer info.status.Unlock()
	return info.status.state == serverDown
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

func (c *checker) SetHealthCheck(serviceID string, key *types.RealServer_Key, check *types.RealServer_HealthCheck) error {
	if err := validateCheck(check); err != nil {
		return fmt.Errorf("invalid healthcheck %v: %v", check, err)
	}

	c.Lock()
	defer c.Unlock()

	infoKey := infoKey{id: serviceID, key: *key}
	if info, ok := c.infos[infoKey]; ok {
		// don't update if it's the same
		if proto.Equal(info.healthCheck, check) {
			return nil
		}
		c.updateHealthCheck(info, check)
		return nil
	}

	info := &checkInfo{
		serverIP:    key.Ip,
		healthCheck: check,
		status: &checkStatus{
			state: serverDown,
			count: 0,
		},
	}
	c.infos[infoKey] = info

	info.startBackgroundHealthCheck()
	return nil
}

func (c *checker) updateHealthCheck(info *checkInfo, check *types.RealServer_HealthCheck) {
	info.healthCheck = check

	// stop any existing health check goroutine
	if info.stopCh != nil {
		close(info.stopCh)
		info.stopCh = nil
	}

	// kick off new health check
	info.startBackgroundHealthCheck()
}

func (info *checkInfo) startBackgroundHealthCheck() {
	if info.healthCheck.Endpoint.GetValue() == "" {
		// endpoint is empty, don't health check
		return
	}
	stopCh := make(chan struct{})
	info.stopCh = stopCh
	go info.status.healthCheck(info.stopCh, info.serverIP, info.healthCheck)
}

func (c *checker) RemHealthCheck(serviceID string, key *types.RealServer_Key) {
	c.Lock()
	defer c.Unlock()

	infoKey := infoKey{id: serviceID, key: *key}
	info, ok := c.infos[infoKey]
	if !ok {
		// nothing to remove
		return
	}

	if info.stopCh != nil {
		close(info.stopCh)
	}
	delete(c.infos, infoKey)
}

func (c *checker) Stop() {
	c.Lock()
	defer c.Unlock()

	for _, info := range c.infos {
		c.updateHealthCheck(info, &types.RealServer_HealthCheck{})
	}
}

// healthCheck a real server and update its status
func (s *checkStatus) healthCheck(stopCh <-chan struct{}, serverIP string, check *types.RealServer_HealthCheck) {
	log.Infof("Starting health check for %s: %v", serverIP, check)

	// perform first health check immediately
	s.performHealthCheck(serverIP, check)

	per, _ := ptypes.Duration(check.Period)
	t := time.NewTicker(per)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.performHealthCheck(serverIP, check)
		case <-stopCh:
			log.Infof("Stopped health check for %s", serverIP)
			return
		}
	}
}

func (s *checkStatus) performHealthCheck(serverIP string, healthCheck *types.RealServer_HealthCheck) {
	checkURL, err := url.Parse(healthCheck.Endpoint.GetValue())
	if err != nil {
		panic(err)
	}
	if checkURL.Scheme != "http" {
		panic("unsupported health check scheme " + checkURL.Scheme)
	}

	// Create a custom transport so we don't reuse prior connections, which might hide connectivity problems.
	tr := &http.Transport{
		DisableKeepAlives: true,
	}
	timeout, err := ptypes.Duration(healthCheck.Timeout)
	if err != nil {
		panic(err)
	}
	client := http.Client{
		Transport: tr,
		Timeout:   timeout,
	}

	serverURL, err := url.Parse(fmt.Sprintf("http://%s:%s%s", serverIP, checkURL.Port(), checkURL.Path))
	if err != nil {
		panic(err)
	}

	resp, err := client.Get(serverURL.String())
	if err != nil {
		log.Infof("%s inaccessible: %v", serverURL, err)
		s.incrementDown(healthCheck)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || 300 <= resp.StatusCode {
		body, _ := ioutil.ReadAll(resp.Body)
		log.Infof("%s returned %d: %s", serverURL, resp.StatusCode, string(body))
		s.incrementDown(healthCheck)
		return
	}
	s.incrementUp(healthCheck)
}

func (s *checkStatus) incrementUp(healthCheck *types.RealServer_HealthCheck) {
	s.Lock()
	defer s.Unlock()
	switch s.state {
	case serverDown:
		s.count++
		if s.count >= healthCheck.UpThreshold {
			s.state = serverUp
			s.count = 0
		}
	case serverUp:
		s.count = 0
	}
}

func (s *checkStatus) incrementDown(healthCheck *types.RealServer_HealthCheck) {
	s.Lock()
	defer s.Unlock()
	switch s.state {
	case serverDown:
		s.count = 0
	case serverUp:
		s.count++
		if s.count >= healthCheck.DownThreshold {
			s.state = serverDown
			s.count = 0
		}
	}
}
