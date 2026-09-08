package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/realAllenSong/Verity/apps/api/internal/verity"
)

type ReviewQueue struct {
	Count    int                     `json:"count"`
	Returned int                     `json:"returned"`
	Records  []verity.EvidenceRecord `json:"records"`
}

func (c *Client) Reviews(ctx context.Context) (ReviewQueue, error) {
	var result ReviewQueue
	_, err := c.doJSON(ctx, http.MethodGet, "/api/v1/review-queue", nil, &result, nil)
	return result, err
}

func (c *Client) SubmitReview(ctx context.Context, recordID, decision, note string) (json.RawMessage, error) {
	var workspace json.RawMessage
	_, err := c.doJSON(ctx, http.MethodPatch, "/api/v1/reviews/"+url.PathEscape(recordID), verity.ReviewUpdate{Decision: decision, Note: note}, &workspace, nil)
	return workspace, err
}
