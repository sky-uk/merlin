package main

import (
	"fmt"
	"strings"

	"os"
	"text/tabwriter"

	"github.com/golang/protobuf/ptypes"
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

		fmt.Fprintln(w, "ID\tProt\tLocalAddress:Port\tScheduler\tFlags\t\t")
		fmt.Fprintln(w, "\t    \tHealth\tPeriod\tTimeout\tUp/Down\t")
		fmt.Fprintln(w, "\t  ->\tRemoteAddress:Port\tForward\tWeight\t\t")

		for _, item := range resp.Items {
			svc := item.Service

			fmt.Fprintf(w, "%s\t%s\t%s:%d\t%s\t(%s)\t\t\n",
				svc.Id,
				svc.Key.Protocol.String(),
				svc.Key.Ip,
				svc.Key.Port,
				svc.Config.Scheduler,
				strings.Join(svc.Config.Flags, ","))

			if svc.HealthCheck != nil {
				period, _ := ptypes.Duration(svc.HealthCheck.Period)
				timeout, _ := ptypes.Duration(svc.HealthCheck.Timeout)
				fmt.Fprintf(w, "\t    \t%s\t%v\t%v\t%d/%d\t\n",
					svc.HealthCheck.Endpoint,
					period,
					timeout,
					svc.HealthCheck.UpThreshold,
					svc.HealthCheck.DownThreshold)
			}

			for _, server := range item.Servers {
				fmt.Fprintf(w, "\t  ->\t%s:%d\t%s\t%d\t\t\n",
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
