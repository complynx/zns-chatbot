package credits

import (
	"context"
	"encoding/json"
	"errors"
)

type PriceRevision struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Source   string `json:"source"`
	Price    Price  `json:"price"`
}

// RegisterPrice accepts an explicit operator-reviewed revision. A version cannot
// be reused with changed rates, conversion, model, provider or provenance.
func (s Service) RegisterPrice(ctx context.Context, revision PriceRevision) error {
	return registerPrice(ctx, s.DB, revision)
}

func registerPrice(ctx context.Context, db executor, revision PriceRevision) error {
	if len(revision.Price.ServiceTier) > 64 ||
		(revision.Provider == "openai" && revision.Price.Unit == "" && (revision.Price.ServiceTier == "" || revision.Price.MaxInput <= 0)) {
		return ErrInvalid
	}
	if len(revision.Price.Currency) != 3 || len(revision.Price.ConversionVersion) > 256 {
		return ErrInvalid
	}
	for _, char := range revision.Price.Currency {
		if char < 'A' || char > 'Z' {
			return ErrInvalid
		}
	}
	for _, field := range []string{revision.Provider, revision.Model, revision.Source, revision.Price.Version, revision.Price.Currency} {
		if field == "" || len(field) > 256 {
			return ErrInvalid
		}
	}
	zero := int64(0)
	input := revision.Price.MinInput
	sample := Usage{
		Basis:       basisReported,
		ServiceTier: revision.Price.ServiceTier,
		Input:       &input,
		Cached:      &zero,
		CacheWrite:  &zero,
		Output:      &zero,
	}
	if revision.Price.Unit == UnitASRTokens {
		sample.AudioInput, sample.TextInput = &input, &zero
	}
	if revision.Price.Unit == UnitAudioSeconds {
		seconds := "0"
		sample = Usage{Basis: basisReported, AudioSeconds: &seconds}
	}
	_, err := revision.Price.Estimate(sample)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(revision.Price)
	if err != nil {
		return ErrInvalid
	}
	tag, err := db.Exec(ctx, `INSERT INTO credits.price_versions(version,provider,model,specification,source)
 VALUES($1,$2,$3,$4,$5) ON CONFLICT(version) DO UPDATE SET version=EXCLUDED.version
 WHERE credits.price_versions.provider=EXCLUDED.provider AND credits.price_versions.model=EXCLUDED.model
 AND credits.price_versions.specification=EXCLUDED.specification AND credits.price_versions.source=EXCLUDED.source`,
		revision.Price.Version, revision.Provider, revision.Model, raw, revision.Source)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return err
}

// Quote uses an exact revision, never the newest rate or an inferred conversion.
// This is a local price-list estimate and cannot be labelled a provider charge.
func (s Service) Quote(ctx context.Context, provider, model, version string, usage Usage) (Settlement, error) {
	return quote(ctx, s.DB, provider, model, version, usage)
}

func quote(ctx context.Context, db queryRow, provider, model, version string, usage Usage) (Settlement, error) {
	var raw []byte
	if err := db.QueryRow(ctx, `SELECT specification FROM credits.price_versions WHERE version=$1 AND provider=$2 AND model=$3`, version, provider, model).
		Scan(&raw); err != nil {
		return Settlement{}, err
	}
	var price Price
	if json.Unmarshal(raw, &price) != nil {
		return Settlement{}, ErrInvalid
	}
	amount, original, err := price.calculate(usage)
	if err != nil {
		return Settlement{}, err
	}
	return Settlement{
		Usage:             usage,
		CostNanoUSD:       &amount,
		CostBasis:         basisEstimated,
		PriceVersion:      version,
		ConversionVersion: price.ConversionVersion,
		OriginalAmount:    original,
		OriginalCurrency:  price.Currency,
	}, nil
}

// SelectPrice changes only future reservations. Existing attempts retain their
// exact revision. No provider rate is fetched or inferred during a call.
func (s Service) SelectPrice(ctx context.Context, provider, model, version string) error {
	return selectPrice(ctx, s.DB, provider, model, version)
}

func selectPrice(ctx context.Context, db executor, provider, model, version string) error {
	tag, err := db.Exec(ctx, `INSERT INTO credits.price_selection(provider,model,version)
 SELECT provider,model,version FROM credits.price_versions WHERE provider=$1 AND model=$2 AND version=$3
 ON CONFLICT(provider,model) DO UPDATE SET version=EXCLUDED.version`, provider, model, version)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrInvalid
	}
	return err
}

func priceUsage(ctx context.Context, db queryRow, id string, value Settlement) (Settlement, error) {
	if value.CostBasis != basisUnknown || value.Usage.Basis != basisReported {
		return value, nil
	}
	var provider, model string
	var version *string
	err := db.QueryRow(ctx, `SELECT provider,model,price_version FROM credits.attempts WHERE id=$1`, id).
		Scan(&provider, &model, &version)
	if err != nil {
		return Settlement{}, err
	}
	if version == nil || value.Usage.Model != model {
		return value, nil
	}
	quote, err := quote(ctx, db, provider, model, *version, value.Usage)
	if errors.Is(err, ErrInvalid) {
		return value, nil
	}
	return quote, err
}
