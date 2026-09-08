# Verity CLI

The Go CLI automates the same REST lifecycle as the browser. It never reads an entire source file into memory and never bypasses the API's privacy, review, or publication rules.

## Build

```bash
cd apps/api
go build -o ../../bin/verity ./cmd/verity
```

Set the server once if it is not local:

```bash
export VERITY_SERVER=http://127.0.0.1:8000
export VERITY_API_TOKEN=your-token
```

The token comes from the environment so it does not need to appear in shell history.

## Import and wait

```bash
./bin/verity import ./sample_data/examples/noisy-workflow-events.csv \
  --wait \
  --output ./ready.parquet
```

This single command creates a resumable import, uploads in bounded chunks, starts the default workflow automatically, waits for a terminal state, downloads the published Parquet result, and checks the server-provided SHA-256 digest.

Use `--json` for automation. Progress stays on stderr and the final machine-readable result is the only value written to stdout.

## Other commands

```bash
./bin/verity status JOB_ID --wait --json
./bin/verity inspect privacy --limit 50 --json
./bin/verity review --json
./bin/verity review RECORD_ID --decision accepted --reason "Verified against source evidence"
./bin/verity export OUTPUT_ID --output ./result.parquet --json
```

Stage inspection is cursor-based and capped at 200 rows per call. Review mutations require both an explicit decision and a reason.
