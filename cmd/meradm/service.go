package main

import (
	"errors"

	"strconv"

	"fmt"
	"strings"

	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/sky-uk/merlin/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var serviceCmd = &cobra.Command{
	Use:   "service [add|edit|del]",
	Short: "Modify a virtual service",
}

func validIDProtocolIPPort(_ *cobra.Command, args []string) error {
	if len(args) != 3 {
		return errors.New("requires three arguments")
	}
	b := []byte(args[2])
	if !ipPortRegex.Match(b) {
		return errors.New("must be ip:port")
	}
	return nil
}

var addServiceCmd = &cobra.Command{
	Use:   "add [id] [protocol] [ip:port]",
	Short: "Add a virtual service",
	Args:  validIDProtocolIPPort,
	RunE:  addService,
}

var editServiceCmd = &cobra.Command{
	Use:   "edit [id]",
	Short: "Edit a virtual service",
	Args:  cobra.ExactArgs(1),
	RunE:  editService,
}

var deleteServiceCmd = &cobra.Command{
	Use:   "del [id]",
	Short: "Delete a virtual service",
	Args:  cobra.ExactArgs(1),
	RunE:  deleteService,
}

var (
	scheduler           string
	schedFlags          []string
	healthEndpoint      string
	healthPeriod        time.Duration
	healthTimeout       time.Duration
	healthUpThreshold   uint16
	healthDownThreshold uint16
)

func init() {
	rootCmd.AddCommand(serviceCmd)
	serviceCmd.AddCommand(addServiceCmd)
	serviceCmd.AddCommand(editServiceCmd)
	serviceCmd.AddCommand(deleteServiceCmd)

	for _, f := range []*pflag.FlagSet{addServiceCmd.Flags(), editServiceCmd.Flags()} {
		f.StringVarP(&scheduler, "scheduler", "s", "", "scheduler for new connections")
		f.StringSliceVarP(&schedFlags, "sched-flags", "b", nil, "scheduler flags")
		f.StringVar(&healthEndpoint, "health-endpoint", "", "Endpoint for health checks. "+
			"If unset, no health check occurs and the server is assumed to always be up.")
		f.DurationVar(&healthPeriod, "health-period", 10*time.Second, "Time period between health checks.")
		f.DurationVar(&healthTimeout, "health-timeout", time.Second, "Timeout for health checks.")
		f.Uint16Var(&healthUpThreshold, "health-up", 3,
			"Threshold of successful health checks before marking a server as up.")
		f.Uint16Var(&healthDownThreshold, "health-down", 2,
			"Threshold of failed health checks before marking a server as down.")
	}

	addServiceCmd.MarkFlagRequired("scheduler")
}

func serviceFromFlags(id string) *types.VirtualService {
	svc := &types.VirtualService{
		Id: id,
		Config: &types.VirtualService_Config{
			Scheduler: scheduler,
			Flags:     schedFlags,
		},
	}

	if healthEndpoint != "" || healthPeriod != 0 || healthTimeout != 0 ||
		healthUpThreshold != 0 || healthDownThreshold != 0 {

		svc.HealthCheck = &types.VirtualService_HealthCheck{}
		if healthEndpoint != "" {
			svc.HealthCheck.Endpoint = healthEndpoint
		}
		if healthPeriod != 0 {
			svc.HealthCheck.Period = ptypes.DurationProto(healthPeriod)
		}
		if healthTimeout != 0 {
			svc.HealthCheck.Timeout = ptypes.DurationProto(healthTimeout)
		}
		if healthUpThreshold != 0 {
			svc.HealthCheck.UpThreshold = uint32(healthUpThreshold)
		}
		if healthDownThreshold != 0 {
			svc.HealthCheck.DownThreshold = uint32(healthDownThreshold)
		}
	}

	return svc
}

func addService(_ *cobra.Command, args []string) error {
	return client(func(c types.MerlinClient) error {
		svc := serviceFromFlags(args[0])

		proto, ok := types.Protocol_value[strings.ToUpper(args[1])]
		if !ok {
			return errors.New("unrecognized protocol")
		}
		matches := ipPortRegex.FindSubmatch([]byte(args[2]))
		ip := string(matches[1])
		port, err := strconv.ParseUint(string(matches[2]), 10, 16)
		if err != nil {
			return fmt.Errorf("unable to parse port: %v", err)
		}

		svc.Key = &types.VirtualService_Key{
			Protocol: types.Protocol(proto),
			Ip:       ip,
			Port:     uint32(port),
		}

		ctx, cancel := clientContext()
		defer cancel()
		_, err = c.CreateService(ctx, svc)
		return err
	})
}

func editService(_ *cobra.Command, args []string) error {
	return client(func(c types.MerlinClient) error {
		svc := serviceFromFlags(args[0])
		ctx, cancel := clientContext()
		defer cancel()
		_, err := c.UpdateService(ctx, svc)
		return err
	})
}

func deleteService(_ *cobra.Command, args []string) error {
	return client(func(c types.MerlinClient) error {
		id := wrappers.StringValue{Value: args[0]}
		ctx, cancel := clientContext()
		defer cancel()
		_, err := c.DeleteService(ctx, &id)
		return err
	})
}
