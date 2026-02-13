package safe_test

import (
	"errors"
	"fmt"

	"github.com/LerianStudio/lib-uncommons/uncommons/safe"
)

func ExampleCompile_errorHandling() {
	_, err := safe.Compile("[")

	fmt.Println(errors.Is(err, safe.ErrInvalidRegex))

	// Output:
	// true
}
