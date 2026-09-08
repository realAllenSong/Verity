package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/realAllenSong/Verity/apps/api/internal/client"
)

const usage = `Verity automation CLI

Usage:
  verity import <file> [--server URL] [--wait] [--output FILE] [--json]
  verity status <job-id> [--server URL] [--wait] [--json]
  verity inspect <stage-id> [--cursor CURSOR] [--limit 100] [--json]
  verity review [record-id] [--decision accepted|modified|rejected] [--reason TEXT] [--json]
  verity export <output-id> --output FILE [--json]

Environment:
  VERITY_SERVER      API base URL (default http://127.0.0.1:8000)
  VERITY_API_TOKEN   Bearer token; never needs to appear in shell history
`

type options struct {
	server   string
	json     bool
	wait     bool
	output   string
	cursor   string
	limit    int
	decision string
	reason   string
	args     []string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

func run(arguments []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(arguments) == 0 || arguments[0] == "help" || arguments[0] == "--help" || arguments[0] == "-h" {
		fmt.Fprint(stderr, usage)
		if len(arguments) == 0 {
			return 2
		}
		return 0
	}
	command := arguments[0]
	parsed, err := parseOptions(arguments[1:], getenv)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n\n%s", err, usage)
		return 2
	}
	api, err := client.New(parsed.server, getenv("VERITY_API_TOKEN"), nil)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	switch command {
	case "import":
		if len(parsed.args) != 1 {
			return usageError(stderr, "import requires one file path")
		}
		if !parsed.json {
			fmt.Fprintf(stderr, "Uploading %s\n", parsed.args[0])
		}
		job, err := api.ImportFile(ctx, parsed.args[0])
		if err != nil {
			return commandError(stderr, err)
		}
		if parsed.wait || parsed.output != "" {
			if !parsed.json {
				fmt.Fprintf(stderr, "Processing %s\n", job.ID)
			}
			job, err = api.WaitJob(ctx, job.ID, 350*time.Millisecond)
			if err != nil {
				return commandError(stderr, err)
			}
		}
		result := map[string]any{"job": job}
		if parsed.output != "" {
			if job.OutputID == "" {
				return commandError(stderr, errors.New("job has no downloadable output yet"))
			}
			download, err := api.DownloadOutput(ctx, job.OutputID, parsed.output)
			if err != nil {
				return commandError(stderr, err)
			}
			result["download"] = download
		}
		return writeResult(stdout, result, parsed.json, fmt.Sprintf("Job %s: %s", job.ID, job.State))

	case "status":
		if len(parsed.args) != 1 {
			return usageError(stderr, "status requires one job ID")
		}
		job, err := api.Job(ctx, parsed.args[0])
		if err == nil && parsed.wait {
			job, err = api.WaitJob(ctx, job.ID, 350*time.Millisecond)
		}
		if err != nil {
			return commandError(stderr, err)
		}
		return writeResult(stdout, job, parsed.json, fmt.Sprintf("%s\t%s", job.ID, job.State))

	case "inspect":
		if len(parsed.args) != 1 {
			return usageError(stderr, "inspect requires one stage ID")
		}
		page, err := api.StageRecords(ctx, parsed.args[0], parsed.cursor, parsed.limit)
		if err != nil {
			return commandError(stderr, err)
		}
		return writeResult(stdout, page, parsed.json, fmt.Sprintf("%s: %d records", page.StageID, page.Returned))

	case "review":
		if len(parsed.args) == 0 {
			queue, err := api.Reviews(ctx)
			if err != nil {
				return commandError(stderr, err)
			}
			return writeResult(stdout, queue, parsed.json, fmt.Sprintf("%d records need review", queue.Count))
		}
		if len(parsed.args) != 1 || parsed.decision == "" || strings.TrimSpace(parsed.reason) == "" {
			return usageError(stderr, "submitting review requires record ID, --decision, and --reason")
		}
		workspace, err := api.SubmitReview(ctx, parsed.args[0], parsed.decision, parsed.reason)
		if err != nil {
			return commandError(stderr, err)
		}
		return writeResult(stdout, map[string]any{"record_id": parsed.args[0], "decision": parsed.decision, "workspace": workspace}, parsed.json, "Review saved")

	case "export":
		if len(parsed.args) != 1 || parsed.output == "" {
			return usageError(stderr, "export requires one output ID and --output")
		}
		result, err := api.DownloadOutput(ctx, parsed.args[0], parsed.output)
		if err != nil {
			return commandError(stderr, err)
		}
		return writeResult(stdout, result, parsed.json, fmt.Sprintf("Saved %s (%s)", result.Path, result.SHA256))
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", command, usage)
		return 2
	}
}

func parseOptions(arguments []string, getenv func(string) string) (options, error) {
	result := options{server: strings.TrimSpace(getenv("VERITY_SERVER")), limit: 100}
	if result.server == "" {
		result.server = "http://127.0.0.1:8000"
	}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--json":
			result.json = true
		case "--wait":
			result.wait = true
		case "--server", "--output", "--cursor", "--limit", "--decision", "--reason":
			if index+1 >= len(arguments) {
				return options{}, fmt.Errorf("%s requires a value", argument)
			}
			index++
			value := arguments[index]
			switch argument {
			case "--server":
				result.server = value
			case "--output":
				result.output = value
			case "--cursor":
				result.cursor = value
			case "--limit":
				limit, err := strconv.Atoi(value)
				if err != nil || limit < 1 || limit > 200 {
					return options{}, errors.New("--limit must be between 1 and 200")
				}
				result.limit = limit
			case "--decision":
				result.decision = value
			case "--reason":
				result.reason = value
			}
		default:
			if strings.HasPrefix(argument, "-") {
				return options{}, fmt.Errorf("unknown option %q", argument)
			}
			result.args = append(result.args, argument)
		}
	}
	return result, nil
}

func writeResult(stdout io.Writer, value any, structured bool, human string) int {
	if structured {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(value); err != nil {
			return 1
		}
		return 0
	}
	fmt.Fprintln(stdout, human)
	return 0
}

func commandError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "error: %v\n", err)
	return 1
}

func usageError(stderr io.Writer, message string) int {
	fmt.Fprintf(stderr, "error: %s\n\n%s", message, usage)
	return 2
}
