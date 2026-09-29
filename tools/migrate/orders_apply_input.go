package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

type OrderResolutions struct {
	Version               int    `json:"version"`
	PlanSHA256            string `json:"plan_sha256"`
	DatesVerified         bool   `json:"dates_verified"`
	ConfigurationVerified bool   `json:"configuration_verified"`
	AdminGrantsVerified   bool   `json:"admin_grants_verified"`
	BotNamespaceVerified  bool   `json:"bot_namespace_verified"`
	WritersStopped        bool   `json:"writers_stopped"`
}
type preparedOrderEvent struct {
	Catalog OrderPlanRecord
	Orders  []OrderPlanRecord
	Slots   []OrderPlanRecord
}
type orderProofPayload struct {
	Reference Proof
	Body      []byte
	Hash      string
}
type preparedOrders struct {
	Plan              OrderPlan
	PlanHash          string
	ResolutionHash    string
	Events            []preparedOrderEvent
	Users             map[int64]OrderDependency
	EventDependencies map[string]OrderDependency
	Proofs            map[string]orderProofPayload
}

func prepareOrders(stage, planPath, resolutionsPath string, limits Limits) (preparedOrders, error) {
	p := preparedOrders{
		Users:             map[int64]OrderDependency{},
		EventDependencies: map[string]OrderDependency{},
		Proofs:            map[string]orderProofPayload{},
	}
	plan, generated, err := preparedOrderPlan(stage, limits)
	if err != nil {
		return p, err
	}
	original, err := readApplyFile(planPath, maxUserPlanBytes)
	if err != nil {
		return p, err
	}
	if !bytes.Equal(generated, original) {
		return p, errors.New("apply_plan_mismatch")
	}
	raw, err := readApplyFile(resolutionsPath, maxUserResolutionBytes)
	if err != nil {
		return p, err
	}
	if validJSON(raw) != nil {
		return p, errors.New("resolution_invalid")
	}
	if _, err = objectFields(
		raw,
		"version plan_sha256 dates_verified configuration_verified admin_grants_verified bot_namespace_verified writers_stopped",
	); err != nil {
		return p, errors.New("resolution_invalid")
	}
	var resolution OrderResolutions
	if json.Unmarshal(raw, &resolution) != nil || resolution.Version != 1 || !resolution.DatesVerified ||
		!resolution.ConfigurationVerified ||
		!resolution.AdminGrantsVerified ||
		!resolution.BotNamespaceVerified ||
		!resolution.WritersStopped {
		return p, errors.New("resolution_attestation_required")
	}
	p.Plan = plan
	p.PlanHash = hashBytes(original)
	p.ResolutionHash = hashBytes(raw)
	if resolution.PlanSHA256 != p.PlanHash {
		return p, errors.New("resolution_plan_mismatch")
	}
	if err = p.group(); err != nil {
		return p, err
	}
	if err = p.loadProofs(stage, limits); err != nil {
		return p, err
	}
	return p, nil
}

func (p *preparedOrders) group() error {
	if err := p.Plan.validateOrderCoverage(); err != nil {
		return err
	}
	if err := p.indexDependencies(); err != nil {
		return err
	}
	events := map[string]int{}
	for _, row := range p.Plan.Records {
		if len(row.Blockers) > 0 {
			return errors.New("apply_record_blocked")
		}
		if row.Catalog != nil {
			if _, exists := events[row.Catalog.EventID]; exists {
				return errors.New("order_catalog_duplicate")
			}
			events[row.Catalog.EventID] = len(p.Events)
			p.Events = append(p.Events, preparedOrderEvent{Catalog: row})
		}
	}
	if len(p.Events) == 0 {
		return errors.New("order_catalog_required")
	}
	if err := p.groupEventRecords(events); err != nil {
		return err
	}
	for _, event := range p.Events {
		if err := p.validateEvent(event); err != nil {
			return err
		}
	}
	return nil
}

