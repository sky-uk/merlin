package main

import (
	"context"

	"fmt"

	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/merlin/types"
	"google.golang.org/grpc"
)

func clientContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

func client(fn func(client types.MerlinClient) error) error {
	dest := fmt.Sprintf("%s:%d", host, port)
	log.Debugf("Dialing %s", dest)
	conn, err := grpc.Dial(dest, grpc.WithInsecure())
	if err != nil {
		return err
	}
	defer conn.Close()
	c := types.NewMerlinClient(conn)

	return fn(c)
}
