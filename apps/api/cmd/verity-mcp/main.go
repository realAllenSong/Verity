package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	verityclient "github.com/realAllenSong/Verity/apps/api/internal/client"
	"github.com/realAllenSong/Verity/apps/api/internal/mcpserver"
)

func main() {
	serverURL := strings.TrimSpace(os.Getenv("VERITY_SERVER"))
	if serverURL == "" {
		serverURL = "http://127.0.0.1:8000"
	}
	api, err := verityclient.New(serverURL, os.Getenv("VERITY_API_TOKEN"), nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := mcpserver.New(api).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
