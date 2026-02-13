package server_test

import (
	"errors"
	"fmt"

	"github.com/LerianStudio/lib-uncommons/uncommons/server"
)

func ExampleServerManager_StartWithGracefulShutdownWithError_validation() {
	sm := server.NewServerManager(nil, nil, nil)
	err := sm.StartWithGracefulShutdownWithError()

	fmt.Println(errors.Is(err, server.ErrNoServersConfigured))

	// Output:
	// true
}
