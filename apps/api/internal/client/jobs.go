package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/realAllenSong/Verity/apps/api/internal/verity"
)

func (c *Client) JobEvents(ctx context.Context, jobID string, after uint64) ([]verity.JobEvent, error) {
	request, err := c.newRequest(ctx, http.MethodGet, "/api/v1/jobs/"+url.PathEscape(jobID)+"/events", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/event-stream")
	if after > 0 {
		request.Header.Set("Last-Event-ID", strconv.FormatUint(after, 10))
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, decodeAPIError(response)
	}
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		return nil, fmt.Errorf("job events returned %q instead of text/event-stream", response.Header.Get("Content-Type"))
	}
	var events []verity.JobEvent
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 16*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event verity.JobEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			return nil, fmt.Errorf("decode job event: %w", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}
