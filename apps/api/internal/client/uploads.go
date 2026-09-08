package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/realAllenSong/Verity/apps/api/internal/verity"
)

func (c *Client) ImportFile(ctx context.Context, sourcePath string) (verity.JobSummary, error) {
	file, err := os.Open(sourcePath)
	if err != nil {
		return verity.JobSummary{}, fmt.Errorf("open source: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return verity.JobSummary{}, fmt.Errorf("stat source: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() < 1 {
		return verity.JobSummary{}, fmt.Errorf("source must be a non-empty regular file")
	}
	datasetID, err := c.WorkspaceID(ctx)
	if err != nil {
		return verity.JobSummary{}, err
	}
	identity := sha256.Sum256([]byte(filepath.Base(sourcePath) + ":" + strconv.FormatInt(info.Size(), 10) + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10)))
	key := "cli:" + hex.EncodeToString(identity[:16])
	request := verity.ImportCreate{
		DatasetID: datasetID,
		Filename:  filepath.Base(sourcePath),
		MediaType: mime.TypeByExtension(filepath.Ext(sourcePath)),
		SizeBytes: info.Size(),
	}
	var imported verity.ImportSummary
	if _, err := c.doJSON(ctx, http.MethodPost, "/api/v1/imports", request, &imported, map[string]string{"Idempotency-Key": key}); err != nil {
		return verity.JobSummary{}, err
	}
	offset := imported.Offset
	if offset > 0 && offset < info.Size() {
		head, err := c.newRequest(ctx, http.MethodHead, imported.UploadURL, nil)
		if err != nil {
			return verity.JobSummary{}, err
		}
		response, err := c.HTTP.Do(head)
		if err != nil {
			return verity.JobSummary{}, err
		}
		if response.StatusCode != http.StatusNoContent {
			return verity.JobSummary{}, decodeAPIError(response)
		}
		response.Body.Close()
		acknowledged, err := strconv.ParseInt(response.Header.Get("Upload-Offset"), 10, 64)
		if err != nil || acknowledged < 0 || acknowledged > info.Size() {
			return verity.JobSummary{}, errorsNewUploadOffset()
		}
		offset = acknowledged
	}
	chunkSize := c.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 8 << 20
	}
	for offset < info.Size() {
		length := min(chunkSize, info.Size()-offset)
		request, err := c.newRequest(ctx, http.MethodPatch, imported.UploadURL, io.NewSectionReader(file, offset, length))
		if err != nil {
			return verity.JobSummary{}, err
		}
		request.ContentLength = length
		request.Header.Set("Content-Type", "application/offset+octet-stream")
		request.Header.Set("Upload-Offset", strconv.FormatInt(offset, 10))
		response, err := c.HTTP.Do(request)
		if err != nil {
			return verity.JobSummary{}, err
		}
		if response.StatusCode != http.StatusNoContent {
			return verity.JobSummary{}, decodeAPIError(response)
		}
		response.Body.Close()
		next, err := strconv.ParseInt(response.Header.Get("Upload-Offset"), 10, 64)
		if err != nil || next <= offset {
			return verity.JobSummary{}, errorsNewUploadOffset()
		}
		offset = next
	}
	var job verity.JobSummary
	_, err = c.doJSON(ctx, http.MethodPost, "/api/v1/imports/"+imported.ImportID+"/complete", nil, &job, map[string]string{"Idempotency-Key": "complete:" + key})
	return job, err
}

func errorsNewUploadOffset() error {
	return fmt.Errorf("upload response did not acknowledge a forward offset")
}
