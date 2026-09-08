package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type DownloadResult struct {
	Path           string `json:"path"`
	Bytes          int64  `json:"bytes"`
	SHA256         string `json:"sha256"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
	Verified       bool   `json:"verified"`
}

func (c *Client) DownloadOutput(ctx context.Context, outputID, targetPath string) (DownloadResult, error) {
	request, err := c.newRequest(ctx, http.MethodGet, "/api/v1/outputs/"+url.PathEscape(outputID), nil)
	if err != nil {
		return DownloadResult{}, err
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return DownloadResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return DownloadResult{}, decodeAPIError(response)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil {
		return DownloadResult{}, err
	}
	file, err := os.CreateTemp(filepath.Dir(targetPath), filepath.Base(targetPath)+"-*.partial")
	if err != nil {
		return DownloadResult{}, err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), response.Body)
	if syncErr := file.Sync(); copyErr == nil {
		copyErr = syncErr
	}
	if closeErr := file.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return DownloadResult{}, fmt.Errorf("download output: %w", copyErr)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	expected := strings.TrimSpace(response.Header.Get("X-Verity-SHA256"))
	if expected != "" && !strings.EqualFold(expected, actual) {
		return DownloadResult{}, fmt.Errorf("output checksum mismatch: expected %s, got %s", expected, actual)
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return DownloadResult{}, err
	}
	if err := os.Rename(temporary, targetPath); err != nil {
		return DownloadResult{}, err
	}
	return DownloadResult{Path: targetPath, Bytes: written, SHA256: actual, ExpectedSHA256: expected, Verified: expected != ""}, nil
}
