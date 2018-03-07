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
	forwardMethod       string
	forwardPort         uint32
)

func init() {
	rootCmd.AddCommand(serviceCmd)
	serviceCmd.AddCommand(addServiceCmd)
	serviceCmd.AddCommand(editServiceCmd)
	serviceCmd.AddCommand(deleteServiceCmd)

	for _, f := range []*pflag.FlagSet{addServiceCmd.Flags(), editServiceCmd.Flags()} {
		f.StringVarP(&scheduler, "scheduler", "s", "", "scheduler for new connections")
		f.StringSliceVarP(&schedFlags, "sched-flags", "b", nil, "scheduler flags")
		f.StringVarP(&forwardMethod, "forward-method", "f", "", "forward method, one of [route|tunnel|masq]")
		f.Uint32VarP(&forwardPort, "forward-port", "p", 0, "forward port for real servers")
		f.StringVar(&healthEndpoint, "health-endpoint", "", "endpoint for health checks, set to empty to clear")
		f.DurationVar(&healthPeriod, "health-period", 0, "time period between health checks")
		f.DurationVar(&healthTimeout, "health-timeout", 0, "timeout for health checks")
		f.Uint16Var(&healthUpThreshold, "health-up", 0, "threshold of successful health checks")
		f.Uint16Var(&healthDownThreshold, "health-down", 0, "threshold of failed health checks")
	}

	addServiceCmd.MarkFlagRequired("scheduler")
	addServiceCmd.MarkFlagRequired("forward-method")
	addServiceCmd.MarkFlagRequired("forward-port")
}

func serviceFromFlags(cmd *cobra.Command, id string) (*types.VirtualService, error) {
	svc := &types.VirtualService{
		Id: id,
		Config: &types.VirtualService_Config{
			Scheduler: scheduler,
			Flags:     schedFlags,
		},
		RealServerConfig: &types.VirtualService_RealServerConfig{
			ForwardPort: forwardPort,
		},
		HealthCheck: &types.VirtualService_HealthCheck{},
	}

	if forwardMethod != "" {
		f, ok := types.ForwardMethod_value[strings.ToUpper(forwardMethod)]
		if !ok {
			return nil, fmt.Errorf("unrecognized forward method")
		}
		svc.RealServerConfig.ForwardMethod = types.ForwardMethod(f)
	}

	if cmd.Flag("health-endpoint").Changed {
		svc.HealthCheck.Endpoint = &wrappers.StringValue{Value: healthEndpoint}
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

	return svc, nil
}

func addService(cmd *cobra.Command, args []string) error {
	return client(func(c types.MerlinClient) error {
		svc, err := serviceFromFlags(cmd, args[0])
		if err != nil {
			return err
		}

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

func editService(cmd *cobra.Command, args []string) error {
	return client(func(c types.MerlinClient) error {
		svc, err := serviceFromFlags(cmd, args[0])
		if err != nil {
			return err
		}
		ctx, cancel := clientContext()
		defer cancel()
		_, err = c.UpdateService(ctx, svc)
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
