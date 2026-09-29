package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func botDeliveryRoutes(
	mux *http.ServeMux,
	s botdelivery.Service,
	auth applicationauth.Authorizer,
	signer identity.Signer,
	logger *slog.Logger,
) {
	botDeliveryRoute(
		mux,
		"enqueue",
		auth,
		signer,
		logger,
		func(in botdelivery.EnqueueRequest) string { return in.Owner },
		s.Enqueue,
	)
	botDeliveryRoute(
		mux,
		"card",
		auth,
		signer,
		logger,
		func(in botdelivery.CardRequest) string { return in.Owner },
		func(ctx context.Context, in botdelivery.CardRequest) (struct{}, error) {
			return struct{}{}, s.EnqueueCard(ctx, in)
		},
	)
	botDeliveryRoute(
		mux,
		"result",
		auth,
		signer,
		logger,
		func(in botdelivery.ResultRequest) string { return in.Owner },
		func(ctx context.Context, in botdelivery.ResultRequest) (struct{}, error) {
			return struct{}{}, s.EnqueueResult(ctx, in)
		},
	)
	botDeliveryRoute(
		mux,
		"receipt",
		auth,
		signer,
		logger,
		func(in botdelivery.ReceiptRequest) string { return in.Observed.Owner },
		func(ctx context.Context, in botdelivery.ReceiptRequest) (struct{}, error) {
			return struct{}{}, s.ApplyReceipt(ctx, in)
		},
	)
	botDeliveryRoute(
		mux,
		"source",
		auth,
		signer,
		logger,
		func(in botdelivery.SourceRequest) string { return in.Owner },
		func(ctx context.Context, in botdelivery.SourceRequest) (struct{}, error) {
			return struct{}{}, s.CheckSource(ctx, in)
		},
	)
	botDeliveryRoute(
		mux,
		"pass-menu",
		auth,
		signer,
		logger,
		func(in botdelivery.PassMenuRequest) string { return in.Owner },
		func(ctx context.Context, in botdelivery.PassMenuRequest) (struct{}, error) {
			return struct{}{}, s.StorePassMenu(ctx, in)
		},
	)

	botDeliveryRoute(
		mux,
		"begin",
		auth,
		signer,
		logger,
		func(in botdelivery.BeginRequest) string { return in.Observed.Owner },
		s.Begin,
	)
}

func botDeliveryRoute[I, O any](
	mux *http.ServeMux,
	path string,
	auth applicationauth.Authorizer,
	signer identity.Signer,
	logger *slog.Logger,
	owner func(I) string,
	call func(context.Context, I) (O, error),
) {
	mux.HandleFunc("POST /internal/bot-delivery/"+path, func(w http.ResponseWriter, r *http.Request) {
		if signer.VerifyDelivery(r.Header.Get(derivedHostHeader)) != nil {
			JSON(w, http.StatusUnauthorized, map[string]string{codeField: unauthorized})
			return
		}
		var in I
		if err := decodeDerivedJSON(
			http.MaxBytesReader(w, r.Body, int64(botdelivery.MaxRequestBytes)),
			&in,
		); err != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if owner(in) != "" && requestOwner(r) != owner(in) {
				JSON(w, http.StatusUnauthorized, map[string]string{codeField: unauthorized})
				return
			}
			value, err := call(r.Context(), in)
			respond(logger, w, value, err)
		})
		if owner(in) == "" {
			next.ServeHTTP(w, r)
			return
		}
		authenticated(auth, next, logger).ServeHTTP(w, r)
	})
}
