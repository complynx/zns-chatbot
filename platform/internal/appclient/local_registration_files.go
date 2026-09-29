package appclient

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Client) localUploadPassProof(ctx context.Context, owner, filename string, body []byte) (orders.Proof, error) {
	actor, err := c.registrationActor(ctx, owner)
	if err != nil {
		return orders.Proof{}, err
	}
	if len(body) > orders.MaxProofBytes {
		return orders.Proof{}, &core.ProblemError{Status: http.StatusRequestEntityTooLarge, Code: "invalid_proof"}
	}
	value, err := c.LocalRegistration.Files.UploadProof(ctx, actor, filename, body)
	if err != nil {
		return orders.Proof{}, orderApplicationError(err)
	}
	return value, nil
}

func (c Client) localDownloadPassProof(ctx context.Context, actor, event, owner string) (orders.Proof, error) {
	value, err := directRegistration(
		ctx,
		c,
		actor,
		func(service passbooking.Service, verified string) (orders.Proof, error) {
			return service.PaymentProof(ctx, verified, event, owner)
		},
	)
	if err != nil {
		return orders.Proof{}, err
	}
	if value.Attempt == "" {
		return orders.Proof{}, errors.New("invalid payment file version")
	}
	if len(value.Body) == 0 || len(value.Body) > orders.MaxProofBytes {
		return orders.Proof{}, errors.New("invalid payment file size")
	}
	// The public download contract identifies the attachment by attempt and version.
	value.ID = ""
	return value, nil
}

func (c Client) localExportPasses(ctx context.Context, owner string) ([]byte, error) {
	value, err := directRegistration(
		ctx,
		c,
		owner,
		func(service passbooking.Service, actor string) ([]byte, error) { return service.Export(ctx, actor) },
	)
	if err != nil {
		return nil, err
	}
	if len(value) == 0 || len(value) > passbooking.MaxExportBytes {
		return nil, errors.New("invalid export size")
	}
	return value, nil
}

func (c Host) localExportPassSnapshot(ctx context.Context, owner string) (passbooking.ExportSnapshot, error) {
	value, err := directDerived(
		ctx,
		c,
		owner,
		func(service derivedmutation.Service, actor string) (passbooking.ExportSnapshot, error) {
			return service.Registration.ExportSnapshot(ctx, actor)
		},
	)
	if err != nil {
		return passbooking.ExportSnapshot{}, err
	}
	if len(value.Body) == 0 || len(value.Body) > passbooking.MaxExportBytes ||
		!passbooking.ValidExportEvents(value.Events) {
		return passbooking.ExportSnapshot{}, errors.New("invalid export snapshot")
	}
	return value, nil
}

func (c Host) localCheckPassExportSnapshot(ctx context.Context, owner string, events []string) error {
	_, err := directDerived(ctx, c, owner, func(service derivedmutation.Service, actor string) (struct{}, error) {
		return struct{}{}, service.Registration.CheckExportSnapshot(ctx, actor, events)
	})
	return err
}
