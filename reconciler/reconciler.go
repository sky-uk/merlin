package reconciler

import (
	"time"

	"context"

	log "github.com/Sirupsen/logrus"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/sky-uk/merlin/ipvs"
	"github.com/sky-uk/merlin/reconciler/healthchecks"
	"github.com/sky-uk/merlin/types"
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
	log.Infof("Starting reconciler")
	go func() {
		for {
			t := time.NewTimer(r.period)
			select {
			case <-t.C:
				r.reconcile()
			case <-r.syncCh:
				r.reconcile()
			case <-r.stopCh:
				log.Infof("Stopped reconciler")
				return
			}
		}
	}()
	r.Sync()
	return nil
}

func (r *reconciler) Stop() {
	close(r.stopCh)
	r.checker.Stop()
}

func (r *reconciler) Sync() {
	go func() { r.syncCh <- struct{}{} }()
}

func (r *reconciler) reconcile() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
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

		if match == nil || !proto.Equal(desiredService.HealthCheck, match.HealthCheck) {
			r.checker.SetHealthCheck(desiredService.Id, desiredService.HealthCheck)
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

		// health check new servers
		// do this before checking GetDownServers, to ensure we set weights of new servers correctly
		for _, desiredServer := range desiredServers {
			var found bool
			for _, actualServer := range actualServers {
				if proto.Equal(desiredServer.Key, actualServer.Key) {
					found = true
					break
				}
			}
			if !found {
				r.checker.AddServer(desiredService.Id, desiredServer.Key.Ip)
			}
		}

		// set weight to 0 for any down servers
		downIPs := r.checker.GetDownServers(desiredService.Id)
		for _, ip := range downIPs {
			for _, desiredServer := range desiredServers {
				if desiredServer.Key.Ip == ip {
					desiredServer.Config.Weight = &wrappers.UInt32Value{Value: 0}
				}
			}
		}

		for _, desiredServer := range desiredServers {
			var match *types.RealServer
			for _, actualServer := range actualServers {
				if proto.Equal(desiredServer.Key, actualServer.Key) {
					match = actualServer
					break
				}
			}
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
				r.checker.RemServer(desiredService.Id, actualServer.Key.Ip)
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
			// remove health checks
			r.checker.SetHealthCheck(actual.Id, nil)
			// remove from ipvs
			if err := r.ipvs.DeleteService(actual.Key); err != nil {
				log.Errorf("Unable to delete service: %v", err)
			}
		}
	}
}
