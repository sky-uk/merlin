package reconciler

import (
	"time"

	"context"

	"fmt"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/wrappers"
	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/merlin/ipvs"
	"github.com/sky-uk/merlin/reconciler/healthchecks"
	"github.com/sky-uk/merlin/types"
)

const (
	initTimeout       = 10 * time.Second
	reconcilerTimeout = 1 * time.Minute
)

type reconciler struct {
	period  time.Duration
	syncCh  chan struct{}
	store   Store
	ipvs    ipvs.IPVS
	checker healthchecks.Checker
	flush   bool
	stopCh  chan struct{}
}

// Store expected store interface for reconciler.
type Store interface {
	ListServices(context.Context) ([]*types.VirtualService, error)
	ListServers(ctx context.Context, serviceID string) ([]*types.RealServer, error)
}

// Reconciler reconciles store with local IPVS state.
type Reconciler interface {
	Start() error
	Stop()
	Sync()
}

// New returns a reconciler that populates the ipvs state periodically and on demand.
func New(period time.Duration, store Store, ipvs ipvs.IPVS) Reconciler {
	return &reconciler{
		period:  period,
		syncCh:  make(chan struct{}),
		store:   store,
		ipvs:    ipvs,
		checker: healthchecks.New(),
		stopCh:  make(chan struct{}),
	}
}

func (r *reconciler) Start() error {
	log.Debug("Starting reconciler loop")
	go func() {
		for {
			t := time.NewTimer(r.period)
			select {
			case <-t.C:
				r.reconcile()
			case <-r.syncCh:
				r.reconcile()
			case <-r.stopCh:
				log.Debug("Stopped reconciler loop")
				return
			}
		}
	}()
	err := r.initializeHealthChecks()
	if err != nil {
		r.Stop()
		return err
	}
	return nil
}

func (r *reconciler) initializeHealthChecks() error {
	ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()

	services, err := r.store.ListServices(ctx)
	if err != nil {
		return fmt.Errorf("failed to query store when initializing: %v", err)
	}

	for _, service := range services {
		servers, err := r.store.ListServers(ctx, service.Id)
		if err != nil {
			return fmt.Errorf("failed to query store when initializing: %v", err)
		}
		for _, server := range servers {
			fn := r.createHealthStateWeightUpdater(service.Key, server)
			r.checker.SetHealthCheck(server.ServiceID, server.Key, server.HealthCheck, fn)
		}
	}

	return nil
}

func (r *reconciler) createHealthStateWeightUpdater(serviceKey *types.VirtualService_Key,
	originalServer *types.RealServer) healthchecks.TransitionFunc {

	// clone the original server, to protect against external mutation
	server := proto.Clone(originalServer).(*types.RealServer)

	return func(state healthchecks.HealthState) {
		serverCopy := proto.Clone(server).(*types.RealServer)
		switch state {
		case healthchecks.ServerDown:
			serverCopy.Config.Weight = &wrappers.UInt32Value{Value: 0}
		case healthchecks.ServerUp:
			// change nothing - restore the original weight
		default:
			panic("unexpected state")
		}

		if err := r.ipvs.UpdateServer(serviceKey, serverCopy); err != nil {
			log.Warnf("Unable to update the weight for %v: %v", serverCopy, err)
		}
	}
}

func (r *reconciler) Stop() {
	close(r.stopCh)
	r.checker.Stop()
}

func (r *reconciler) Sync() {
	go func() { r.syncCh <- struct{}{} }()
}

func (r *reconciler) reconcile() {
	log.Debug("Starting reconcile")
	defer log.Debug("Finished reconcile")
	ctx, cancel := context.WithTimeout(context.Background(), reconcilerTimeout)
	defer cancel()

	desiredServices, err := r.store.ListServices(ctx)
	if err != nil {
		log.Errorf("unable to populate: %v", err)
		return
	}

	actualServices, err := r.ipvs.ListServices()
	if err != nil {
		log.Errorf("unable to populate: %v", err)
		return
	}

	// create or update services
	for _, desiredService := range desiredServices {
		desiredService.SortFlags()
		var match *types.VirtualService
		for _, actual := range actualServices {
			if proto.Equal(desiredService.Key, actual.Key) {
				match = actual
				match.SortFlags()
				break
			}
		}

		if match == nil {
			log.Infof("Adding virtual service: %v", desiredService)
			if err := r.ipvs.AddService(desiredService); err != nil {
				log.Errorf("Unable to add service: %v", err)
			}
		} else if !proto.Equal(desiredService.Config, match.Config) {
			log.Infof("Updating virtual service %q: [%v] to [%v]", desiredService.Id, match.Config, desiredService.Config)
			if err := r.ipvs.UpdateService(desiredService); err != nil {
				log.Errorf("Unable to update service: %v", err)
			}
		}

		desiredServers, err := r.store.ListServers(ctx, desiredService.Id)
		if err != nil {
			log.Errorf("unable to list servers in store for %s: %v", desiredService.Key, err)
			continue
		}
		actualServers, err := r.ipvs.ListServers(desiredService.Key)
		if err != nil {
			log.Errorf("unable to list servers in ipvs for %v: %v", desiredService.Key, err)
			continue
		}

		// update servers
		for _, desiredServer := range desiredServers {
			var match *types.RealServer
			for _, actualServer := range actualServers {
				if proto.Equal(desiredServer.Key, actualServer.Key) {
					match = actualServer
					break
				}
			}

			// update health check
			fn := r.createHealthStateWeightUpdater(desiredService.Key, desiredServer)
			r.checker.SetHealthCheck(desiredServer.ServiceID, desiredServer.Key, desiredServer.HealthCheck, fn)
			if r.checker.IsDown(desiredServer.ServiceID, desiredServer.Key) {
				desiredServer.Config.Weight = &wrappers.UInt32Value{Value: 0}
			}

			// update IPVS
			if match == nil {
				log.Infof("Adding real server: %v", desiredServer)
				if err := r.ipvs.AddServer(desiredService.Key, desiredServer); err != nil {
					log.Errorf("Unable to add server: %v", err)
				}
			} else if !proto.Equal(desiredServer.Config, match.Config) {
				log.Infof("Updating real server: %v", desiredServer)
				if err := r.ipvs.UpdateServer(desiredService.Key, desiredServer); err != nil {
					log.Errorf("Unable to update server: %v", err)
				}
			}
		}

		// remove old servers
		for _, actualServer := range actualServers {
			var found bool
			for _, desiredServer := range desiredServers {
				if proto.Equal(actualServer.Key, desiredServer.Key) {
					found = true
					break
				}
			}
			if !found {
				log.Infof("Deleting real server: %v", actualServer)
				// remove health check
				r.checker.RemHealthCheck(desiredService.Id, actualServer.Key)
				// remove from ipvs
				if err := r.ipvs.DeleteServer(desiredService.Key, actualServer); err != nil {
					log.Errorf("Unable to delete server: %v", err)
				}
			}
		}
	}

	// delete services
	for _, actual := range actualServices {
		var found bool
		for _, desired := range desiredServices {
			if proto.Equal(actual.Key, desired.Key) {
				found = true
				break
			}
		}
		if !found {
			log.Infof("Deleting virtual service: %v", actual)
			if err := r.ipvs.DeleteService(actual.Key); err != nil {
				log.Errorf("Unable to delete service: %v", err)
			}
		}
	}
}
