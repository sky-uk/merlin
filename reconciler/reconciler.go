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
	storeTimeout = 30 * time.Second
	ipvsTimeout  = 30 * time.Second
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
	services, err := r.listStoreServices()
	if err != nil {
		return fmt.Errorf("failed to query store when initializing: %v", err)
	}

	for _, service := range services {
		servers, err := r.listStoreServers(service.Id)
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

	return func(state healthchecks.ServerStatus) {
		serverCopy := proto.Clone(server).(*types.RealServer)
		switch state {
		case healthchecks.ServerDown:
			serverCopy.Config.Weight = &wrappers.UInt32Value{Value: 0}
		case healthchecks.ServerUp:
			// change nothing - restore the original weight
		default:
			panic("unexpected state")
		}

		if err := r.updateIPVSServer(serviceKey, serverCopy); err != nil {
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

	desiredServices, err := r.listStoreServices()
	if err != nil {
		log.Errorf("Unable to populate: %v", err)
		return
	}

	actualServices, err := r.listIPVSServices()
	if err != nil {
		log.Panicf("Unable to populate: %v", err)
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
			log.Infof("Adding virtual service: %s", desiredService.PrettyString())
			if err := r.addIPVSService(desiredService); err != nil {
				log.Panicf("Unable to add service: %v", err)
			}
		} else if !proto.Equal(desiredService.Config, match.Config) {
			log.Infof("Updating virtual service %q: [%v] to [%v]", desiredService.Id, match.Config.PrettyString(),
				desiredService.Config.PrettyString())
			if err := r.updateIPVSService(desiredService); err != nil {
				log.Panicf("Unable to update service: %v", err)
			}
		}

		desiredServers, err := r.listStoreServers(desiredService.Id)
		if err != nil {
			log.Errorf("Unable to list servers in store for %s: %v", desiredService.Key.PrettyString(), err)
			continue
		}
		actualServers, err := r.listIPVSServers(desiredService.Key)
		if err != nil {
			log.Panicf("Unable to list servers in ipvs for %v: %v", desiredService.Key.PrettyString(), err)
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
				log.Infof("Adding real server: %v", desiredServer.PrettyString())
				if err := r.addIPVSServer(desiredService.Key, desiredServer); err != nil {
					log.Panicf("Unable to add server: %v", err)
				}
			} else if !proto.Equal(desiredServer.Config, match.Config) {
				log.Infof("Updating real server: %v", desiredServer.PrettyString())
				if err := r.updateIPVSServer(desiredService.Key, desiredServer); err != nil {
					log.Panicf("Unable to update server: %v", err)
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
				log.Infof("Deleting real server: %v", actualServer.PrettyString())
				// remove health check
				r.checker.RemHealthCheck(desiredService.Id, actualServer.Key)
				// remove from ipvs
				if err := r.deleteIPVSServer(desiredService.Key, actualServer); err != nil {
					log.Panicf("Unable to delete server: %v", err)
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
			log.Infof("Deleting virtual service: %v", actual.PrettyString())
			if err := r.deleteIPVSService(actual.Key); err != nil {
				log.Panicf("Unable to delete service: %v", err)
			}
		}
	}
}

func (r *reconciler) listStoreServices() ([]*types.VirtualService, error) {
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	return r.store.ListServices(ctx)
}

func (r *reconciler) listStoreServers(serviceID string) ([]*types.RealServer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	return r.store.ListServers(ctx, serviceID)
}

func (r *reconciler) addIPVSService(svc *types.VirtualService) error {
	ctx, cancel := context.WithTimeout(context.Background(), ipvsTimeout)
	defer cancel()
	return r.ipvs.AddService(ctx, svc)
}

func (r *reconciler) updateIPVSService(svc *types.VirtualService) error {
	ctx, cancel := context.WithTimeout(context.Background(), ipvsTimeout)
	defer cancel()
	return r.ipvs.UpdateService(ctx, svc)
}

func (r *reconciler) deleteIPVSService(key *types.VirtualService_Key) error {
	ctx, cancel := context.WithTimeout(context.Background(), ipvsTimeout)
	defer cancel()
	return r.ipvs.DeleteService(ctx, key)
}

func (r *reconciler) listIPVSServices() ([]*types.VirtualService, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ipvsTimeout)
	defer cancel()
	return r.ipvs.ListServices(ctx)
}

func (r *reconciler) addIPVSServer(key *types.VirtualService_Key, server *types.RealServer) error {
	ctx, cancel := context.WithTimeout(context.Background(), ipvsTimeout)
	defer cancel()
	return r.ipvs.AddServer(ctx, key, server)
}

func (r *reconciler) updateIPVSServer(key *types.VirtualService_Key, server *types.RealServer) error {
	ctx, cancel := context.WithTimeout(context.Background(), ipvsTimeout)
	defer cancel()
	return r.ipvs.UpdateServer(ctx, key, server)
}

func (r *reconciler) deleteIPVSServer(key *types.VirtualService_Key, server *types.RealServer) error {
	ctx, cancel := context.WithTimeout(context.Background(), ipvsTimeout)
	defer cancel()
	return r.ipvs.DeleteServer(ctx, key, server)
}

func (r *reconciler) listIPVSServers(key *types.VirtualService_Key) ([]*types.RealServer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ipvsTimeout)
	defer cancel()
	return r.ipvs.ListServers(ctx, key)
}
