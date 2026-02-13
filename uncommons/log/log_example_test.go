package log_test

import (
	"fmt"

	ulog "github.com/LerianStudio/lib-uncommons/uncommons/log"
)

func ExampleParseLevel() {
	level, err := ulog.ParseLevel("warning")

	fmt.Println(err == nil)
	fmt.Println(level.String())

	// Output:
	// true
	// warn
}
