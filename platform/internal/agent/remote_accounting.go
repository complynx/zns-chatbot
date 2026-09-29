package agent

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

// AuthenticatedModelHandler owns the paid remote boundary. Only the application
// holding the channel secret can supply accounting scope; the proxy never meters
// again. Provider attempt receipts remain in the shared durable accounting store.
func AuthenticatedModelHandler(model Model, secret string, enforce bool) http.Handler {
	next := ModelHandler(model)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if secret == "" ||
			subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+secret)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mode, err := credits.RequestMode(r)
		if err != nil || mode != enforce {
			http.Error(w, "accounting mode mismatch", http.StatusBadRequest)
			return
		}
		scope, err := credits.AuthenticatedScope(r)
		if err != nil || scope.Key == "unattributed" {
			http.Error(w, "accounting scope required", http.StatusBadRequest)
			return
		}
		ctx, receipts := credits.WithReceiptCollector(credits.WithScope(r.Context(), scope))
		buffer := &modelResponse{header: make(http.Header), status: http.StatusOK}
		next.ServeHTTP(buffer, r.WithContext(ctx))
		maps.Copy(w.Header(), buffer.header)
		raw, _ := json.Marshal(receipts.IDs())
		w.Header().Set("X-Zns-Credit-Attempts", base64.RawURLEncoding.EncodeToString(raw))
		w.WriteHeader(buffer.status)
		_, _ = w.Write(buffer.body.Bytes())
	})
}

type modelResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *modelResponse) Header() http.Header            { return w.header }
func (w *modelResponse) WriteHeader(status int)         { w.status = status }
func (w *modelResponse) Write(data []byte) (int, error) { return w.body.Write(data) }

func (m Remote) receiveReceipts(ctx context.Context, response *http.Response) error {
	if m.Secret == "" {
		return nil
	}
	header := response.Header.Get("X-Zns-Credit-Attempts")
	if header == "" || len(header) > 4096 {
		return credits.ErrAccounting
	}
	raw, err := base64.RawURLEncoding.DecodeString(header)
	var ids []string
	if err != nil || json.Unmarshal(raw, &ids) != nil || len(ids) > credits.MaxRemoteReceipts {
		return credits.ErrAccounting
	}
	if m.Receipts == nil {
		return credits.ErrAccounting
	}
	return m.Receipts.VerifyRemoteReceipts(ctx, ids)
}

func (m Remote) prepareRequest(request *http.Request) {
	if m.Secret == "" {
		return
	}
	request.Header.Set("Authorization", "Bearer "+m.Secret)
	credits.SetRequestScope(request)
	credits.SetRequestMode(request, m.Enforce)
}

func remoteClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Timeout: remoteTimeout}
	}
	singleSend := *client
	singleSend.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &singleSend
}