func (p *preparedOrders) validateEvent(event preparedOrderEvent) error {
	catalog := event.Catalog.Catalog
	if _, exists := p.EventDependencies[catalog.EventID]; !exists {
		return errors.New("order_event_dependency_required")
	}
	admins, err := p.eventAdmins(catalog)
	if err != nil {
		return err
	}
	orders, err := p.eventOrders(event, admins)
	if err != nil {
		return err
	}
	seats := map[string]bool{}
	reservations := map[string]*OrderSlot{}
	if err = validateOrderSlots(event, orders, seats, reservations); err != nil {
		return err
	}
	for service, extra := range catalog.Extras {
		for seat := range extra.Capacity {
			if !seats[fmt.Sprintf("%s:%s:%d", catalog.EventID, service, seat)] {
				return errors.New("order_slot_inventory_incomplete")
			}
		}
	}
	return validatePaidOrderSlots(catalog, orders, reservations)
}

func orderSlotID(s *OrderSlot) string { return fmt.Sprintf("%s:%s:%d", s.EventID, s.Service, s.Seat) }
func orderChoiceExtras(raw json.RawMessage) map[string]json.RawMessage {
	var choice struct {
		Extras map[string]json.RawMessage `json:"extras"`
	}
	_ = json.Unmarshal(raw, &choice)
	return choice.Extras
}

func (p *preparedOrders) loadProofs(stage string, limits Limits) error {
	root, report, err := verifiedUsersStage(stage, limits)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	manifest, err := readVerifiedUsersManifest(root, report, limits)
	if err != nil {
		return err
	}
	if report.ManifestSHA256 != p.Plan.ManifestSHA256 {
		return errors.New("orders_source_changed")
	}
	if err = p.validateDelegatedProofs(root, manifest); err != nil {
		return err
	}
	files := map[string]File{}
	for _, file := range manifest.Files {
		files[file.Path] = file
	}
	proofs, err := p.proofIndex()
	if err != nil {
		return err
	}
	var total int64
	for _, event := range p.Events {
		for _, row := range event.Orders {
			o := row.Order
			if o.ProofFile == "" || o.ProofFile == orderStateCash {
				continue
			}
			if err = p.loadOrderProof(root, row, proofs, files, &total); err != nil {
				return err
			}
		}
	}
	if len(proofs) > 0 {
		return errors.New("order_proof_inventory_unresolved")
	}
	return nil
}
func orderSignedNumber(raw json.RawMessage) (int64, error) {
	id, err := recordID(raw)
	if err != nil || !strings.HasPrefix(id, "integer:") {
		return 0, errors.New("order_integer_invalid")
	}
	return strconv.ParseInt(strings.TrimPrefix(id, "integer:"), 10, 64)
}

func orderEventUsers(event preparedOrderEvent) []int64 {
	seen := map[int64]bool{}
	for _, admin := range event.Catalog.Catalog.Admins {
		seen[admin.TelegramID] = true
	}
	for _, row := range event.Orders {
		seen[row.Order.TelegramID] = true
	}
	users := make([]int64, 0, len(seen))
	for id := range seen {
		users = append(users, id)
	}
	slices.Sort(users)
	return users
}
func (p *preparedOrders) indexDependencies() error {
	for _, dependency := range p.Plan.Dependencies {
		if dependency.Source == usersSource {
			if _, exists := p.Users[dependency.TelegramID]; exists {
				return errors.New("order_dependency_duplicate")
			}
			p.Users[dependency.TelegramID] = dependency
		} else {
			if _, exists := p.EventDependencies[dependency.EventID]; exists {
				return errors.New("order_dependency_duplicate")
			}
			p.EventDependencies[dependency.EventID] = dependency
		}
	}
	return nil
}

func (p *preparedOrders) groupEventRecords(events map[string]int) error {
	for _, row := range p.Plan.Records {
		if row.Order != nil {
			index, exists := events[row.Order.EventID]
			if !exists {
				return errors.New("order_catalog_required")
			}
			p.Events[index].Orders = append(p.Events[index].Orders, row)
		}
		if row.Slot != nil {
			index, exists := events[row.Slot.EventID]
			if !exists {
				return errors.New("order_catalog_required")
			}
			p.Events[index].Slots = append(p.Events[index].Slots, row)
		}
	}
	return nil
}

