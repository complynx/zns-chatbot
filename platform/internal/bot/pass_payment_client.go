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
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func registrationPaymentPath(event, owner string) string {
	return "/v1/passes/events/" + url.PathEscape(event) + "/participants/" + url.PathEscape(owner) + "/payment"
}

func (c APIClient) PassPayment(ctx context.Context, actor, event, owner string) (passbooking.Payment, error) {
	var value passbooking.Payment
	err := c.call(ctx, actor, http.MethodGet, registrationPaymentPath(event, owner), nil, &value)
	return value, err
}

func (c APIClient) PassPaymentQuote(ctx context.Context, actor, event string) (passbooking.PaymentQuote, error) {
	var value passbooking.PaymentQuote
	err := c.call(
		ctx,
		actor,
		http.MethodGet,
		"/v1/passes/events/"+url.PathEscape(event)+"/me/payment-quote",
		nil,
		&value,
	)
	return value, err
}

func (c APIClient) PassPaymentQueue(ctx context.Context, actor, event, after string) (passbooking.PaymentPage, error) {
	var value passbooking.PaymentPage
	err := c.call(ctx, actor, http.MethodGet, "/v1/passes/events/"+url.PathEscape(event)+
		"/payment-queue?"+url.Values{passAfterQuery: {after}}.Encode(), nil, &value)
	return value, err
}

func (c APIClient) UploadPassProof(ctx context.Context, owner, filename string, body []byte) (orders.Proof, error) {
	var value orders.Proof
	err := c.request(ctx, owner, http.MethodPost, "/v1/pass-proofs?filename="+url.QueryEscape(filename), body, &value)
	return value, err
}

// DownloadPassProof returns the current authorized attachment and its version.
// Callers compare both guards with the displayed button before sending the file.
func (c APIClient) DownloadPassProof(ctx context.Context, actor, event, owner string) (orders.Proof, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.Base+registrationPaymentPath(event, owner)+"/file",
		http.NoBody,
	)
	if err != nil {
		return orders.Proof{}, err
	}
	token, err := c.userToken(ctx, actor)
	if err != nil {
		return orders.Proof{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := c.httpClient()
	response, err := client.Do(request)
	if err != nil {
		return orders.Proof{}, errors.New("payment file unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var problem core.ProblemError
		if err = json.NewDecoder(io.LimitReader(response.Body, maxAPIBytes)).Decode(&problem); err != nil {
			return orders.Proof{}, errors.New("invalid payment file response")
		}
		problem.Status = response.StatusCode
		return orders.Proof{}, &problem
	}
	proof := orders.Proof{Attempt: response.Header.Get("X-Payment-Attempt")}
	proof.Version, err = strconv.ParseInt(response.Header.Get("X-Pass-Version"), 10, 64)
	if err != nil || proof.Attempt == "" {
		return orders.Proof{}, errors.New("invalid payment file version")
	}
	_, parameters, err := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
	if err != nil {
		return orders.Proof{}, errors.New("invalid payment filename")
	}
	proof.Filename = parameters["filename"]
	proof.Body, err = io.ReadAll(io.LimitReader(response.Body, orders.MaxProofBytes+1))
	if err != nil || len(proof.Body) == 0 || len(proof.Body) > orders.MaxProofBytes {
		return orders.Proof{}, errors.New("invalid payment file size")
	}
	return proof, nil
}
