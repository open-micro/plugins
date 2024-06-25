// Package mdns provides a multicast dns registry
package mdns

import (
	"go-micro.org/v5/registry"
	"go-micro.org/v5/util/cmd"
)

func init() {
	cmd.DefaultRegistries["mdns"] = NewRegistry
}

// NewRegistry returns a new mdns registry.
func NewRegistry(opts ...registry.Option) registry.Registry {
	return registry.NewRegistry(opts...)
}
