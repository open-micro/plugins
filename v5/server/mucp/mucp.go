// Package mucp provides an mucp server
package mucp

import (
	"go-micro.org/v5/server"
	"go-micro.org/v5/util/cmd"
)

func init() {
	cmd.DefaultServers["mucp"] = NewServer
}

// NewServer returns a micro server interface.
func NewServer(opts ...server.Option) server.Server {
	return server.NewServer(opts...)
}
