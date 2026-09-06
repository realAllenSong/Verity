package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/realAllenSong/Verity/apps/api/internal/verity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	address := env("VERITY_TEMPORAL_ADDRESS", client.DefaultHostPort)
	namespace := env("VERITY_TEMPORAL_NAMESPACE", client.DefaultNamespace)
	taskQueue := env("VERITY_TEMPORAL_TASK_QUEUE", verity.DefaultTemporalTaskQueue)
	temporalClient, err := client.Dial(client.Options{HostPort: address, Namespace: namespace})
	check(err)
	defer temporalClient.Close()
	check(verity.RunTemporalWorker(temporalClient, taskQueue, worker.InterruptCh()))
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
