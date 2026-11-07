package orders

import (
	"cmp"
	"slices"
)

const (
	MaxResources        = 64
	MaxCapacityBindings = 400
	MaxBindingUnits     = int64(1_000_000)
	MaxResourceUnits    = int64(1_000_000_000)
)

type CapacityResource struct {
	ID      string `json:"id" pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	Name    Label  `json:"name"`
	Ceiling int64  `json:"ceiling" minimum:"0" maximum:"1000000000"`
}

// CapacityBinding charges once per selected offer, or per item quantity when ItemID is set.
type CapacityBinding struct {
	OfferID    string `json:"offer_id" pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	ItemID     string `json:"item_id,omitempty" pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	ResourceID string `json:"resource_id" pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	Units      int64  `json:"units" minimum:"1" maximum:"1000000"`
}

// CompleteCapacity attests that all resource requirements are declared.
// Unlisted offer/item pairs require none; an absent policy means unknown.
type CompleteCapacity struct {
	Version   int                `json:"version" minimum:"1" maximum:"1"`
	Resources []CapacityResource `json:"resources" minItems:"0" maxItems:"64"`
	Bindings  []CapacityBinding  `json:"bindings" minItems:"0" maxItems:"400"`
}

type ResourceDemand struct {
	ResourceID string `json:"resource_id"`
	Name       Label  `json:"name"`
	Units      int64  `json:"units"`
}

// CapacitySnapshot freezes sold requirements, never mutable ceilings or availability.
type CapacitySnapshot struct {
	Version int              `json:"version"`
	Demands []ResourceDemand `json:"demands"`
}

func normalizeCapacity(d CatalogDocument) (*CompleteCapacity, error) {
	p := d.CompleteCapacity
	if p == nil {
		//nolint:nilnil // Optional attestation: missing means unknown, not unlimited.
		return nil, nil
	}
	if p.Version != 1 || p.Resources == nil || p.Bindings == nil || len(p.Resources) > MaxResources ||
		len(p.Bindings) > MaxCapacityBindings {
		return nil, ErrInvalid
	}
	result := &CompleteCapacity{
		Version:   1,
		Resources: append([]CapacityResource{}, p.Resources...),
		Bindings:  append([]CapacityBinding{}, p.Bindings...),
	}
	known := make(map[string]bool, len(p.Resources))
	for i, resource := range p.Resources {
		if !identifier.MatchString(resource.ID) || known[resource.ID] || resource.Ceiling < 0 ||
			resource.Ceiling > MaxResourceUnits {
			return nil, ErrInvalid
		}
		name, err := normalizeLabel(resource.Name)
		if err != nil {
			return nil, err
		}
		result.Resources[i].Name = name
		known[resource.ID] = true
	}
	allowed := make(map[string][]string, len(d.Offers))
	for _, offer := range d.Offers {
		allowed[offer.ID] = offer.ItemIDs
	}
	seen := make(map[[3]string]bool, len(p.Bindings))
	for _, binding := range p.Bindings {
		key := [3]string{binding.OfferID, binding.ItemID, binding.ResourceID}
		if allowed[binding.OfferID] == nil || !known[binding.ResourceID] || seen[key] || binding.Units < 1 ||
			binding.Units > MaxBindingUnits {
			return nil, ErrInvalid
		}
		if binding.ItemID != "" && !slices.Contains(allowed[binding.OfferID], binding.ItemID) {
			return nil, ErrInvalid
		}
		seen[key] = true
	}
	slices.SortFunc(result.Resources, func(a, b CapacityResource) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(result.Bindings, compareCapacityBindings)
	return result, nil
}

func compareCapacityBindings(a, b CapacityBinding) int {
	if n := cmp.Compare(a.OfferID, b.OfferID); n != 0 {
		return n
	}
	if n := cmp.Compare(a.ItemID, b.ItemID); n != 0 {
		return n
	}
	return cmp.Compare(a.ResourceID, b.ResourceID)
}

// CapacityRequirements calculates immutable requirements without checking sales,
// current capacity, ownership or payment. Callers enforce those independently.
func CapacityRequirements(document CatalogDocument, selections []Selection) (*CapacitySnapshot, error) {
	d, err := Normalize(document)
	if err != nil {
		return nil, err
	}
	quantities, offers, err := capacitySelectionQuantities(d, selections)
	if err != nil {
		return nil, err
	}
	if d.CompleteCapacity == nil {
		//nolint:nilnil // Preserve unknown historical context.
		return nil, nil
	}
	totals := make(map[string]int64, len(d.CompleteCapacity.Resources))
	for _, binding := range d.CompleteCapacity.Bindings {
		quantity := int64(0)
		if binding.ItemID != "" {
			quantity = quantities[[2]string{binding.OfferID, binding.ItemID}]
		} else if offers[binding.OfferID] {
			quantity = 1
		}
		if quantity == 0 {
			continue
		}
		prior := totals[binding.ResourceID]
		if quantity > (MaxResourceUnits-prior)/binding.Units {
			return nil, ErrSelection
		}
		totals[binding.ResourceID] = prior + quantity*binding.Units
	}
	result := &CapacitySnapshot{Version: 1, Demands: []ResourceDemand{}}
	for _, resource := range d.CompleteCapacity.Resources {
		if units := totals[resource.ID]; units > 0 {
			result.Demands = append(
				result.Demands,
				ResourceDemand{ResourceID: resource.ID, Name: resource.Name, Units: units},
			)
		}
	}
	return result, nil
}

func capacitySelectionQuantities(
	d CatalogDocument,
	selections []Selection,
) (map[[2]string]int64, map[string]bool, error) {
	if len(selections) < 1 || len(selections) > MaxSelections {
		return nil, nil, ErrSelection
	}
	allowed := make(map[string][]string, len(d.Offers))
	for _, offer := range d.Offers {
		allowed[offer.ID] = offer.ItemIDs
	}
	quantities := make(map[[2]string]int64, len(selections))
	offers := make(map[string]bool)
	var total int64
	for _, selection := range selections {
		key := [2]string{selection.OfferID, selection.ItemID}
		if selection.Quantity < 1 || selection.Quantity > MaxQuantity || quantities[key] != 0 ||
			!slices.Contains(allowed[selection.OfferID], selection.ItemID) ||
			total > MaxTotalQuantity-selection.Quantity {
			return nil, nil, ErrSelection
		}
		quantities[key] = selection.Quantity
		offers[selection.OfferID] = true
		total += selection.Quantity
	}
	return quantities, offers, nil
}

// RetainsCapacityResources preserves event resource identities across catalog edits.
// Unused resources remain declared; a ceiling of zero closes them to future admission.
func RetainsCapacityResources(prior, next *CompleteCapacity) bool {
	if prior == nil {
		return true
	}
	if next == nil {
		return false
	}
	ids := make(map[string]bool, len(next.Resources))
	for _, resource := range next.Resources {
		ids[resource.ID] = true
	}
	for _, resource := range prior.Resources {
		if !ids[resource.ID] {
			return false
		}
	}
	return true
}
