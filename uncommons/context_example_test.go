package uncommons_test

import (
	"context"
	"fmt"
	"time"

	"github.com/LerianStudio/lib-uncommons/uncommons"
)

func ExampleWithTimeoutSafe() {
	ctx := context.Background()

	timeoutCtx, cancel, err := uncommons.WithTimeoutSafe(ctx, 100*time.Millisecond)
	defer cancel()

	_, hasDeadline := timeoutCtx.Deadline()

	fmt.Println(err == nil)
	fmt.Println(hasDeadline)

	// Output:
	// true
	// true
}
