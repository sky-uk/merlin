package main

import (
	"fmt"
	"strings"

	"os"
	"text/tabwriter"

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

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)

		fmt.Fprintln(w, "ID\tProt\tLocalAddress:Port\tScheduler\tFlags")
		fmt.Fprintln(w, "\t  ->\tRemoteAddress:Port\tForward\tWeight")

		for _, item := range resp.Items {
			fmt.Fprintf(w, "%s\t%s\t%s:%d\t%s\t(%s)\n",
				item.Service.Id,
				item.Service.Key.Protocol.String(),
				item.Service.Key.Ip,
				item.Service.Key.Port,
				item.Service.Config.Scheduler,
				strings.Join(item.Service.Config.Flags, ","))

			for _, server := range item.Servers {
				fmt.Fprintf(w, "\t  ->\t%s:%d\t%s\t%d\n",
					server.Key.GetIp(),
					server.Key.GetPort(),
					server.Config.GetForward(),
					server.Config.GetWeight().GetValue())
			}
		}

		w.Flush()
		return nil
	})
}
