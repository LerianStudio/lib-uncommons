//go:build unit

package circuitbreaker_test

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons/circuitbreaker"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
)

func ExampleManager_Execute_fallbackOnOpen() {
	mgr, err := circuitbreaker.NewManager(&log.NopLogger{})
	if err != nil {
		return
	}

	_, err = mgr.GetOrCreate("core-ledger", circuitbreaker.Config{
		MaxRequests:         1,
		Interval:            time.Minute,
		Timeout:             time.Second,
		ConsecutiveFailures: 1,
		FailureRatio:        0.0,
		MinRequests:         1,
	})
	if err != nil {
		return
	}

	_, firstErr := mgr.Execute("core-ledger", func() (any, error) {
		return nil, errors.New("upstream timeout")
	})

	_, secondErr := mgr.Execute("core-ledger", func() (any, error) {
		return "ok", nil
	})

	fallback := "primary"
	if secondErr != nil {
		fallback = "cached-response"
	}

	fmt.Println(firstErr != nil)
	fmt.Println(mgr.GetState("core-ledger") == circuitbreaker.StateOpen)
	fmt.Println(strings.Contains(secondErr.Error(), "currently unavailable"))
	fmt.Println(fallback)

	// Output:
	// true
	// true
	// true
	// cached-response
}
