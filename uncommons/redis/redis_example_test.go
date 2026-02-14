//go:build unit

package redis_test

import (
	"fmt"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons/redis"
)

func ExampleConfig() {
	cfg := redis.Config{
		Topology: redis.Topology{
			Standalone: &redis.StandaloneTopology{Address: "redis.internal:6379"},
		},
		Auth: redis.Auth{
			StaticPassword: &redis.StaticPasswordAuth{Password: "redacted"},
		},
	}

	fmt.Println(cfg.Topology.Standalone.Address)
	fmt.Println(cfg.Auth.StaticPassword != nil)

	// Output:
	// redis.internal:6379
	// true
}
