package main

import (
	"fmt"
	"strings"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/sky-uk/merlin/types"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List IPVS services and servers",
	RunE:  list,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func list(_ *cobra.Command, _ []string) error {
	return client(func(c types.MerlinClient) error {
		ctx, cancel := clientContext()
		defer cancel()
		resp, err := c.List(ctx, &empty.Empty{})
		if err != nil {
			return err
		}

		for _, item := range resp.Items {
			fmt.Printf("%s %s %s:%d %s (%s)\n",
				item.Service.Id, item.Service.Key.Protocol.String(), item.Service.Key.Ip, item.Service.Key.Port,
				item.Service.Config.Scheduler, strings.Join(item.Service.Config.Flags, ","))
		}

		return nil
	})
}
