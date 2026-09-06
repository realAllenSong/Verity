package verity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type AirbyteClient struct {
	baseURL string
	token   string
	client  *http.Client
}

type AirbyteSyncRequest struct {
	ConnectionID string `json:"connection_id"`
}

type AirbyteJob struct {
	JobID   int64  `json:"job_id"`
	Status  string `json:"status"`
	JobType string `json:"job_type,omitempty"`
}

func NewAirbyteClient(baseURL, token string, client *http.Client) (*AirbyteClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("Airbyte URL must be an absolute HTTP URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("Airbyte URL must use http or https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("Airbyte URL must not contain credentials, query parameters, or a fragment")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &AirbyteClient{baseURL: baseURL, token: strings.TrimSpace(token), client: client}, nil
}

func (c *AirbyteClient) TriggerSync(ctx context.Context, connectionID string) (AirbyteJob, error) {
	payload, err := json.Marshal(map[string]string{"connectionId": connectionID, "jobType": "sync"})
	if err != nil {
		return AirbyteJob{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/jobs", bytes.NewReader(payload))
	if err != nil {
		return AirbyteJob{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	return c.do(request)
}

func (c *AirbyteClient) Job(ctx context.Context, jobID int64) (AirbyteJob, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/jobs/"+strconv.FormatInt(jobID, 10), nil)
	if err != nil {
		return AirbyteJob{}, err
	}
	return c.do(request)
}

func (c *AirbyteClient) do(request *http.Request) (AirbyteJob, error) {
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Verity/0.3")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return AirbyteJob{}, fmt.Errorf("call Airbyte: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return AirbyteJob{}, fmt.Errorf("read Airbyte response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return AirbyteJob{}, fmt.Errorf("Airbyte returned %s", response.Status)
	}
	var wire struct {
		JobID   int64  `json:"jobId"`
		Status  string `json:"status"`
		JobType string `json:"jobType"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return AirbyteJob{}, fmt.Errorf("decode Airbyte response: %w", err)
	}
	if wire.JobID == 0 {
		return AirbyteJob{}, errors.New("Airbyte response is missing jobId")
	}
	return AirbyteJob{JobID: wire.JobID, Status: wire.Status, JobType: wire.JobType}, nil
}
