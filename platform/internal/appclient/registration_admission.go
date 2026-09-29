package appclient

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
)

// AdmitPassBooking carries host-observed ingress independently of model commands.
func (c Host) AdmitPassBooking(
	ctx context.Context,
	owner string,
	command passbooking.Command,
	source *readsource.Derivation,
) (passbooking.Admission, error) {
	if !passbooking.InitiatesRegistration(command) {
		return passbooking.Admission{}, nil
	}
	ref, ok := registrationingress.FromContext(ctx)
	if !ok {
		return passbooking.Admission{}, &core.ProblemError{Status: http.StatusBadRequest, Code: "pass_ingress_required"}
	}
	request := passbooking.AdmissionRequest{Command: command, Ingress: &ref}
	if c.LocalDerived != nil {
		return directDerived(
			ctx,
			c,
			owner,
			func(s derivedmutation.Service, actor string) (passbooking.Admission, error) {
				if source != nil {
					return s.CapturePassAdmission(ctx, actor, request, source.Clone())
				}
				return s.Registration.CaptureAdmission(ctx, actor, request)
			},
		)
	}
	var result passbooking.Admission
	if source != nil {
		err := c.derivedRequest(ctx, owner, "/internal/derived/pass-admission", request, source.Clone(), &result)
		return result, err
	}
	if c.UserToken == nil {
		return result, identity.ErrZitadelIdentity
	}
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return result, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	err = requestAuthorizedLimit(ctx, c.Base, c.HTTP, token, c.Signer.DerivedMutationToken(owner), http.MethodPost,
		"/internal/registration/admission", body, &result, int64(MaxAPIBytes))
	return result, err
}
