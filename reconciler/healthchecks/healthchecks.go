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
	// GetDown returns the downed server IPs for the associated id. Downed servers are those that are not currently
	// passing the health check.
	GetDownServers(id string) []string
	// SetHealthCheck sets the health check for the given ID. If health check is nil, it is removed.
	SetHealthCheck(id string, check *types.VirtualService_HealthCheck) error
	// AddServer adds a server to health check.
	AddServer(id, ip string)
	// RemServer removes a server from health checks.
	RemServer(id, ip string)
	// Stop all health checks. Use at shutdown.
	Stop()
}

type checker struct {
	infos map[string]*checkInfo
	sync.Mutex
}

type checkInfo struct {
	healthCheck    *types.VirtualService_HealthCheck
	serverStatuses map[string]*checkStatus
	serverStopChs  map[string]chan struct{}
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
	return &checker{infos: make(map[string]*checkInfo)}
}

func (c *checker) GetDownServers(id string) []string {
	c.Lock()
	defer c.Unlock()

	info, ok := c.infos[id]
	if !ok || info.healthCheck.Endpoint.GetValue() == "" {
		return nil
	}

	var downServers []string
	for server, status := range info.serverStatuses {
		status.Lock()
		if status.state == serverDown {
			downServers = append(downServers, server)
		}
		status.Unlock()
	}
	return downServers
}

func validateCheck(check *types.VirtualService_HealthCheck) error {
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

func (c *checker) SetHealthCheck(id string, check *types.VirtualService_HealthCheck) error {
	if err := validateCheck(check); err != nil {
		return fmt.Errorf("invalid healthcheck %v: %v", check, err)
	}

	c.Lock()
	defer c.Unlock()

	if info, ok := c.infos[id]; ok {
		// don't update if it's the same
		if proto.Equal(info.healthCheck, check) {
			return nil
		}
		c.updateHealthCheck(info, check)
		return nil
	}

	info := &checkInfo{
		healthCheck:    check,
		serverStatuses: make(map[string]*checkStatus),
		serverStopChs:  make(map[string]chan struct{}),
	}
	c.infos[id] = info
	return nil
}

func (c *checker) updateHealthCheck(info *checkInfo, check *types.VirtualService_HealthCheck) {
	info.healthCheck = check

	// stop all existing health check goroutines
	for server, stopCh := range info.serverStopChs {
		close(stopCh)
		delete(info.serverStopChs, server)
	}

	if check.Endpoint.GetValue() == "" {
		return
	}

	// kick off new health checks with the updated check
	for server, status := range info.serverStatuses {
		info.startBackgroundHealthCheck(server, status)
	}
}

func (c *checker) AddServer(id, server string) {
	c.Lock()
	defer c.Unlock()

	info, ok := c.infos[id]
	if !ok {
		// initialise health check if not set yet
		c.Unlock()
		if err := c.SetHealthCheck(id, &types.VirtualService_HealthCheck{}); err != nil {
			panic(err)
		}
		c.Lock()
		info = c.infos[id]
	}
	if _, ok := info.serverStatuses[server]; ok {
		// server already added
		return
	}

	status := &checkStatus{
		state: serverDown,
		count: 0,
	}
	info.serverStatuses[server] = status

	if info.healthCheck.Endpoint.GetValue() == "" {
		//  nothing more to do, return
		return
	}

	// kick off health check
	info.startBackgroundHealthCheck(server, status)
}

func (info *checkInfo) startBackgroundHealthCheck(server string, status *checkStatus) {
	stopCh := make(chan struct{})
	info.serverStopChs[server] = stopCh
	go status.healthCheck(stopCh, server, info.healthCheck)
}

func (c *checker) RemServer(id, server string) {
	c.Lock()
	defer c.Unlock()

	info, ok := c.infos[id]
	if !ok {
		// nothing to remove
		return
	}
	if _, ok := info.serverStatuses[server]; !ok {
		// server already removed
		return
	}

	if stopCh, ok := info.serverStopChs[server]; ok {
		close(stopCh)
	}
	delete(info.serverStopChs, server)
	delete(info.serverStatuses, server)
}

func (c *checker) Stop() {
	c.Lock()
	defer c.Unlock()

	for _, info := range c.infos {
		c.updateHealthCheck(info, &types.VirtualService_HealthCheck{})
	}
}

// healthCheck a real server and update its status
func (s *checkStatus) healthCheck(stopCh <-chan struct{}, server string, healthCheck *types.VirtualService_HealthCheck) {
	log.Infof("Starting health check for %s: %v", server, healthCheck)

	// perform first health check immediately
	s.performHealthCheck(server, healthCheck)

	per, _ := ptypes.Duration(healthCheck.Period)
	t := time.NewTicker(per)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.performHealthCheck(server, healthCheck)
		case <-stopCh:
			log.Infof("Stopped health check for %s", server)
			return
		}
	}
}

func (s *checkStatus) performHealthCheck(server string, healthCheck *types.VirtualService_HealthCheck) {
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

	serverURL, err := url.Parse(fmt.Sprintf("http://%s:%s%s", server, checkURL.Port(), checkURL.Path))
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

func (s *checkStatus) incrementUp(healthCheck *types.VirtualService_HealthCheck) {
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

func (s *checkStatus) incrementDown(healthCheck *types.VirtualService_HealthCheck) {
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
