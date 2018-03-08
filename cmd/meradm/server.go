package main

import (
	"errors"

	"strconv"

	"fmt"

	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/sky-uk/merlin/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var serverCmd = &cobra.Command{
	Use:   "server [add|edit|del]",
	Short: "Modify a real server",
}

func validServiceIDAndIP(_ *cobra.Command, args []string) error {
	if len(args) != 2 {
		return errors.New("requires two arguments")
	}
	b := []byte(args[1])
	if !ipRegex.Match(b) {
		return errors.New("must be a valid ip")
	}
	return nil
}

var addServerCmd = &cobra.Command{
	Use:   "add [serviceID] [ip]",
	Short: "Add a real server",
	Args:  validServiceIDAndIP,
	RunE:  addServer,
}

var editServerCmd = &cobra.Command{
	Use:   "edit [serviceID] [ip]",
	Short: "Edit a real server",
	Args:  validServiceIDAndIP,
	RunE:  editServer,
}

var deleteServerCmd = &cobra.Command{
	Use:   "del [serviceID] [ip]",
	Short: "Delete a real server",
	Args:  validServiceIDAndIP,
	RunE:  deleteServer,
}

var (
	weight string
)

func init() {
	rootCmd.AddCommand(serverCmd)
	serverCmd.AddCommand(addServerCmd)
	serverCmd.AddCommand(editServerCmd)
	serverCmd.AddCommand(deleteServerCmd)

	for _, f := range []*pflag.FlagSet{addServerCmd.Flags(), editServerCmd.Flags()} {
		f.StringVarP(&weight, "weight", "w", "", "weight of the real server")
	}

	addServerCmd.MarkFlagRequired("weight")
}

func initServer(serviceID string, ip string) (*types.RealServer, error) {
	server := &types.RealServer{
		ServiceID: serviceID,
		Key: &types.RealServer_Key{
			Ip: ip,
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
