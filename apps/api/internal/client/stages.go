package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/realAllenSong/Verity/apps/api/internal/verity"
)

func (c *Client) StageRecords(ctx context.Context, stageID, cursor string, limit int) (verity.StagePage, error) {
	limit = max(1, min(limit, 200))
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	var page verity.StagePage
	_, err := c.doJSON(ctx, http.MethodGet, "/api/v1/stages/"+url.PathEscape(stageID)+"/records?"+query.Encode(), nil, &page, nil)
	return page, err
}
