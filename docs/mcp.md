# Verity MCP server

`verity-mcp` is a thin stdio server built with the official Model Context Protocol Go SDK. It calls Verity through the same REST client as the CLI; it does not import the data-plane package or create a second execution path.

## Build and configure

```bash
cd apps/api
go build -o ../../bin/verity-mcp ./cmd/verity-mcp
```

Configure any MCP-capable local agent to launch `/absolute/path/to/bin/verity-mcp` and provide these environment variables:

```text
VERITY_SERVER=http://127.0.0.1:8000
VERITY_API_TOKEN=your-token
```

The server exposes six tools:

- `verity_import`: stream a local file and optionally wait for its job
- `verity_job_status`: read one lifecycle summary
- `verity_inspect_stage`: read at most 50 privacy-safe row summaries
- `verity_list_reviews`: read at most 50 uncertain-record summaries
- `verity_submit_review`: save one explicit decision and reason
- `verity_export`: download and checksum one published artifact

Raw file bytes, prompt bodies, model responses, payload objects, and complete stage artifacts are deliberately excluded from MCP tool results. Cancellation propagates through the SDK context to active REST requests.
