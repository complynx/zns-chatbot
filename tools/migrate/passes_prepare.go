package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

type PassResolutions struct {
	HistoricalAnnouncements string `json:"historical_announcements,omitempty"`
	Version                 int    `json:"version"`
	PlanSHA256              string `json:"plan_sha256"`
	DatesVerified           bool   `json:"dates_verified"`
	BotNamespaceVerified    bool   `json:"bot_namespace_verified"`
	WritersStopped          bool   `json:"writers_stopped"`
}
type preparedPasses struct {
	HistoricalAnnouncements  string
	Catalogs                 map[string]*EventCandidate
	Plan                     PassPlan
	PlanHash, ResolutionHash string
	Users                    map[int64]OrderDependency
	Events                   map[string]OrderDependency
	Bookings                 map[string][]PassPlanRecord
	Preferences              []PassPlanRecord
	Proofs                   map[string]passProofPayload
}
type passProofPayload struct {
	Reference Proof
	Body      []byte
	Hash      string
}

func preparePasses(stage, planPath, resolutions string, limits Limits) (preparedPasses, error) {
	p := preparedPasses{
		Users:    map[int64]OrderDependency{},
		Events:   map[string]OrderDependency{},
		Bookings: map[string][]PassPlanRecord{},
		Proofs:   map[string]passProofPayload{},
	}
	plan, generated, err := preparedPassPlan(stage, limits)
	if err != nil {
		return p, err
	}
	p.Plan = plan
	p.PlanHash = hashBytes(generated)
	original, err := readApplyFile(planPath, maxUserPlanBytes)
	if err != nil {
		return p, err
	}
	if !bytes.Equal(original, generated) {
		return p, errors.New("apply_plan_mismatch")
	}
	raw, err := readApplyFile(resolutions, maxUserResolutionBytes)
	if err != nil {
		return p, err
	}
	if validJSON(raw) != nil {
		return p, errors.New("resolution_invalid")
	}
	if _, err = objectFields(
		raw,
		"version plan_sha256 dates_verified bot_namespace_verified writers_stopped historical_announcements",
	); err != nil {
		return p, errors.New("resolution_invalid")
	}
	var resolution PassResolutions
	if json.Unmarshal(raw, &resolution) != nil || resolution.Version != 1 || resolution.PlanSHA256 != p.PlanHash ||
		!resolution.DatesVerified ||
		!resolution.BotNamespaceVerified ||
		!resolution.WritersStopped {
		return p, errors.New("pass_resolution_required")
	}
	p.ResolutionHash = hashBytes(raw)
	if err = p.indexPassRecords(); err != nil {
		return p, err
	}
	p.HistoricalAnnouncements = resolution.HistoricalAnnouncements
	if err = validateAnnouncementPolicy(p); err != nil {
		return p, err
	}
	if err = p.validatePassPairs(); err != nil {
		return p, err
	}
	if len(p.eventNames()) > 0 {
		catalog, _, catalogErr := preparedEventPlan(stage, limits)
		if catalogErr != nil {
			return p, catalogErr
		}
		p.Catalogs = map[string]*EventCandidate{}
		for _, row := range catalog.Events {
			if row.Candidate != nil {
				p.Catalogs[row.Candidate.ID] = row.Candidate
			}
		}
	}
	return p, p.loadPassProofs(stage, limits)
}
func (p *preparedPasses) indexPassRecords() error {
	for _, d := range p.Plan.Dependencies {
		if d.Source == usersSource {
			if _, ok := p.Users[d.TelegramID]; ok {
				return errors.New("pass_dependency_duplicate")
			}
			p.Users[d.TelegramID] = d
		} else {
			if _, ok := p.Events[d.EventID]; ok {
				return errors.New("pass_dependency_duplicate")
			}
			p.Events[d.EventID] = d
		}
	}
	for _, row := range p.Plan.Records {
		if row.Excluded {
			continue
		}
		if len(row.Blockers) > 0 {
			return errors.New("pass_plan_blocked")
		}
		if row.Candidate != nil {
			if _, ok := p.Events[row.Candidate.Event]; !ok {
				return errors.New("pass_event_dependency_required")
			}
			p.Bookings[row.Candidate.Event] = append(p.Bookings[row.Candidate.Event], row)
		} else {
			p.Preferences = append(p.Preferences, row)
		}
	}
	return nil
}
func (p *preparedPasses) validatePassPairs() error {
	for _, rows := range p.Bookings {
		if err := p.validateEventPairs(rows); err != nil {
			return err
		}
	}
	for _, row := range p.Preferences {
		ids := []int64{row.TelegramID}
		for _, admin := range row.Preferences {
			ids = append(ids, admin)
		}
		if err := p.requirePassUsers(ids...); err != nil {
			return err
		}
	}
	return nil
}
func (p *preparedPasses) requirePassUsers(ids ...int64) error {
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := p.Users[id]; !ok {
			return errors.New("pass_user_dependency_required")
		}
	}
	return nil
}
func (p *preparedPasses) validateEventPairs(rows []PassPlanRecord) error {
	byID := map[int64]*PassCandidate{}
	for _, row := range rows {
		if !row.Shadowed {
			byID[row.Candidate.TelegramID] = row.Candidate
		}
	}
	for _, b := range byID {
		if err := p.requirePassUsers(b.TelegramID, b.Admin, b.ReceivingAdmin, b.ReviewingAdmin); err != nil {
			return err
		}
		if b.Partner == 0 || b.State == registrationPending {
			continue
		}
		other := byID[b.Partner]
		if other == nil || other.Partner != b.TelegramID || other.State == registrationPending {
			return errors.New("pass_pair_conflict_correct_source_before_import")
		}
	}
	return nil
}
func (p *preparedPasses) loadPassProofs(stage string, limits Limits) error {
	root, report, err := verifiedUsersStage(stage, limits)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if report.ManifestSHA256 != p.Plan.ManifestSHA256 {
		return errors.New("passes_source_changed")
	}
	files := map[string]File{}
	for _, file := range p.Plan.Files {
		files[file.Path] = file
	}
	inventory, err := p.passProofInventory()
	if err != nil {
		return err
	}
	var total int64
	for _, row := range p.Plan.Records {
		b := row.Candidate
		if row.Excluded || b == nil || row.Shadowed || b.ProofFile == "" || b.ProofFile == registrationFree {
			continue
		}
		field := registrationProofField
		if row.Field != "" {
			field = row.Field + ".proof_file"
		}
		id, _ := recordID(row.Legacy.RecordID)
		key := row.Source + ":" + id + ":" + field
		proof, ok := inventory[key]
		if !ok || proof.TelegramFileID != b.ProofFile {
			return errors.New("pass_proof_reference_required")
		}
		payload, loadErr := p.readPassProof(root, files, proof, b.TelegramID)
		if loadErr != nil {
			return loadErr
		}
		total += int64(len(payload.Body))
		if total > maxUserPlanBytes {
			return errors.New("pass_proof_budget_exceeded")
		}
		p.Proofs[row.Legacy.Key] = payload
		delete(inventory, key)
	}
	return p.validateUnusedProofs(inventory)
}
func (p *preparedPasses) passProofInventory() (map[string]Proof, error) {
	inventory := map[string]Proof{}
	for _, proof := range p.Plan.Proofs {
		if proof.Field != registrationProofField && !strings.HasSuffix(proof.Field, ".proof_file") {
			continue
		}
		id, _ := recordID(proof.RecordID)
		key := proof.Source + ":" + id + ":" + proof.Field
		if _, ok := inventory[key]; ok {
			return nil, errors.New("pass_proof_duplicate")
		}
		inventory[key] = proof
	}
	return inventory, nil
}

