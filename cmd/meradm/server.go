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
	weight  string
	forward string
)

func init() {
	rootCmd.AddCommand(serverCmd)
	serverCmd.AddCommand(addServerCmd)
	serverCmd.AddCommand(editServerCmd)
	serverCmd.AddCommand(deleteServerCmd)

	for _, f := range []*pflag.FlagSet{addServerCmd.Flags(), editServerCmd.Flags()} {
		f.StringVarP(&weight, "weight", "w", "", "weight of real server, used by the IPVS scheduler")
		f.StringVarP(&forward, "forward", "f", "", "forwarding method, one of [route|tunnel|masq]")
	}

	addServerCmd.MarkFlagRequired("weight")
	addServerCmd.MarkFlagRequired("forward")
}

func initServer(serviceID string, hostPort string) (*types.RealServer, error) {
	matches := ipPortRegex.FindSubmatch([]byte(hostPort))
	host := string(matches[1])
	port, err := strconv.ParseUint(string(matches[2]), 10, 16)
	if err != nil {
		return nil, fmt.Errorf("unable to parse port: %v", err)
	}

	server := &types.RealServer{
		ServiceID: serviceID,
		Key: &types.RealServer_Key{
			Ip:   host,
			Port: uint32(port),
		},
		Config: &types.RealServer_Config{},
	}

	if weight != "" {
		w, err := strconv.ParseUint(weight, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("unable to convert weight to uint32: %v", err)
		}
		server.Config.Weight = &wrappers.UInt32Value{Value: uint32(w)}
	}

	if forward != "" {
		f, ok := types.ForwardMethod_value[strings.ToUpper(forward)]
		if !ok {
			return nil, fmt.Errorf("unrecognized forward method")
		}
		server.Config.Forward = types.ForwardMethod(f)
	}

	return server, nil

}

func addServer(_ *cobra.Command, args []string) error {
	return client(func(c types.MerlinClient) error {
		server, err := initServer(args[0], args[1])
		if err != nil {
			return err
		}

		ctx, cancel := clientContext()
		defer cancel()
		_, err = c.CreateServer(ctx, server)
		return err
	})
}

func editServer(_ *cobra.Command, args []string) error {
	return client(func(c types.MerlinClient) error {
		server, err := initServer(args[0], args[1])
		if err != nil {
			return err
		}
		ctx, cancel := clientContext()
		defer cancel()
		_, err = c.UpdateServer(ctx, server)
		return err
	})
}

func deleteServer(_ *cobra.Command, args []string) error {
	return client(func(c types.MerlinClient) error {
		server, err := initServer(args[0], args[1])
		if err != nil {
			return err
		}
		ctx, cancel := clientContext()
		defer cancel()
		_, err = c.DeleteServer(ctx, server)
		return err
	})
}
