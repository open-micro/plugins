# Registry Cache 

Cache is a library that provides a caching layer for the go-micro [registry](https://godoc.org/github.com/open-micro/go-micro/registry#Registry).

If you're looking for caching in your microservices use the [selector](https://micro.mu/docs/fault-tolerance.html#caching-discovery).

## Interface

```
// Cache is the registry cache interface
type Cache interface {
	// embed the registry interface
	registry.Registry
	// stop the cache watcher
	Stop()
}
```

## Usage

```
import (
	"go-micro.org/v5/registry"
	"go-micro.org/v5/registry/cache"
)

r := registry.NewRegistry()
cache := cache.New(r)

services, _ := cache.GetService("my.service")
```
