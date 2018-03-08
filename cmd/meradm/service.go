package main

import (
	"errors"

	"strconv"

	"fmt"
	"strings"

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
	scheduler      string
	schedulerFlags []string
)

func init() {
	rootCmd.AddCommand(serviceCmd)
	serviceCmd.AddCommand(addServiceCmd)
	serviceCmd.AddCommand(editServiceCmd)
	serviceCmd.AddCommand(deleteServiceCmd)

	for _, f := range []*pflag.FlagSet{addServiceCmd.Flags(), editServiceCmd.Flags()} {
		f.StringVarP(&scheduler, "scheduler", "s", "", "scheduler for new connections")
		f.StringSliceVarP(&schedulerFlags, "scheduler-flags", "b", nil, "scheduler flags")
	}

	addServiceCmd.MarkFlagRequired("scheduler")
}

func serviceFromFlags(cmd *cobra.Command, id string) *types.VirtualService {
	svc := &types.VirtualService{
		Id: id,
		Config: &types.VirtualService_Config{
			Scheduler: scheduler,
			Flags:     schedulerFlags,
		},
	}

	return svc
}

func addService(cmd *cobra.Command, args []string) error {
	return client(func(c types.MerlinClient) error {
		svc := serviceFromFlags(cmd, args[0])

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
		svc := serviceFromFlags(cmd, args[0])
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
