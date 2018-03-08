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

var serverCmd = &cobra.Command{
	Use:   "server [add|edit|del]",
	Short: "Modify a real server",
}

func validServiceIDIPPort(_ *cobra.Command, args []string) error {
	if len(args) != 2 {
		return errors.New("requires two arguments")
	}
	b := []byte(args[1])
	if !ipPortRegex.Match(b) {
		return errors.New("must be ip:port")
	}
	return nil
}

var addServerCmd = &cobra.Command{
	Use:   "add [serviceID] [ip:port]",
	Short: "Add a real server",
	Args:  validServiceIDIPPort,
	RunE:  addServer,
}

var editServerCmd = &cobra.Command{
	Use:   "edit [serviceID] [ip:port]",
	Short: "Edit a real server",
	Args:  validServiceIDIPPort,
	RunE:  editServer,
}

var deleteServerCmd = &cobra.Command{
	Use:   "del [serviceID] [ip:port]",
	Short: "Delete a real server",
	Args:  validServiceIDIPPort,
	RunE:  deleteServer,
}

var (
	weight              string
	forwardMethod       string
	healthEndpoint      string
	healthPeriod        time.Duration
	healthTimeout       time.Duration
	healthUpThreshold   uint16
	healthDownThreshold uint16
)

func init() {
	rootCmd.AddCommand(serverCmd)
	serverCmd.AddCommand(addServerCmd)
	serverCmd.AddCommand(editServerCmd)
	serverCmd.AddCommand(deleteServerCmd)

	for _, f := range []*pflag.FlagSet{addServerCmd.Flags(), editServerCmd.Flags()} {
		f.StringVarP(&weight, "weight", "w", "", "weight of the real server")
		f.StringVarP(&forwardMethod, "forward-method", "f", "", "one of [route|tunnel|masq]")
		f.StringVar(&healthEndpoint, "health-endpoint", "",
			"endpoint for health checks, should be a valid URL 'http://:8080/health' or empty to disable")
		f.DurationVar(&healthPeriod, "health-period", 0, "time period between health checks")
		f.DurationVar(&healthTimeout, "health-timeout", 0, "timeout for health checks")
		f.Uint16Var(&healthUpThreshold, "health-up", 0, "threshold of successful health checks")
		f.Uint16Var(&healthDownThreshold, "health-down", 0, "Threshold of failed health checks")
	}

	addServerCmd.MarkFlagRequired("weight")
	addServerCmd.MarkFlagRequired("forward")
}

func initServer(cmd *cobra.Command, serviceID string, ipPort string) (*types.RealServer, error) {
	matches := ipPortRegex.FindSubmatch([]byte(ipPort))
	ip := string(matches[1])
	port, err := strconv.ParseUint(string(matches[2]), 10, 16)
	if err != nil {
		return nil, fmt.Errorf("unable to parse port: %v", err)
	}

	server := &types.RealServer{
		ServiceID: serviceID,
		Key: &types.RealServer_Key{
			Ip:   ip,
			Port: uint32(port),
		},
		Config:      &types.RealServer_Config{},
		HealthCheck: &types.RealServer_HealthCheck{},
	}

	if weight != "" {
		w, err := strconv.ParseUint(weight, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("unable to convert weight to uint32: %v", err)
		}
		server.Config.Weight = &wrappers.UInt32Value{Value: uint32(w)}
	}

	if forwardMethod != "" {
		f, ok := types.ForwardMethod_value[strings.ToUpper(forwardMethod)]
		if !ok {
			return nil, fmt.Errorf("unrecognized forward method")
		}
		server.Config.Forward = types.ForwardMethod(f)
	}

	endpointFlag := cmd.Flag("health-endpoint")
	if endpointFlag != nil && endpointFlag.Changed {
		server.HealthCheck.Endpoint = &wrappers.StringValue{Value: healthEndpoint}
	}
	if healthPeriod != 0 {
		server.HealthCheck.Period = ptypes.DurationProto(healthPeriod)
	}
	if healthTimeout != 0 {
		server.HealthCheck.Timeout = ptypes.DurationProto(healthTimeout)
	}
	if healthUpThreshold != 0 {
		server.HealthCheck.UpThreshold = uint32(healthUpThreshold)
	}
	if healthDownThreshold != 0 {
		server.HealthCheck.DownThreshold = uint32(healthDownThreshold)
	}

	return server, nil
}

func addServer(cmd *cobra.Command, args []string) error {
	return client(func(c types.MerlinClient) error {
		server, err := initServer(cmd, args[0], args[1])
		if err != nil {
			return err
		}

		ctx, cancel := clientContext()
		defer cancel()
		_, err = c.CreateServer(ctx, server)
		return err
	})
}

func editServer(cmd *cobra.Command, args []string) error {
	return client(func(c types.MerlinClient) error {
		server, err := initServer(cmd, args[0], args[1])
		if err != nil {
			return err
		}
		ctx, cancel := clientContext()
		defer cancel()
		_, err = c.UpdateServer(ctx, server)
		return err
	})
}

func deleteServer(cmd *cobra.Command, args []string) error {
	return client(func(c types.MerlinClient) error {
		server, err := initServer(cmd, args[0], args[1])
		if err != nil {
			return err
		}
		ctx, cancel := clientContext()
		defer cancel()
		_, err = c.DeleteServer(ctx, server)
		return err
	})
}
