package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
)

// IdentityPreparation configures a removable operator operation. Credentials are
// provided by the caller's provider and never appear in summaries or artifacts.
type IdentityPreparation struct {
	DatabaseURL                       string
	Stage, Plan, Policy, Output       string
	Issuer, Organization, EmailDomain string
	Provider                          identityprovision.Provider
	Limits                            Limits
}

type IdentityPrepareSummary struct {
	PlanSHA256     string `json:"plan_sha256"`
	PolicySHA256   string `json:"policy_sha256"`
	ArtifactSHA256 string `json:"artifact_sha256"`
	Candidates     int    `json:"candidates"`
	Prepared       int    `json:"prepared"`
	Excluded       int64  `json:"excluded_other_bot"`
	Reused         bool   `json:"reused"`
}

// PrepareUserIdentities validates the complete reviewed input before any provider
// call. Only reservations are written; ApplyUsers owns the separate atomic import.
func PrepareUserIdentities(ctx context.Context, input IdentityPreparation) (IdentityPrepareSummary, error) {
	var summary IdentityPrepareSummary
	prepared, plan, err := verifiedUserPlan(input.Stage, input.Plan, input.Limits)
	if err != nil {
		return summary, err
	}
	raw, err := readApplyFile(input.Policy, maxUserResolutionBytes)
	if err != nil {
		return summary, err
	}
	policy, err := decodeIdentityPolicy(raw, prepared.planHash)
	if err != nil {
		return summary, err
	}
	candidates, err := identityCandidates(plan, policy)
	if err != nil {
		return summary, err
	}
	if err = validateIdentityPreparation(input); err != nil {
		return summary, err
	}
	if err = checkIdentityOutput(input, prepared.planHash, candidates); err != nil {
		return summary, err
	}
	summary = IdentityPrepareSummary{PlanSHA256: prepared.planHash, PolicySHA256: hashBytes(raw),
		Candidates: len(candidates), Excluded: prepared.excluded}
	artifact, err := writePrivatePlan(input.Stage, input.Output, func(writer io.Writer) error {
		return emitPreparedIdentities(ctx, writer, input, prepared, candidates, &summary)
	})
	summary.ArtifactSHA256, summary.Reused = artifact.ArtifactSHA256, artifact.Reused
	return summary, err
}

func checkIdentityOutput(input IdentityPreparation, hash string, candidates []identityCandidate) error {
	if _, err := os.Lstat(input.Output); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	raw, err := readApplyFile(input.Output, maxUserResolutionBytes)
	if err != nil {
		return errors.New("identity_output_conflict")
	}
	previous, err := decodeUserResolutions(raw)
	if err != nil || previous.PlanSHA256 != hash || len(previous.Users) != len(candidates) {
		return errors.New("identity_output_conflict")
	}
	for index, candidate := range candidates {
		row := previous.Users[index]
		if row.LegacyKey != candidate.key || *row.CanBook != candidate.canBook || row.Issuer != input.Issuer {
			return errors.New("identity_output_conflict")
		}
	}
	return nil
}

func validateIdentityPreparation(input IdentityPreparation) error {
	issuer, err := url.Parse(input.Issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil ||
		issuer.RawQuery != "" || issuer.Fragment != "" || (issuer.Path != "" && issuer.Path != "/") ||
		!validLinkText(input.Issuer) || input.Organization == "" || input.Provider == nil ||
		!strings.HasSuffix(input.EmailDomain, ".invalid") || strings.ContainsAny(input.EmailDomain, " /:@\\\r\n") {
		return errors.New("identity_configuration_invalid")
	}
	return nil
}

func emitPreparedIdentities(ctx context.Context, writer io.Writer, input IdentityPreparation,
	prepared preparedUsers, candidates []identityCandidate, summary *IdentityPrepareSummary) error {
	db, err := pgxpool.New(ctx, input.DatabaseURL)
	if err != nil {
		return errors.New("identity_database_unavailable")
	}
	defer db.Close()
	service := identityprovision.Service{DB: db, Provider: input.Provider, Issuer: input.Issuer,
		Organization: input.Organization, EmailDomain: input.EmailDomain, BotID: prepared.botID}
	resolutions := UserResolutions{Version: 1, PlanSHA256: prepared.planHash, IdentityAttested: true,
		Users: make([]UserResolution, 0, len(candidates))}
	for _, candidate := range candidates {
		binding, prepareErr := service.PrepareTelegram(ctx, candidate.telegram)
		if prepareErr != nil {
			return identityPreparationError(prepareErr)
		}
		allowed := candidate.canBook
		resolutions.Users = append(resolutions.Users, UserResolution{LegacyKey: candidate.key, Owner: binding.Owner,
			Issuer: input.Issuer, Subject: binding.Subject, CanBook: &allowed})
		summary.Prepared++
	}
	data, err := json.Marshal(resolutions)
	if err != nil {
		return errors.New("identity_output_invalid")
	}
	if int64(len(data))+1 > maxUserResolutionBytes {
		return errors.New("identity_output_limit")
	}
	if _, err = decodeUserResolutions(data); err != nil {
		return errors.New("identity_output_invalid")
	}
	if _, err = writer.Write(append(data, '\n')); err != nil {
		return errors.New("identity_output_failed")
	}
	return nil
}

func identityPreparationError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return errors.New("identity_preparation_cancelled")
	case errors.Is(err, identityprovision.ErrConflict):
		return errors.New("identity_preparation_conflict")
	case errors.Is(err, identityprovision.ErrInvalid):
		return errors.New("identity_configuration_invalid")
	default:
		return errors.New("identity_preparation_unavailable")
	}
}
