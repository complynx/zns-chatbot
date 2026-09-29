package migrate

import (
	"encoding/json"
	"errors"
	"io"
	"os"
)

type foodProof struct {
	Reference Proof
	Body      []byte
	Raw       json.RawMessage
}

func (p *preparedFood) loadFoodProofs(root *os.Root) error {
	p.Proofs = map[string]foodProof{}
	for _, event := range p.Events {
		for _, row := range event.Orders {
			for _, part := range []struct {
				field   string
				payment FoodPayment
			}{
				{registrationProofField, row.Food.MealPayment}, {"activities_proof_file", row.Food.ActivityPayment},
			} {
				if part.payment.Proof == "" {
					continue
				}
				if err := p.loadFoodProof(root, row, part.field, part.payment); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (p *preparedFood) loadFoodProof(root *os.Root, row FoodPlanRecord, field string, payment FoodPayment) error {
	var found *Proof
	for _, ref := range p.Plan.Proofs {
		id, _ := recordID(ref.RecordID)
		originalID, _ := recordID(row.Legacy.RecordID)
		if ref.Source != ordersSource || id != originalID || ref.Field != field {
			continue
		}
		if found != nil {
			return errors.New("food_proof_ambiguous")
		}
		copyRef := ref
		found = &copyRef
	}
	if found == nil {
		return errors.New("food_proof_disposition_required")
	}
	owner, ownerErr := recordID(found.OwnerID)
	expectedOwner, expectedErr := recordID(p.Users[row.Food.Owner].Legacy.RecordID)
	if ownerErr != nil || expectedErr != nil || owner != expectedOwner ||
		found.TelegramFileID != payment.Proof {
		return errors.New("food_proof_identity_mismatch")
	}
	payload := foodProof{Reference: *found}
	payload.Raw, _ = json.Marshal(found)
	if !found.Unavailable {
		body, err := p.readFoodProofBlob(root, *found)
		if err != nil {
			return err
		}
		payload.Body = body
	}
	p.Proofs[row.Legacy.Key+":"+field] = payload
	return nil
}

func (p *preparedFood) readFoodProofBlob(root *os.Root, ref Proof) ([]byte, error) {
	var entry File
	for _, f := range p.Plan.Files {
		if f.Path == ref.Blob && f.Kind == orderBlobKind {
			entry = f
		}
	}
	if entry.Path == "" {
		return nil, errors.New("food_proof_blob_required")
	}
	file, err := openRegular(root, entry.Path)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(file, entry.Bytes+1))
	_ = file.Close()
	if err != nil || int64(len(body)) != entry.Bytes || hashBytes(body) != entry.SHA256 {
		return nil, errors.New("food_proof_blob_changed")
	}
	return body, nil
}
