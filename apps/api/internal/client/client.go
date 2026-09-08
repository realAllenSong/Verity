package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/realAllenSong/Verity/apps/api/internal/verity"
)

type Client struct {
	BaseURL   *url.URL
	Token     string
	HTTP      *http.Client
	ChunkSize int64
}

type APIError struct {
	Status int
	Title  string
	Detail string
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("Verity API returned %d: %s", e.Status, e.Detail)
	}
	return fmt.Sprintf("Verity API returned %d: %s", e.Status, e.Title)
}

type workspaceIdentity struct {
	Dataset struct {
		ID string `json:"id"`
	} `json:"dataset"`
}

func New(baseURL, token string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("server must be an absolute http or https URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("server must use http or https")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	return &Client{BaseURL: parsed, Token: strings.TrimSpace(token), HTTP: httpClient, ChunkSize: 8 << 20}, nil
}

func (c *Client) endpoint(reference string) string {
	if parsed, err := url.Parse(reference); err == nil {
		return c.BaseURL.ResolveReference(parsed).String()
	}
	return c.BaseURL.String() + "/" + strings.TrimLeft(reference, "/")
}

func (c *Client) newRequest(ctx context.Context, method, reference string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.endpoint(reference), body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return request, nil
}

func (c *Client) doJSON(ctx context.Context, method, reference string, payload, destination any, headers map[string]string) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := c.newRequest(ctx, method, reference, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		return response, decodeAPIError(response)
	}
	if destination != nil {
		defer response.Body.Close()
		if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
			return response, fmt.Errorf("decode Verity response: %w", err)
		}
	}
	return response, nil
}

func decodeAPIError(response *http.Response) error {
	defer response.Body.Close()
	problem := struct {
		Title  string `json:"title"`
		Detail string `json:"detail"`
	}{}
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	_ = json.Unmarshal(data, &problem)
	if problem.Detail == "" {
		problem.Detail = strings.TrimSpace(string(data))
	}
	if problem.Title == "" {
		problem.Title = response.Status
	}
	return &APIError{Status: response.StatusCode, Title: problem.Title, Detail: problem.Detail}
}

func (c *Client) WorkspaceID(ctx context.Context) (string, error) {
	var workspace workspaceIdentity
	if _, err := c.doJSON(ctx, http.MethodGet, "/api/v1/workspace", nil, &workspace, nil); err != nil {
		return "", err
	}
	if workspace.Dataset.ID == "" {
		return "", errors.New("workspace response has no dataset ID")
	}
	return workspace.Dataset.ID, nil
}

func (c *Client) Job(ctx context.Context, jobID string) (verity.JobSummary, error) {
	var job verity.JobSummary
	_, err := c.doJSON(ctx, http.MethodGet, "/api/v1/jobs/"+url.PathEscape(jobID), nil, &job, nil)
	return job, err
}

func (c *Client) WaitJob(ctx context.Context, jobID string, interval time.Duration) (verity.JobSummary, error) {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	for {
		job, err := c.Job(ctx, jobID)
		if err != nil {
			return verity.JobSummary{}, err
		}
		switch job.State {
		case verity.JobSucceeded:
			return job, nil
		case verity.JobFailed, verity.JobCanceled:
			if job.Error == "" {
				job.Error = "job ended in state " + string(job.State)
			}
			return job, errors.New(job.Error)
		case verity.JobNeedsInput:
			return job, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return verity.JobSummary{}, ctx.Err()
		case <-timer.C:
		}
	}
}
