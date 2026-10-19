package orders

const MaxSupplements = 20

type ChargeMode string

const (
	PerPortion ChargeMode = "per_portion"
	PerOffer   ChargeMode = "per_offer"
)

type Supplement struct {
	ID         string     `json:"id"          pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	Name       Label      `json:"name"`
	PriceMinor int64      `json:"price_minor"                                   minimum:"0" maximum:"9000000000000000"`
	Mode       ChargeMode `json:"mode"                                                                                 enum:"per_portion,per_offer"`
}

// CompletePricing attests that base prices plus required supplements
// cover the complete price, even when the supplement list is explicitly empty.
type CompletePricing struct {
	Version     int          `json:"version"     minimum:"1" maximum:"1"`
	Supplements []Supplement `json:"supplements"                         minItems:"0" maxItems:"20"`
}

func normalizePricing(p *CompletePricing) (*CompletePricing, error) {
	if p == nil {
		// Absence is valid and intentionally disables calculation for old catalogs.
		//nolint:nilnil // Optional policy: nil means no complete-pricing attestation.
		return nil, nil
	}
	if p.Version != 1 || p.Supplements == nil || len(p.Supplements) > MaxSupplements {
		return nil, ErrInvalid
	}
	result := &CompletePricing{Version: p.Version, Supplements: append([]Supplement{}, p.Supplements...)}
	seen := make(map[string]bool, len(p.Supplements))
	for i, supplement := range p.Supplements {
		if !identifier.MatchString(supplement.ID) || seen[supplement.ID] || supplement.PriceMinor < 0 ||
			supplement.PriceMinor > MaxPrice || (supplement.Mode != PerPortion && supplement.Mode != PerOffer) {
			return nil, ErrInvalid
		}
		name, err := normalizeLabel(supplement.Name)
		if err != nil {
			return nil, err
		}
		result.Supplements[i].Name = name
		seen[supplement.ID] = true
	}
	return result, nil
}

func normalizeSupplementReferences(refs []string, pricing *CompletePricing) ([]string, error) {
	if len(refs) > MaxSupplements {
		return nil, ErrInvalid
	}
	known := make(map[string]bool)
	if pricing != nil {
		for _, supplement := range pricing.Supplements {
			known[supplement.ID] = true
		}
	}
	seen := make(map[string]bool, len(refs))
	for _, id := range refs {
		if !known[id] || seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
	}
	return append([]string(nil), refs...), nil
}
