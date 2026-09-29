package migrate

import (
	"encoding/json"
	"errors"
	"os"
)

// OrderDelegatedDomain identifies the importer responsible for retained evidence.
// A disposition is not an apply receipt or a whole-archive completion claim.
type OrderDelegatedDomain string

const orderDelegatedFood OrderDelegatedDomain = "food"
const orderDelegatedMassage OrderDelegatedDomain = "massage"
const orderLegacyFoodKind = "legacy_food"
const orderActivityProofField = "activities_proof_file"

type OrderDomainDisposition struct {
	Domain       OrderDelegatedDomain `json:"domain"`
	Kind         string               `json:"kind"`
	SourceKey    string               `json:"source_key"`
	SourceSHA256 string               `json:"source_sha256"`
	Path         string               `json:"path,omitempty"`
	SHA256       string               `json:"sha256"`
	ProofField   string               `json:"proof_field,omitempty"`
	BlobSHA256   string               `json:"blob_sha256,omitempty"`
}

func delegatedOrderRecord(row OrderPlanRecord, fields map[string]json.RawMessage, bot int64) OrderDelegatedDomain {
	if row.Source == ordersSource {
		return delegatedFoodRecord(row, fields, bot)
	}
	if row.Source != orderConfigurationSource {
		return ""
	}
	var kind string
	_ = json.Unmarshal(fields["kind"], &kind)
	if kind == orderLegacyFoodKind {
		if _, err := convertFoodConfiguration(row.Record); err == nil {
			return orderDelegatedFood
		}
	}
	if kind == "legacy_massage" {
		if _, err := convertMassageConfiguration(row.Record); err == nil {
			return orderDelegatedMassage
		}
	}
	return ""
}

func delegatedFoodRecord(row OrderPlanRecord, fields map[string]json.RawMessage, bot int64) OrderDelegatedDomain {
	if _, present := fields["pass_key"]; !present {
		return ""
	}
	if _, err := convertFood(row.Record, bot); err != nil {
		return ""
	}
	if raw, exists := fields["bot_id"]; exists {
		if id, valid := telegramNumber(raw); !valid || id != bot {
			return ""
		}
	}
	return orderDelegatedFood
}

func (p *OrderPlan) recordDomainDispositions(manifest Manifest) {
	for _, row := range p.Records {
		if row.DelegatedDomain != orderDelegatedFood {
			continue
		}
		if row.Source == orderConfigurationSource {
			p.recordFoodResourceDisposition(row, manifest)
		}
		if row.Source == ordersSource {
			p.recordFoodProofDispositions(row, manifest)
		}
	}
}

func (p *OrderPlan) recordFoodResourceDisposition(row OrderPlanRecord, manifest Manifest) {
	config, _ := convertFoodConfiguration(row.Record)
	for _, file := range manifest.Files {
		if file.Kind == foodResourceKind && file.Source == orderConfigurationSource && file.Path == config.MenuFile &&
			file.SHA256 == config.MenuSHA256 {
			p.Dispositions = append(
				p.Dispositions,
				OrderDomainDisposition{
					Domain:       orderDelegatedFood,
					Kind:         foodResourceKind,
					SourceKey:    row.Legacy.Key,
					SourceSHA256: row.Legacy.RecordSHA256,
					Path:         file.Path,
					SHA256:       file.SHA256,
				},
			)
		}
	}
}

func (p *OrderPlan) recordFoodProofDispositions(row OrderPlanRecord, manifest Manifest) {
	food, _ := convertFood(row.Record, p.BotID)
	id, _ := recordID(row.Legacy.RecordID)
	for _, proof := range p.Proofs {
		proofID, _ := recordID(proof.RecordID)
		if proofID != id {
			continue
		}
		expected := ""
		switch proof.Field {
		case registrationProofField:
			expected = food.MealPayment.Proof
		case orderActivityProofField:
			expected = food.ActivityPayment.Proof
		}
		if expected == "" || expected != proof.TelegramFileID {
			continue
		}
		raw, _ := json.Marshal(proof)
		disposition := OrderDomainDisposition{
			Domain:       orderDelegatedFood,
			Kind:         "proof",
			SourceKey:    row.Legacy.Key,
			SourceSHA256: row.Legacy.RecordSHA256,
			Path:         proof.Blob,
			SHA256:       hashBytes(raw),
			ProofField:   proof.Field,
		}
		for _, file := range manifest.Files {
			if file.Path == proof.Blob && file.Kind == orderBlobKind {
				disposition.BlobSHA256 = file.SHA256
			}
		}
		p.Dispositions = append(p.Dispositions, disposition)
	}
}

func (p *OrderPlan) resourceDelegated(file File) bool {
	for _, disposition := range p.Dispositions {
		if disposition.Kind == foodResourceKind && disposition.Path == file.Path && disposition.SHA256 == file.SHA256 {
			return true
		}
	}
	return false
}

func (p *OrderPlan) validateOrderCoverage() error {
	for _, source := range p.Sources {
		if source.Status != sourceIncluded {
			return errors.New("order_source_coverage_required")
		}
	}
	for _, file := range p.Files {
		if file.Kind == foodResourceKind && !p.resourceDelegated(file) {
			return errors.New("order_configuration_records_required")
		}
	}
	return nil
}

func (p *OrderPlan) proofDelegated(proof Proof) bool {
	raw, _ := json.Marshal(proof)
	for _, disposition := range p.Dispositions {
		if disposition.Kind == "proof" && disposition.SHA256 == hashBytes(raw) {
			return true
		}
	}
	return false
}

// Reuse the owning proof validator before omitting proof references from the
// modern order index. Missing, duplicate, foreign-owner and changed blobs fail.
func (p *preparedOrders) validateDelegatedProofs(root *os.Root, manifest Manifest) error {
	food := preparedFood{Users: p.Users, Plan: FoodPlan{Proofs: p.Plan.Proofs, Files: manifest.Files}}
	var rows []FoodPlanRecord
	for _, row := range p.Plan.Records {
		if row.DelegatedDomain == orderDelegatedFood && row.Source == ordersSource {
			candidate, _ := convertFood(row.Record, p.Plan.BotID)
			rows = append(rows, FoodPlanRecord{Legacy: row.Legacy, Food: candidate})
		}
	}
	food.Events = []preparedFoodEvent{{Orders: rows}}
	return food.loadFoodProofs(root)
}
