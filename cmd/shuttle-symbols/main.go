// Shuttle-symbols retains the standalone descriptor operator entry point.
package main

import (
	"fmt"
	"os"

	"github.com/complynx/zns-chatbot/internal/descriptor"
)

func main() {
	if err := descriptor.Command(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