func (p *preparedOrders) eventAdmins(catalog *OrderCatalog) (map[int64]OrderAdmin, error) {
	admins := map[int64]OrderAdmin{}
	for _, admin := range catalog.Admins {
		if _, exists := p.Users[admin.TelegramID]; !exists {
			return nil, errors.New("order_owner_dependency_required")
		}
		if _, exists := admins[admin.TelegramID]; exists {
			return nil, errors.New("order_admin_duplicate")
		}
		admins[admin.TelegramID] = admin
	}
	if catalog.PaymentAdminRU > 0 {
		admin, exists := admins[catalog.PaymentAdminRU]
		if !exists || admin.Country != "ru" {
			return nil, errors.New("order_ru_admin_dependency_required")
		}
	}
	return admins, nil
}

func (p *preparedOrders) eventOrders(
	event preparedOrderEvent,
	admins map[int64]OrderAdmin,
) (map[string]*OrderCandidate, error) {
	catalog := event.Catalog.Catalog
	orders := map[string]*OrderCandidate{}
	for _, row := range event.Orders {
		o := row.Order
		if _, exists := p.Users[o.TelegramID]; !exists {
			return nil, errors.New("order_owner_dependency_required")
		}
		if _, exists := orders[o.ID]; exists {
			return nil, errors.New("order_duplicate")
		}
		orders[o.ID] = o
		if err := validateOrderCatalogRelation(o, catalog, admins); err != nil {
			return nil, err
		}
	}
	return orders, nil
}

func validateOrderSlots(
	event preparedOrderEvent,
	orders map[string]*OrderCandidate,
	seats map[string]bool,
	reservations map[string]*OrderSlot,
) error {
	catalog := event.Catalog.Catalog
	for _, row := range event.Slots {
		s := row.Slot
		extra, exists := catalog.Extras[s.Service]
		if !exists || extra.Capacity <= 0 || s.Seat >= extra.Capacity {
			return errors.New("order_slot_configuration_mismatch")
		}
		key := orderSlotID(s)
		if seats[key] {
			return errors.New("order_slot_duplicate")
		}
		seats[key] = true
		if s.ReservationID != nil {
			key = s.Service + ":" + *s.ReservationID
			if _, exists = reservations[key]; exists {
				return errors.New("order_reservation_duplicate")
			}
			reservations[key] = s
			if err := validateOrderReservation(s, orders); err != nil {
				return err
			}
		}
	}
	return nil
}
func (p *preparedOrders) proofIndex() (map[string]Proof, error) {
	proofs := map[string]Proof{}
	for _, proof := range p.Plan.Proofs {
		if p.Plan.proofDelegated(proof) {
			continue
		}
		id, idErr := recordID(proof.RecordID)
		if idErr != nil || proof.Field != "proof_file" {
			return nil, errors.New("order_proof_inventory_unresolved")
		}
		if _, exists := proofs[id]; exists {
			return nil, errors.New("order_proof_duplicate")
		}
		proofs[id] = proof
	}
	return proofs, nil
}

