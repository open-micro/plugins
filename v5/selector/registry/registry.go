// Package registry uses the go-micro registry for selection
package registry

import (
	"go-micro.org/v5/selector"
	"go-micro.org/v5/util/cmd"
)

func init() {
	cmd.DefaultSelectors["registry"] = NewSelector
}

// NewSelector returns a new registry selector.
func NewSelector(opts ...selector.Option) selector.Selector {
	return selector.NewSelector(opts...)
}
