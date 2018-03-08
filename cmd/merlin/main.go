package main

import (
	"flag"

	_ "net/http/pprof"

	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"strings"
	"time"

	"context"

	"github.com/mqliang/libipvs"
	"github.com/onrik/logrus/filename"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/merlin/ipvs"
	"github.com/sky-uk/merlin/reconciler"
	"github.com/sky-uk/merlin/server"
	"github.com/sky-uk/merlin/store"
	"github.com/sky-uk/merlin/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	debug               bool
	port                int
	healthPort          int
	storeEndpoints      string
	storePrefix         string
	reconcileSyncPeriod time.Duration
	reconcile           bool
)

func init() {
	flag.BoolVar(&debug, "debug", false, "enable debug logs")
	flag.IntVar(&port, "port", 4282, "server port")
	flag.IntVar(&healthPort, "health-port", 4283, "/health, /alive, /metrics, and /debug endpoints")
	flag.StringVar(&storeEndpoints, "store-endpoints", "", "comma delimited list of etcd2 endpoints")
	flag.StringVar(&storePrefix, "store-prefix", "/merlin", "prefix to store state")
	flag.DurationVar(&reconcileSyncPeriod, "reconcile-sync-period", time.Minute, "how often to periodically sync ipvs state")
	flag.BoolVar(&reconcile, "reconcile", true, "if enabled, merlin will reconcile local ipvs with store state")
}

func main() {
	flag.Parse()

	if debug {
		log.SetLevel(log.DebugLevel)
		log.Debug("Debug logs on")
	}
	filenameHook := filename.NewHook()
	filenameHook.Field = "source"
	log.AddHook(filenameHook)

	srv := &srv{}
	srv.Start()
	addSignalHandler(srv)
	addHealthPort(srv)
	select {}
}

type srv struct {
	grpcServer      *grpc.Server
	reconciler      reconciler.Reconciler
	subscribeStopCh chan struct{}
}

func (s *srv) Health() error {
	return nil
}

func (s *srv) Start() {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	log.Infof("Starting merlin")

	etcdStore, err := store.NewEtcd2(strings.Split(storeEndpoints, ","), storePrefix)
	if err != nil {
		log.Fatalf("Unable to start store client: %v", err)
	}

	if reconcile {
		h, err := libipvs.New()
		if err != nil {
			log.Fatalf("Unable to init IPVS: %v", err)
		}
		ipvs := ipvs.New(h)

		s.reconciler = reconciler.New(reconcileSyncPeriod, etcdStore, ipvs)
	} else {
		s.reconciler = reconciler.NewStub()
	}

	if err := s.reconciler.Start(); err != nil {
		log.Fatalf("Unable to start reconciler: %v", err)
	}
	s.reconciler.Sync()

	s.subscribeStopCh = make(chan struct{})
	etcdStore.Subscribe(func() {
		log.Info("Store updated, starting sync")
		s.reconciler.Sync()
	}, s.subscribeStopCh)

	server := server.New(etcdStore)

	s.grpcServer = grpc.NewServer(
		grpc.UnaryInterceptor(logRequests),
	)
	types.RegisterMerlinServer(s.grpcServer, server)
	go func() {
		if err := s.grpcServer.Serve(lis); err != nil {
			log.Error(err)
		}
	}()
}

func (s *srv) Stop() error {
	close(s.subscribeStopCh)
	s.reconciler.Stop()
	s.grpcServer.GracefulStop()
	log.Infof("Stopped merlin")
	return nil
}

func logRequests(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	resp, err := handler(ctx, req)
	// catch any internal errors and wrap in the correct status code
	if _, ok := status.FromError(err); !ok {
		log.Error(err)
		err = status.Errorf(codes.Internal, "%v", err)
	}
	return resp, err
}

func addSignalHandler(s *srv) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for sig := range c {
			log.Infof("Received %v signal, shutting down...", sig)
			err := s.Stop()
			if err != nil {
				log.Errorf("Error while stopping: %v", err)
				os.Exit(-1)
			}
			os.Exit(0)
		}
	}()
}

func addHealthPort(s *srv) {
	http.HandleFunc("/health", healthHandler(s))
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/alive", okHandler)

	go func() {
		if err := http.ListenAndServe(":"+strconv.Itoa(healthPort), nil); err != nil {
			log.Error(err)
		}
	}()
}

func healthHandler(s *srv) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.Health(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, fmt.Sprintf("%v\n", err))
			return
		}

		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok\n")
	}
}

func okHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "ok\n")
}
