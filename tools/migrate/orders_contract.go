package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"
)

const (
	ordersSource             = "orders"
	orderConfigurationSource = "configuration"
	orderBlobKind            = "blob"
	orderStateProof          = "proof"
	orderStateCash           = "cash"
	orderStatePaid           = "paid"
	orderStateUnpaid         = "unpaid"
	orderTotalKey            = "total"
	maxOrderMoney            = 100000000000
	maxOrderCount            = 1000000
	orderCentsPerUnit        = 100
	maxOrderInstructions     = 1000
	maxOrderRegion           = 80
	orderPlanVersion         = 2
	orderBYNToRUB            = 30
	maxOrderEventBytes       = 100
	maxOrderProofBytes       = 20 << 20
)

// OrderCatalog is a reviewed effective configuration export, not a guessed
// conversion of arbitrary historical configuration resources.
type OrderCatalog struct {
	EventID        string                `json:"event_key"`
	Deadline       time.Time             `json:"deadline"`
	Menu           json.RawMessage       `json:"menu"`
	Extras         map[string]OrderExtra `json:"extras"`
	Instructions   string                `json:"transfer_instructions"`
	Localized      map[string]string     `json:"transfer_instructions_localized"`
	Admins         []OrderAdmin          `json:"payment_admins"`
	PaymentAdminRU int64                 `json:"payment_admin_ru,omitempty"`
}
type OrderExtra struct {
	Price    json.RawMessage `json:"price"`
	Capacity int             `json:"capacity,omitempty"`
	Legacy   bool            `json:"legacy,omitempty"`
}
type OrderAdmin struct {
	TelegramID int64  `json:"user_id"`
	Country    string `json:"country"`
	Region     string `json:"region"`
}
type OrderCandidate struct {
	ID            string          `json:"id"`
	EventID       string          `json:"event_id"`
	TelegramID    int64           `json:"telegram_id"`
	Choice        json.RawMessage `json:"choice"`
	State         string          `json:"state"`
	Attempt       string          `json:"attempt"`
	AttemptAt     *time.Time      `json:"attempt_at"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	ProofFile     string          `json:"proof_file"`
	ProofAdmin    int64           `json:"proof_admin"`
	ProofReceived *time.Time      `json:"proof_received"`
	Country       string          `json:"country"`
}
type OrderSlot struct {
	EventID       string     `json:"event_key"`
	Service       string     `json:"service"`
	Seat          int        `json:"seat"`
	ReservationID *string    `json:"reservation_id"`
	Attempt       *string    `json:"reservation_attempt_token"`
	AttemptAt     *time.Time `json:"reservation_attempt_created_at"`
	ReservedAt    *time.Time `json:"reserved_at"`
}

func orderString(fields map[string]json.RawMessage, key string, required bool) (string, error) {
	raw, exists := fields[key]
	if !exists && !required {
		return "", nil
	}
	var value string
	if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil || required && value == "" {
		return "", errors.New("order_string_invalid")
	}
	return value, nil
}
func orderTargetID(raw json.RawMessage, bot int64) (string, error) {
	id, err := recordID(raw)
	if err != nil || !strings.HasPrefix(id, "oid:") {
		return "", errors.New("order_object_id_required")
	}
	// A constant bot prefix preserves Mongo ObjectID lexical priority ties.
	return "legacy-order:" + strconv.FormatInt(bot, 10) + ":" + strings.TrimPrefix(id, "oid:"), nil
}
func convertOrder(raw []byte, bot int64) (*OrderCandidate, error) {
	f, err := objectFields(
		raw,
		"_id user_id event_key choice created_at updated_at proof_file proof_country proof_admin proof_received proof_chat_id proof_message_id cash_requested_at payment_attempt_token payment_attempt_created_at validated_at validation _migration",
	)
	if err != nil {
		return nil, errors.New("order_field_unmapped")
	}
	o := &OrderCandidate{State: orderStateUnpaid}
	if o.ID, err = orderTargetID(f["_id"], bot); err != nil {
		return nil, err
	}
	if o.EventID, err = orderString(
		f,
		"event_key",
		true,
	); err != nil || !tokenPattern.MatchString(o.EventID) ||
		len(o.EventID) > maxOrderEventBytes {
		return nil, errors.New("order_event_invalid")
	}
	var ok bool
	if o.TelegramID, ok = telegramNumber(f["user_id"]); !ok {
		return nil, errors.New("order_owner_invalid")
	}
	if o.CreatedAt, err = eventInstant(f["created_at"]); err != nil {
		return nil, errors.New("order_datetime_unresolved")
	}
	o.UpdatedAt = o.CreatedAt
	if v, exists := f["updated_at"]; exists {
		if o.UpdatedAt, err = eventInstant(v); err != nil {
			return nil, errors.New("order_datetime_unresolved")
		}
	}
	if err = validateOrderChoice(f["choice"]); err != nil {
		return nil, err
	}
	o.Choice = f["choice"]
	if err = o.paymentFields(f); err != nil {
		return nil, err
	}
	return o, nil
}

func (o *OrderCandidate) paymentFields(f map[string]json.RawMessage) error {
	var err error
	var ok bool
	if o.ProofFile, err = orderProofFile(f); err != nil {
		return err
	}
	if o.Country, err = orderString(
		f,
		"proof_country",
		false,
	); err != nil ||
		o.Country != "" && o.Country != "be" && o.Country != "ru" {
		return errors.New("order_country_invalid")
	}
	if v, exists := f["proof_admin"]; exists {
		if o.ProofAdmin, ok = telegramNumber(v); !ok {
			return errors.New("order_admin_invalid")
		}
	}
	if o.Attempt, err = orderString(f, "payment_attempt_token", false); err != nil {
		return err
	}
	dates, err := orderPaymentDates(f)
	if err != nil {
		return err
	}
	validated := false
	if _, exists := f["validation"]; exists {
		if err = eventBool(f, "validation", &validated); err != nil {
			return errors.New("order_validation_unresolved")
		}
	}
	if o.ProofFile == orderStateCash {
		o.State = orderStateCash
	} else if o.ProofFile != "" {
		o.State = orderStateProof
	} else if _, exists := dates["cash_requested_at"]; exists {
		o.State = orderStateCash
	}
	if validated {
		o.State = orderStatePaid
	}
	if err = o.validateSourcePaymentFields(f); err != nil {
		return err
	}
	if err = o.resolveLegacyValidation(f, dates); err != nil {
		return err
	}
	return o.setPaymentTimes(dates)
}

func (o *OrderCandidate) validateSourcePaymentFields(fields map[string]json.RawMessage) error {
	if o.State == orderStateCash && len(fields["proof_file"]) > 0 && o.ProofFile != orderStateCash {
		return errors.New("order_cash_origin_unresolved")
	}
	if _, exists := fields["payment_attempt_token"]; exists && o.Attempt == "" && o.State != orderStateUnpaid {
		return errors.New("order_empty_attempt_unresolved")
	}
	return nil
}

func orderProofFile(fields map[string]json.RawMessage) (string, error) {
	raw := fields["proof_file"]
	if bytes.Equal(raw, []byte("null")) || bytes.Equal(raw, []byte("false")) {
		return "", nil
	}
	return orderString(fields, "proof_file", false)
}

func (o *OrderCandidate) setPaymentTimes(dates map[string]time.Time) error {
	if o.State == orderStateUnpaid {
		if o.Attempt != "" || len(dates) > 0 {
			return errors.New("order_historical_payment_mapping_required")
		}
		return nil
	}
	actualProof := o.ProofFile != "" && o.ProofFile != orderStateCash
	if o.Attempt == "" && actualProof {
		o.Attempt = "legacy-proof:" + o.ProofFile
	}
	if o.Attempt == "" && o.State != orderStateCash {
		return errors.New("order_legacy_attempt_mapping_required")
	}
	at := orderPaymentTime(dates, o.CreatedAt)
	o.AttemptAt = &at
	if actualProof {
		received, exists := dates["proof_received"]
		if !exists {
			received = at
		}
		o.ProofReceived = &received
	}
	return nil
}

// Match Python payment_attempt_time; cash_requested_at is not a priority date.
func orderPaymentTime(dates map[string]time.Time, created time.Time) time.Time {
	for _, key := range []string{"payment_attempt_created_at", "proof_received", "validated_at"} {
		if at, exists := dates[key]; exists {
			return at
		}
	}
	return created
}

func orderMoney(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 || raw[0] == '"' {
		return 0, errors.New("order_money_invalid")
	}
	r, ok := new(big.Rat).SetString(string(raw))
	if !ok {
		return 0, errors.New("order_money_invalid")
	}
	r.Mul(r, big.NewRat(orderCentsPerUnit, 1))
	if !r.IsInt() || !r.Num().IsInt64() || r.Sign() < 0 || r.Num().Int64() > maxOrderMoney {
		return 0, errors.New("order_money_invalid")
	}
	return r.Num().Int64(), nil
}

// Preserve historical unit prices, including removed dishes; never canonicalize
// the saved choice against today's catalog. Only check exact target arithmetic.
func validateOrderChoice(raw json.RawMessage) error {
	f, err := objectFields(raw, "customer customer_first_name customer_last_name customer_patronymus days extras total")
	if err != nil {
		return errors.New("order_choice_unmapped")
	}
	for _, key := range []string{"customer", "customer_first_name", "customer_last_name", "customer_patronymus"} {
		if _, err = orderString(f, key, false); err != nil {
			return err
		}
	}
	meals, err := orderDaysTotal(f["days"])
	if err != nil {
		return err
	}
	var extras map[string]json.RawMessage
	if json.Unmarshal(f["extras"], &extras) != nil || extras == nil {
		return errors.New("order_choice_unmapped")
	}
	var sum int64
	for key, price := range extras {
		if !tokenPattern.MatchString(key) {
			return errors.New("order_choice_unmapped")
		}
		v, moneyErr := orderMoney(price)
		if moneyErr != nil {
			return moneyErr
		}
		if key != orderTotalKey {
			sum += v
		}
	}
	total, err := orderMoney(f[orderTotalKey])
	if err != nil {
		return err
	}
	extraTotal, err := orderMoney(extras[orderTotalKey])
	if err != nil {
		return err
	}
	if total != sum+meals || extraTotal != sum {
		return errors.New("order_total_inconsistent")
	}
	return nil
}

func convertOrderSlot(raw []byte, bot int64) (*OrderSlot, error) {
	f, err := objectFields(
		raw,
		"_id event_key service seat reservation_id reservation_attempt_token reservation_attempt_created_at reserved_at",
	)
	if err != nil {
		return nil, errors.New("order_slot_field_unmapped")
	}
	s := &OrderSlot{}
	if s.EventID, err = orderString(f, "event_key", true); err != nil {
		return nil, err
	}
	if s.Service, err = orderString(f, "service", true); err != nil {
		return nil, err
	}
	seat, seatErr := orderSignedNumber(f["seat"])
	if seatErr != nil || seat < 0 || seat > maxOrderCount {
		return nil, errors.New("order_slot_invalid")
	}
	s.Seat = int(seat)
	if v, exists := f["reservation_id"]; exists {
		id, idErr := orderTargetID(v, bot)
		if idErr != nil {
			return nil, idErr
		}
		s.ReservationID = &id
	}
	if _, exists := f["reservation_attempt_token"]; exists {
		token, tokenErr := orderString(f, "reservation_attempt_token", true)
		if tokenErr != nil {
			return nil, tokenErr
		}
		s.Attempt = &token
	}
	for key, target := range map[string]**time.Time{"reservation_attempt_created_at": &s.AttemptAt, "reserved_at": &s.ReservedAt} {
		if v, exists := f[key]; exists {
			at, dateErr := eventInstant(v)
			if dateErr != nil {
				return nil, errors.New("order_datetime_unresolved")
			}
			*target = &at
		}
	}
	if s.ReservationID == nil && (s.Attempt != nil || s.AttemptAt != nil || s.ReservedAt != nil) {
		return nil, errors.New("order_slot_invalid")
	}
	return s, nil
}

func convertOrderCatalog(raw []byte) (*OrderCatalog, error) {
	f, err := objectFields(
		raw,
		"_id kind event_key deadline menu extras transfer_instructions transfer_instructions_localized payment_admins payment_admin_ru byn_to_rub",
	)
	if err != nil {
		return nil, errors.New("order_configuration_unmapped")
	}
	kind, err := orderString(f, "kind", true)
	if err != nil || kind != "orders_catalog_v1" {
		return nil, errors.New("order_configuration_unmapped")
	}
	if rate, rateErr := orderSignedNumber(f["byn_to_rub"]); rateErr != nil || rate != orderBYNToRUB {
		return nil, errors.New("order_exchange_rate_unmapped")
	}
	c := &OrderCatalog{}
	if c.PaymentAdminRU, err = orderConfiguredRUAdmin(f); err != nil {
		return nil, err
	}
	if c.EventID, err = orderString(
		f,
		"event_key",
		true,
	); err != nil || !tokenPattern.MatchString(c.EventID) ||
		len(c.EventID) > maxOrderEventBytes {
		return nil, errors.New("order_event_invalid")
	}
	if c.Deadline, err = eventInstant(f["deadline"]); err != nil {
		return nil, errors.New("order_datetime_unresolved")
	}
	if err = validateOrderMenu(f["menu"]); err != nil {
		return nil, err
	}
	c.Menu = f["menu"]
	c.Extras, err = orderCatalogExtras(f["extras"])
	if err != nil {
		return nil, err
	}
	if c.Instructions, err = orderString(
		f,
		"transfer_instructions",
		false,
	); err != nil ||
		len([]rune(c.Instructions)) > maxOrderInstructions {
		return nil, errors.New("order_instructions_invalid")
	}
	if json.Unmarshal(f["transfer_instructions_localized"], &c.Localized) != nil || c.Localized == nil {
		return nil, errors.New("order_instructions_invalid")
	}
	for _, value := range c.Localized {
		if len([]rune(value)) > maxOrderInstructions {
			return nil, errors.New("order_instructions_invalid")
		}
	}
	c.Admins, err = orderCatalogAdmins(f["payment_admins"])
	if err != nil {
		return nil, err
	}
	return c, nil
}

func orderConfiguredRUAdmin(fields map[string]json.RawMessage) (int64, error) {
	value, exists := fields["payment_admin_ru"]
	if !exists {
		return 0, nil
	}
	id, err := orderSignedNumber(value)
	if err != nil || id < 0 {
		return 0, errors.New("order_ru_admin_invalid")
	}
	return id, nil
}

func orderPaymentDates(f map[string]json.RawMessage) (map[string]time.Time, error) {
	dates := map[string]time.Time{}
	for _, key := range []string{"payment_attempt_created_at", "proof_received", "cash_requested_at", "validated_at"} {
		if v, exists := f[key]; exists {
			parsed, dateErr := eventInstant(v)
			if dateErr != nil {
				return nil, errors.New("order_datetime_unresolved")
			}
			dates[key] = parsed
		}
	}
	return dates, nil
}

func orderCatalogExtras(raw json.RawMessage) (map[string]OrderExtra, error) {
	var extras map[string]json.RawMessage
	if json.Unmarshal(raw, &extras) != nil || extras == nil {
		return nil, errors.New("order_extras_invalid")
	}
	result := map[string]OrderExtra{}
	for key, rawExtra := range extras {
		fields, fieldErr := objectFields(rawExtra, "price capacity legacy")
		if fieldErr != nil || !tokenPattern.MatchString(key) || key == orderTotalKey {
			return nil, errors.New("order_extras_invalid")
		}
		var extra OrderExtra
		if json.Unmarshal(rawExtra, &extra) != nil || extra.Capacity < 0 || extra.Capacity > maxOrderCount {
			return nil, errors.New("order_extras_invalid")
		}
		if value, exists := fields["capacity"]; exists && bytes.Equal(value, []byte("null")) {
			return nil, errors.New("order_extras_invalid")
		}
		if err := eventBool(fields, "legacy", &extra.Legacy); err != nil {
			return nil, errors.New("order_extras_invalid")
		}
		if _, err := orderMoney(fields["price"]); err != nil {
			return nil, err
		}
		result[key] = extra
	}
	return result, nil
}

func orderCatalogAdmins(raw json.RawMessage) ([]OrderAdmin, error) {
	var err error
	result := []OrderAdmin{}
	var admins []json.RawMessage
	if json.Unmarshal(raw, &admins) != nil || admins == nil {
		return nil, errors.New("order_admin_invalid")
	}
	for _, rawAdmin := range admins {
		af, adminErr := objectFields(rawAdmin, "user_id country region")
		if adminErr != nil {
			return nil, errors.New("order_admin_invalid")
		}
		a := OrderAdmin{}
		var ok bool
		if a.TelegramID, ok = telegramNumber(af["user_id"]); !ok {
			return nil, errors.New("order_admin_invalid")
		}
		if a.Country, err = orderString(af, "country", true); err != nil || a.Country != "be" && a.Country != "ru" {
			return nil, errors.New("order_admin_invalid")
		}
		if a.Region, err = orderString(af, "region", false); err != nil || len([]rune(a.Region)) > maxOrderRegion {
			return nil, errors.New("order_admin_invalid")
		}
		result = append(result, a)
	}
	return result, nil
}
func orderDaysTotal(raw json.RawMessage) (int64, error) {
	var days map[string]json.RawMessage
	if json.Unmarshal(raw, &days) != nil || days == nil {
		return 0, errors.New("order_meal_mapping_required")
	}
	var meals int64
	for _, day := range days {
		value, dayErr := orderDayTotal(day)
		if dayErr != nil {
			return 0, dayErr
		}
		meals += value
		if meals > maxOrderMoney {
			return 0, errors.New("order_money_invalid")
		}
	}
	return meals, nil
}
