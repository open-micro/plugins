// Package mucp provides an mucp client
package mucp

import (
	"go-micro.org/v5/client"
	"go-micro.org/v5/util/cmd"
)

func init() {
	cmd.DefaultClients["mucp"] = NewClient
}

// NewClient returns a new micro client interface.
func NewClient(opts ...client.Option) client.Client {
	return client.NewClient(opts...)
}
