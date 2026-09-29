package bot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (c APIClient) DownloadOrderProof(ctx context.Context, owner, event, id string) (orders.Proof, error) {
	path := "/v1/order-events/" + url.PathEscape(event) + "/orders/" + url.PathEscape(id) + "/proof/file"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, http.NoBody)
	if err != nil {
		return orders.Proof{}, err
	}
	token, err := c.userToken(ctx, owner)
	if err != nil {
		return orders.Proof{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := c.httpClient()
	response, err := client.Do(request)
	if err != nil {
		return orders.Proof{}, errors.New("core API unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var problem core.ProblemError
		if err = json.NewDecoder(io.LimitReader(response.Body, maxAPIBytes)).Decode(&problem); err != nil {
			return orders.Proof{}, err
		}
		problem.Status = response.StatusCode
		return orders.Proof{}, &problem
	}
	proof := orders.Proof{ID: response.Header.Get("X-Proof-Id"), Attempt: response.Header.Get("X-Payment-Attempt")}
	proof.Version, err = strconv.ParseInt(response.Header.Get("X-Order-Version"), 10, 64)
	if err != nil {
		return orders.Proof{}, errors.New("invalid proof version")
	}
	_, parameters, err := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
	if err != nil {
		return orders.Proof{}, err
	}
	proof.Filename = parameters["filename"]
	proof.Body, err = io.ReadAll(io.LimitReader(response.Body, orders.MaxProofBytes+1))
	if err != nil {
		return orders.Proof{}, err
	}
	if len(proof.Body) == 0 || len(proof.Body) > orders.MaxProofBytes {
		return orders.Proof{}, errors.New("invalid proof size")
	}
	return proof, nil
}
