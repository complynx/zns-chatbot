package appclient

import (
	"context"

	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Client) PassPayment(ctx context.Context, actor, event, owner string) (passbooking.Payment, error) {
	if c.LocalRegistration != nil {
		return c.localPassPayment(ctx, actor, event, owner)
	}
	var value passbooking.Payment
	err := c.Call(ctx, actor, http.MethodGet, registrationPaymentPath(event, owner), nil, &value)
	return registrationHTTPResult(value, err)
}

func (c Client) PassPaymentQuote(ctx context.Context, actor, event string) (passbooking.PaymentQuote, error) {
	if c.LocalRegistration != nil {
		return c.localPassPaymentQuote(ctx, actor, event)
	}
	var value passbooking.PaymentQuote
	err := c.Call(
		ctx,
		actor,
		http.MethodGet,
		"/v1/passes/events/"+url.PathEscape(event)+"/me/payment-quote",
		nil,
		&value,
	)
	return registrationHTTPResult(value, err)
}

func (c Client) PassPaymentQueue(ctx context.Context, actor, event, after string) (passbooking.PaymentPage, error) {
	if c.LocalRegistration != nil {
		return c.localPassPaymentQueue(ctx, actor, event, after)
	}
	var value passbooking.PaymentPage
	err := c.Call(ctx, actor, http.MethodGet, "/v1/passes/events/"+url.PathEscape(event)+
		"/payment-queue?"+url.Values{passAfterQuery: {after}}.Encode(), nil, &value)
	return registrationHTTPResult(value, err)
}

func (c Client) UploadPassProof(ctx context.Context, owner, filename string, body []byte) (orders.Proof, error) {
	if c.LocalRegistration != nil {
		return c.localUploadPassProof(ctx, owner, filename, body)
	}
	var value orders.Proof
	err := c.Request(ctx, owner, http.MethodPost, "/v1/pass-proofs?filename="+url.QueryEscape(filename), body, &value)
	if err != nil {
		return orders.Proof{}, err
	}
	return value, nil
}

// DownloadPassProof returns the current authorized attachment and its version.
// Callers compare both guards with the displayed button before sending the file.
func (c Client) DownloadPassProof(ctx context.Context, actor, event, owner string) (orders.Proof, error) {
	if c.LocalRegistration != nil {
		return c.localDownloadPassProof(ctx, actor, event, owner)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.Base+registrationPaymentPath(event, owner)+"/file",
		http.NoBody,
	)
	if err != nil {
		return orders.Proof{}, err
	}
	token, err := c.UserToken(ctx, actor)
	if err != nil {
		return orders.Proof{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := c.HTTPClient()
	response, err := client.Do(request)
	if err != nil {
		return orders.Proof{}, clientBoundaryError(ctx, err, "payment file unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return orders.Proof{}, coreResponseError(ctx, response, "invalid payment file response")
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
		return orders.Proof{}, clientBoundaryError(ctx, err, "invalid payment file size")
	}
	return proof, nil
}
