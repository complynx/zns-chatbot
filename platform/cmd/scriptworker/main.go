// scriptworker is a one-request data-only JavaScript helper. It must be launched
// in a networkless, memory-limited container. It has no HTTP listener or secrets.
package main

import (
	"context"
	"os"
	"runtime/debug"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

func main() {
	os.Clearenv()
	const softMemoryLimit = 64 * 1024 * 1024
	debug.SetMemoryLimit(softMemoryLimit)
	const rpcArgumentCount = 2
	rpc := len(os.Args) == rpcArgumentCount && os.Args[1] == "rpc"
	if len(os.Args) > 1 && !rpc {
		os.Exit(1)
	}
	runWorker(rpc)
}

func runWorker(rpc bool) {
	const timeoutExit = 124
	timeout := scriptworker.ProcessTimeout
	if rpc {
		timeout = scriptworker.ExecuteProcessTimeout
	}
	watchdog := time.AfterFunc(timeout, func() { os.Exit(timeoutExit) })
	var err error
	if rpc {
		err = scriptworker.ServeRPC(context.Background(), os.Stdin, os.Stdout)
	} else {
		err = scriptworker.Serve(context.Background(), os.Stdin, os.Stdout)
	}
	watchdog.Stop()
	if err != nil {
		os.Exit(1)
	}
}
