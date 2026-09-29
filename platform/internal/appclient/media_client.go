package appclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/media"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (c Client) UploadMedia(ctx context.Context, owner, name string, body []byte) (media.Attachment, error) {
	var result media.Attachment
	err := c.Request(ctx, owner, http.MethodPost, "/v1/media?filename="+url.QueryEscape(name), body, &result)
	return result, err
}

func (c Client) Media(ctx context.Context, owner, id string) (media.Attachment, error) {
	var result media.Attachment
	path := "/v1/media/" + url.PathEscape(id)
	if err := c.Call(ctx, owner, http.MethodGet, path, nil, &result); err != nil {
		return result, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path+"/file", http.NoBody)
	if err != nil {
		return result, err
	}
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return result, err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	client := c.HTTPClient()
	response, err := client.Do(r)
	if err != nil {
		return result, errors.New("media download unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusForbidden {
		return result, &core.ProblemError{Status: response.StatusCode, Code: "media_not_found"}
	}
	if response.StatusCode != http.StatusOK {
		return result, errors.New("media download unavailable")
	}
	result.Body, err = io.ReadAll(io.LimitReader(response.Body, media.MaxMediaBytes+1))
	if err != nil || len(result.Body) == 0 || len(result.Body) > media.MaxMediaBytes {
		return media.Attachment{}, errors.New("invalid media body")
	}
	return result, nil
}

func (c Client) PromoteMedia(ctx context.Context, owner, id string) (orders.Proof, error) {
	var result orders.Proof
	err := c.Call(ctx, owner, http.MethodPost, "/v1/media/"+url.PathEscape(id)+"/proof", nil, &result)
	return result, err
}
