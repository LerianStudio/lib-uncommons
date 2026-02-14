//go:build unit

package uncommons_test

import (
	"context"
	"fmt"
	"time"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons"
)

func ExampleWithTimeoutSafe() {
	ctx := context.Background()

	timeoutCtx, cancel, err := uncommons.WithTimeoutSafe(ctx, 100*time.Millisecond)
	if cancel != nil {
		defer cancel()
	}

	_, hasDeadline := timeoutCtx.Deadline()

	fmt.Println(err == nil)
	fmt.Println(hasDeadline)

	// Output:
	// true
	// true
}