func (p *preparedOrders) loadOrderProof(
	root *os.Root,
	row OrderPlanRecord,
	proofs map[string]Proof,
	files map[string]File,
	total *int64,
) error {
	o := row.Order
	id, _ := recordID(row.Legacy.RecordID)
	proof, exists := proofs[id]
	if !exists || proof.Unavailable || proof.TelegramFileID != o.ProofFile {
		return errors.New("order_proof_unavailable")
	}
	ownerID, _ := recordID(proof.OwnerID)
	expectedOwner, _ := recordID(p.Users[o.TelegramID].Legacy.RecordID)
	if ownerID != expectedOwner {
		return errors.New("order_proof_owner_mismatch")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(row.Record, &fields)
	chat, chatErr := orderSignedNumber(fields["proof_chat_id"])
	message, messageErr := orderSignedNumber(fields["proof_message_id"])
	if chatErr != nil || messageErr != nil || chat != proof.ChatID || message != proof.MessageID ||
		message <= 0 {
		return errors.New("order_proof_origin_mismatch")
	}
	file, exists := files[proof.Blob]
	if !exists || file.Kind != orderBlobKind {
		return errors.New("order_proof_unavailable")
	}
	if file.Bytes > maxOrderProofBytes {
		return errors.New("order_proof_outside_target_range")
	}
	*total += file.Bytes
	if *total > maxUserPlanBytes/2 {
		return errors.New("order_proof_payload_limit")
	}
	handle, openErr := openRegular(root, file.Path)
	if openErr != nil {
		return openErr
	}
	body, readErr := io.ReadAll(io.LimitReader(handle, file.Bytes+1))
	_ = handle.Close()
	if readErr != nil || int64(len(body)) != file.Bytes || hashBytes(body) != file.SHA256 {
		return errors.New("orders_source_changed")
	}
	p.Proofs[row.Legacy.Key] = orderProofPayload{Reference: proof, Body: body, Hash: file.SHA256}
	delete(proofs, id)
	return nil
}

func validateOrderCatalogRelation(o *OrderCandidate, catalog *OrderCatalog, admins map[int64]OrderAdmin) error {
	adminID := effectiveOrderAdmin(o, catalog)
	if adminID > 0 {
		admin, exists := admins[adminID]
		if !exists || admin.Country != o.Country {
			return errors.New("order_admin_dependency_required")
		}
	}
	if o.State == orderStateCash && (o.ProofAdmin == 0 || o.Country != "be") {
		return errors.New("order_cash_admin_required")
	}
	for key := range orderChoiceExtras(o.Choice) {
		if key != orderTotalKey {
			if _, exists := catalog.Extras[key]; !exists {
				return errors.New("order_extra_mapping_required")
			}
		}
	}
	if o.Country != "" && adminID == 0 {
		return errors.New("order_implicit_admin_mapping_required")
	}
	return nil
}

// Python routes RU receipts through effective configuration without persisting
// proof_admin. Keep the original source candidate intact and resolve only the route.
func effectiveOrderAdmin(o *OrderCandidate, catalog *OrderCatalog) int64 {
	if o.ProofAdmin > 0 {
		return o.ProofAdmin
	}
	if o.Country == "ru" && o.ProofFile != "" && o.ProofFile != orderStateCash &&
		(o.State == orderStateProof || o.State == orderStatePaid) {
		return catalog.PaymentAdminRU
	}
	return 0
}

func validateOrderReservation(s *OrderSlot, orders map[string]*OrderCandidate) error {
	if s.Attempt != nil {
		o, found := orders[*s.ReservationID]
		if !found || o.Attempt != *s.Attempt || o.AttemptAt == nil || s.AttemptAt == nil ||
			!o.AttemptAt.Equal(*s.AttemptAt) ||
			o.State != orderStateProof && o.State != orderStatePaid {
			return errors.New("order_reservation_attempt_unresolved")
		}
		if _, selected := orderChoiceExtras(o.Choice)[s.Service]; !selected {
			return errors.New("order_reservation_service_unselected")
		}
	}
	return nil
}

func validatePaidOrderSlots(
	catalog *OrderCatalog,
	orders map[string]*OrderCandidate,
	reservations map[string]*OrderSlot,
) error {
	for _, o := range orders {
		if o.State != orderStateProof && o.State != orderStatePaid {
			continue
		}
		for service := range orderChoiceExtras(o.Choice) {
			if catalog.Extras[service].Capacity > 0 {
				s := reservations[service+":"+o.ID]
				if s == nil || s.Attempt == nil {
					return errors.New("order_reservation_missing")
				}
			}
		}
	}
	return nil
}
