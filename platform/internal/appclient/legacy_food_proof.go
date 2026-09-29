package appclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (c Client) FoodProof(ctx context.Context, owner string, command legacyfood.Command) ([]byte, error) {
	return c.foodProof(ctx, owner, command, false)
}

func (c Client) FoodReviewProof(ctx context.Context, owner string, command legacyfood.Command) ([]byte, error) {
	return c.foodProof(ctx, owner, command, true)
}

func (c Client) foodProof(
	ctx context.Context,
	owner string,
	command legacyfood.Command,
	observed bool,
) ([]byte, error) {
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return nil, err
	}
	path := "/v1/food/events/" + url.PathEscape(
		command.EventID,
	) + "/orders/" + url.PathEscape(
		command.OrderID,
	) + "/proof?kind=" + url.QueryEscape(
		command.Kind,
	) + "&generation=" + strconv.FormatInt(
		command.Generation,
		10,
	)
	if observed {
		path += "&version=" + strconv.FormatInt(command.Version, 10)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, http.NoBody)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := c.HTTPClient()
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("food proof unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var problem core.ProblemError
		if err = json.NewDecoder(io.LimitReader(response.Body, MaxAPIBytes)).Decode(&problem); err != nil {
			return nil, err
		}
		problem.Status = response.StatusCode
		return nil, &problem
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, orders.MaxProofBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || len(body) > orders.MaxProofBytes {
		return nil, errors.New("invalid food proof size")
	}
	return body, nil
}