func (p *preparedPasses) readPassProof(
	root *os.Root,
	files map[string]File,
	proof Proof,
	telegramID int64,
) (passProofPayload, error) {
	payload := passProofPayload{Reference: proof}
	ownerID, _ := recordID(proof.OwnerID)
	expectedOwner, _ := recordID(p.Users[telegramID].Legacy.RecordID)
	if ownerID != expectedOwner {
		return payload, errors.New("pass_proof_owner_mismatch")
	}
	if proof.Unavailable {
		return payload, nil
	}
	entry, exists := files[proof.Blob]
	if !exists || entry.Kind != orderBlobKind || entry.Bytes > maxOrderProofBytes || entry.Bytes <= 0 {
		return payload, errors.New("pass_proof_blob_invalid")
	}
	file, err := openRegular(root, entry.Path)
	if err != nil {
		return payload, err
	}
	body, err := io.ReadAll(io.LimitReader(file, entry.Bytes+1))
	_ = file.Close()
	if err != nil || int64(len(body)) != entry.Bytes || hashBytes(body) != entry.SHA256 {
		return payload, errors.New("pass_proof_blob_changed")
	}
	payload.Body = body
	payload.Hash = entry.SHA256
	return payload, nil
}
func (p *preparedPasses) validateUnusedProofs(inventory map[string]Proof) error {
	// Shadowed and other-bot evidence stays in the manifest, never attached to another registration.
	ignored := map[string]bool{}
	for _, row := range p.Plan.Records {
		if row.Shadowed || row.Excluded {
			id, _ := recordID(row.Legacy.RecordID)
			ignored[row.Source+":"+id] = true
		}
	}
	for _, proof := range inventory {
		id, _ := recordID(proof.RecordID)
		if !ignored[proof.Source+":"+id] {
			return errors.New("pass_proof_inventory_unresolved")
		}
	}
	return nil
}
func (p *preparedPasses) eventNames() []string {
	names := make([]string, 0, len(p.Bookings))
	for event := range p.Bookings {
		names = append(names, event)
	}
	for _, row := range p.Preferences {
		for event := range row.Preferences {
			if !slices.Contains(names, event) {
				names = append(names, event)
			}
		}
	}
	slices.Sort(names)
	return names
}
func passAttemptKey(b *PassCandidate) string {
	participant := b.TelegramID
	if b.Partner != 0 {
		participant = min(participant, b.Partner)
	}
	decision, reviewed, _ := passReviewFact(b)
	return b.Event + ":" + strconv.FormatInt(
		participant,
		10,
	) + ":" + b.ProofFile + ":" + passTimeKey(
		b.Received,
	) + ":" + strconv.FormatBool(
		b.ActiveReceipt,
	) + ":" + decision + ":" + passTimeKey(
		reviewed,
	)
}

func passTimeKey(at *time.Time) string {
	if at == nil {
		return ""
	}
	return at.UTC().Format(time.RFC3339Nano)
}
