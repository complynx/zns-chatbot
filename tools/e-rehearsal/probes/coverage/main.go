// Copy only into the isolated platform/cmd/e-runtime-coverage/main.go after importer retirement.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

const owner = "owner-202"
const admin = "owner-101"
const other = "owner-303"
const event = "event_one"
const modern = "legacy-order:999:000000000000000000000001"
const unpaid = "legacy-order:999:000000000000000000000002"
const cash = "legacy-order:999:000000000000000000000003"
const foodID = "legacy-food:999:100000000000000000000001"
const argumentCount = 3
const probeTimeout = 60 * time.Second
const syntheticBot = 999
const orderCount = 3
const deniedPolicyLimit = 500000000
const paidState = "paid"

type expected struct {
	Host        string `json:"host"`
	Port        uint16 `json:"port"`
	Role        string `json:"role"`
	Transport   string `json:"transport"`
	Database    string `json:"database"`
	Marker      string `json:"marker"`
	ModernSHA   string `json:"modern_sha256"`
	PairSHA     string `json:"pair_sha256"`
	MealSHA     string `json:"meal_sha256"`
	ActivitySHA string `json:"activity_sha256"`
	MenuSHA     string `json:"menu_sha256"`
}

func main() {
	if err := run(os.Args); err != nil {
		// Output contains no imported text, proofs, identity or database secrets.
		fmt.Fprintln(os.Stderr, "e_runtime_coverage_failed")
		os.Exit(1)
	}
	fmt.Fprintln(
		os.Stdout,
		`{"runtime_coverage":"passed","orders":true,"passes":true,"food":true,"profile":true,"identity":true,"credits":true}`,
	)
}

func digest(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
func deny(err error) error {
	var p *core.ProblemError
	if !errors.As(err, &p) || (p.Status != 403 && p.Status != 404) {
		return errors.New("current_acl_denial_required")
	}
	return nil
}
func matchProof(proof orders.Proof, err error, sha string) error {
	if err != nil {
		return err
	}
	if proof.ID == "" || digest(proof.Body) != sha {
		return errors.New("source_proof_bytes_mismatch")
	}
	return nil
}

func run(arguments []string) error {
	if len(arguments) != argumentCount {
		return errors.New("expected_projection_required")
	}
	spec, err := loadExpected(arguments[1], arguments[2])
	if err != nil {
		return err
	}
	config, err := runtimeConfig(spec.Host, spec.Port, spec.Database, spec.Role, spec.Transport)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return err
	}
	defer db.Close()
	if err = restrictedDatabase(ctx, db, spec); err != nil {
		return err
	}
	if err = identityAndProfiles(ctx, db); err != nil {
		return err
	}
	if err = modernOrders(ctx, db, spec); err != nil {
		return err
	}
	if err = eventPasses(ctx, db, spec); err != nil {
		return err
	}
	if err = legacyFood(ctx, db, spec); err != nil {
		return err
	}
	return creditPolicy(ctx, db)
}

func loadExpected(path, expectedHash string) (expected, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return expected{}, err
	}
	defer root.Close()
	raw, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		return expected{}, err
	}
	if digest(raw) != expectedHash {
		return expected{}, errors.New("reviewed_probe_input_hash_required")
	}
	var spec expected
	if err = json.Unmarshal(raw, &spec); err != nil {
		return expected{}, err
	}
	if !strings.HasPrefix(spec.Database, "synthetic_qa_zns_") ||
		spec.Marker != "qa.e-import-removal.20261001.synthetic-only" {
		return expected{}, errors.New("explicit_synthetic_probe_binding_required")
	}
	for _, sha := range []string{spec.ModernSHA, spec.PairSHA, spec.MealSHA, spec.ActivitySHA, spec.MenuSHA} {
		if len(sha) != hex.EncodedLen(sha256.Size) {
			return expected{}, errors.New("complete_expected_projection_required")
		}
	}
	return spec, nil
}

// runtimeConfig binds the supported loopback transport before pool creation.
func runtimeConfig(host string, port uint16, database, role, transport string) (*pgxpool.Config, error) {
	rejected := errors.New("allocated_runtime_target_required")
	if transport != "host-loopback" || (host != "127.0.0.1" && host != "localhost") || port < 1024 ||
		role != "zns_app" {
		return nil, rejected
	}
	for _, setting := range os.Environ() {
		key, _, _ := strings.Cut(setting, "=")
		if strings.HasPrefix(strings.ToUpper(key), "PG") {
			return nil, rejected
		}
	}
	raw := os.Getenv("E_RUNTIME_DATABASE_URL")
	if strings.ContainsAny(raw, " \t\r\n") {
		return nil, rejected
	}
	uri, err := url.Parse(raw)
	if err != nil || (uri.Scheme != "postgres" && uri.Scheme != "postgresql") || uri.User == nil ||
		uri.Hostname() != host || uri.Port() != strconv.Itoa(int(port)) || uri.Path != "/"+database ||
		uri.User.Username() != role || uri.Fragment != "" || uri.ForceQuery ||
		(uri.RawQuery != "" && uri.RawQuery != "sslmode=disable") {
		return nil, rejected
	}
	// This transport has one plaintext loopback endpoint, never SSL fallback.
	uri.RawQuery = "sslmode=disable"
	config, err := pgxpool.ParseConfig(uri.String())
	if err != nil {
		return nil, rejected
	}
	effective := config.ConnConfig
	if effective.Host != host || effective.Port != port || effective.Database != database || effective.User != role ||
		effective.TLSConfig != nil || len(effective.Fallbacks) != 0 || len(effective.RuntimeParams) != 0 {
		return nil, rejected
	}
	// localhost is an explicitly supported loopback alias, not ambient DNS authority.
	effective.LookupFunc = func(_ context.Context, name string) ([]string, error) {
		if name != host {
			return nil, rejected
		}
		return []string{"127.0.0.1"}, nil
	}
	return config, nil
}

func restrictedDatabase(ctx context.Context, db *pgxpool.Pool, spec expected) error {
	var database, role, marker string
	var absent bool
	err := db.QueryRow(ctx, `SELECT current_database(),current_user,to_regnamespace('migrate_import') IS NULL,
 coalesce((SELECT description FROM pg_shdescription WHERE objoid=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classoid='pg_database'::regclass),'')`).Scan(&database, &role, &absent, &marker)
	if err != nil {
		return err
	}
	if database != spec.Database || role != "zns_app" || !absent || marker != spec.Marker {
		return errors.New("isolated_removed_importer_runtime_required")
	}
	return nil
}

func identityAndProfiles(ctx context.Context, db *pgxpool.Pool) error {
	for _, mapping := range []struct {
		owner    string
		telegram int64
		language string
	}{{admin, 101, "en"}, {owner, 202, "ru"}, {other, 303, "en"}} {
		var valid bool
		err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.users u JOIN core.telegram_identities t ON t.owner=u.id
 JOIN core.zitadel_identities z ON z.owner=u.id JOIN core.legacy_user_references r ON r.owner=u.id
 WHERE u.id=$1 AND u.telegram_id=$2 AND u.language=$3 AND u.can_book AND t.bot_id=999 AND t.telegram_id=$2
 AND z.issuer='https://synthetic.invalid' AND z.subject=$4)`, mapping.owner, mapping.telegram, mapping.language, fmt.Sprintf("synthetic-e-user-%d", mapping.telegram)).
			Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return errors.New("permanent_identity_projection_mismatch")
		}
	}
	service := passes.Service{DB: db}
	profile, err := service.Get(ctx, owner)
	if err != nil {
		return err
	}
	if profile.Owner != owner || profile.LegalName != "Synthetic Client Name" ||
		profile.Passport != "SYNTHETIC-NOT-A-DOCUMENT" ||
		profile.Frozen ||
		profile.Pending != "" {
		return errors.New("profile_projection_mismatch")
	}
	observer, err := service.Get(ctx, other)
	if err != nil {
		return err
	}
	if observer.Owner != other || observer.LegalName != "" || observer.Passport != "" {
		return errors.New("profile_owner_isolation_mismatch")
	}
	_, err = service.Get(ctx, "e-unknown-owner")
	return deny(err)
}

func modernOrders(ctx context.Context, db *pgxpool.Pool, spec expected) error {
	service := orders.Service{DB: db, LegacyBotID: syntheticBot}
	list, err := service.List(ctx, owner, event)
	if err != nil {
		return err
	}
	if len(list) != orderCount {
		return errors.New("order_list_projection_mismatch")
	}
	for _, want := range []struct {
		id, state string
		total     orders.Money
	}{{modern, paidState, 7499}, {unpaid, "unpaid", 3500}, {cash, "cash", 3500}} {
		order, readErr := service.Get(ctx, owner, event, want.id)
		if readErr != nil {
			return readErr
		}
		if order.Owner != owner || order.ID != want.id || order.State != want.state ||
			order.Choice.Total != want.total {
			return errors.New("order_projection_mismatch")
		}
		_, readErr = service.Get(ctx, other, event, want.id)
		if err = deny(readErr); err != nil {
			return err
		}
	}
	return modernReceipt(ctx, db, service, spec)
}

func modernReceipt(ctx context.Context, db *pgxpool.Pool, service orders.Service, spec expected) error {
	paid, err := service.Get(ctx, owner, event, modern)
	if err != nil {
		return err
	}
	meal := paid.Choice.Days["old-day"].Mealtimes["lunch"]
	if len(meal.Dishes) != 1 || meal.Dishes[0].Name != "historical-removed" || meal.Dishes[0].Price != 999 {
		return errors.New("historical_order_choice_mismatch")
	}
	proof, err := service.OrderProof(ctx, owner, event, modern)
	if err = matchProof(proof, err, spec.ModernSHA); err != nil {
		return err
	}
	proof, err = service.OrderProof(ctx, admin, event, modern)
	if err = matchProof(proof, err, spec.ModernSHA); err != nil {
		return err
	}
	_, err = service.OrderProof(ctx, other, event, modern)
	if err = deny(err); err != nil {
		return err
	}
	review, err := service.ReviewOrder(ctx, admin, event, cash)
	if err != nil {
		return err
	}
	if review.Owner != owner || review.Country != "be" || review.PaymentAdmin != admin {
		return errors.New("cash_review_projection_mismatch")
	}
	_, err = service.ReviewOrder(ctx, other, event, cash)
	if err = deny(err); err != nil {
		return err
	}
	page, err := service.ListPage(ctx, admin, event, "", true)
	if err != nil {
		return err
	}
	if len(page.Orders) != 1 || page.Orders[0].ID != cash || page.Next != "" {
		return errors.New("current_payment_inbox_mismatch")
	}
	return checkCapacity(ctx, db)
}

func checkCapacity(ctx context.Context, db *pgxpool.Pool) error {
	var capacity bool
	err := db.QueryRow(ctx, `SELECT count(*)=2 AND count(*) FILTER(WHERE seat=0 AND reservation_id='legacy-order:999:000000000000000000000999')=1
 AND count(*) FILTER(WHERE seat=1 AND reservation_id=$2 AND reservation_attempt_token='attempt-one')=1
 FROM core.order_capacity_slots WHERE event_id=$1 AND service='shuttle'`, event, modern).
		Scan(&capacity)
	if err != nil {
		return err
	}
	if !capacity {
		return errors.New("source_capacity_projection_mismatch")
	}
	return nil
}

func eventPasses(ctx context.Context, db *pgxpool.Pool, spec expected) error {
	service := passbooking.Service{DB: db}
	catalog, err := service.Events(ctx, owner)
	if err != nil {
		return err
	}
	if len(catalog) != 1 || catalog[0].ID != event || !catalog[0].PassportRequired ||
		catalog[0].Titles["ru"] != "Синтетическая проверка удаления" ||
		catalog[0].Titles["en"] != "Synthetic removal rehearsal" {
		return errors.New("bilingual_event_projection_mismatch")
	}
	for _, want := range []struct {
		owner, partner, role string
		price                int
	}{{admin, owner, "leader", 100}, {owner, admin, "follower", 100}, {other, "", "leader", 0}} {
		booking, getErr := service.Get(ctx, want.owner, event)
		if getErr != nil {
			return getErr
		}
		if booking.Owner != want.owner || booking.State != paidState || booking.Partner != want.partner ||
			string(booking.Role) != want.role ||
			booking.Price == nil ||
			*booking.Price != want.price ||
			booking.PaymentAdmin != admin {
			return errors.New("pass_relationship_projection_mismatch")
		}
	}
	return passPayments(ctx, service, spec)
}

func passPayments(ctx context.Context, service passbooking.Service, spec expected) error {
	first, err := service.Payment(ctx, admin, event, admin)
	if err != nil {
		return err
	}
	second, err := service.Payment(ctx, owner, event, owner)
	if err != nil {
		return err
	}
	if first.Attempt == "" || first.Attempt != second.Attempt || first.Submitter != nil || second.Submitter != nil ||
		first.ProofID == "" ||
		first.ProofID != second.ProofID ||
		first.Decision != "pending" ||
		second.Decision != "pending" {
		return errors.New("shared_receipt_projection_mismatch")
	}
	for _, participant := range []string{admin, owner} {
		proof, proofErr := service.PaymentProof(ctx, participant, event, participant)
		if err = matchProof(proof, proofErr, spec.PairSHA); err != nil {
			return err
		}
	}
	_, err = service.PaymentProof(ctx, other, event, owner)
	if err = deny(err); err != nil {
		return err
	}
	return freePass(ctx, service)
}

func freePass(ctx context.Context, service passbooking.Service) error {
	free, err := service.Payment(ctx, other, event, other)
	if err != nil {
		return err
	}
	if free.Kind != "free" || free.Decision != "accepted" || free.Attempt != "" || free.ProofID != "" ||
		free.ReceivingAdmin != admin {
		return errors.New("embedded_free_payment_projection_mismatch")
	}
	_, err = service.Get(ctx, "e-unknown-owner", event)
	return deny(err)
}

func legacyFood(ctx context.Context, db *pgxpool.Pool, spec expected) error {
	service := legacyfood.Service{DB: db, BotID: syntheticBot}
	view, err := service.View(ctx, owner, event, foodID)
	if err != nil {
		return err
	}
	order := view.Order
	if err = foodProjection(view, spec); err != nil {
		return err
	}
	quote, err := service.Quote(ctx, owner, event, order.Meals)
	if err != nil {
		return err
	}
	if quote.Total != 18500 || !quote.Complete {
		return errors.New("current_food_quote_mismatch")
	}
	review, err := service.ReviewView(ctx, admin, event, foodID)
	if err != nil {
		return err
	}
	if review.Order.Owner != owner {
		return errors.New("food_admin_review_mismatch")
	}
	_, err = service.View(ctx, other, event, foodID)
	if err = deny(err); err != nil {
		return err
	}
	return foodProofs(ctx, service, order, spec)
}

func foodProjection(view legacyfood.View, spec expected) error {
	order := view.Order
	if order.ID != foodID || order.Owner != owner || order.MealTotal != 18499 || order.ActivityTotal != 250000 ||
		!order.Complete ||
		order.PaymentAdmin != admin ||
		order.MealPayment.Status != "rejected" ||
		order.ActivityPayment.Status != paidState ||
		!order.Activities["open"] ||
		!order.Activities["cacao"] ||
		order.MealPayment.RejectedBy != admin ||
		order.ActivityPayment.ConfirmedBy != admin ||
		view.Event.MenuSHA256 != spec.MenuSHA ||
		view.Instructions["ru"] != "Только тест" ||
		view.Instructions["en"] != "Synthetic only" {
		return errors.New("food_projection_mismatch")
	}
	return nil
}

func foodProofs(ctx context.Context, service legacyfood.Service, order legacyfood.Order, spec expected) error {
	for _, part := range []struct {
		kind, sha  string
		generation int64
	}{{legacyfood.Meals, spec.MealSHA, order.MealPayment.Generation}, {legacyfood.Activity, spec.ActivitySHA, order.ActivityPayment.Generation}} {
		for _, actor := range []string{owner, admin} {
			proof, proofErr := service.Proof(ctx, actor, event, foodID, part.kind, part.generation)
			if err := matchProof(proof, proofErr, part.sha); err != nil {
				return err
			}
		}
		_, err := service.Proof(ctx, other, event, foodID, part.kind, part.generation)
		if err = deny(err); err != nil {
			return err
		}
	}
	return nil
}

func creditPolicy(ctx context.Context, db *pgxpool.Pool) error {
	service := credits.Service{DB: db, Enforce: true}
	for _, actor := range []string{admin, owner, other} {
		permissions, err := service.Permissions(ctx, actor)
		if err != nil {
			return err
		}
		if permissions["admin"] {
			return errors.New("event_admin_escalated_to_credit_admin")
		}
	}
	report, err := service.Usage(ctx, owner, owner)
	if err != nil {
		return err
	}
	if report.Policy.Payer != owner || !report.Policy.Inherited || report.Policy.Unlimited ||
		report.Policy.ImplicitUnlimited ||
		report.Policy.MonthlyNanoUSD != 1000000000 ||
		report.SpentNanoUSD != 0 ||
		report.HeldNanoUSD != 0 ||
		report.UnboundedUnknown != 0 ||
		report.AvailableNanoUSD == nil ||
		*report.AvailableNanoUSD != 1000000000 {
		return errors.New("imported_owner_credit_policy_mismatch")
	}
	for _, actor := range []string{admin, other} {
		_, err = service.Usage(ctx, actor, owner)
		if err = deny(err); err != nil {
			return err
		}
		_, err = service.DefaultPolicy(ctx, actor)
		if err = deny(err); err != nil {
			return err
		}
	}
	limit := int64(deniedPolicyLimit)
	_, err = service.SetPolicy(
		ctx,
		admin,
		owner,
		credits.PolicyChange{MonthlyNanoUSD: &limit, Version: 1, OperationKey: "e-runtime-denied-credit-policy"},
	)
	if err = deny(err); err != nil {
		return err
	}
	history, err := service.History(ctx, owner, owner, "")
	if err != nil {
		return err
	}
	if len(history.Items) != 0 {
		return errors.New("unexpected_paid_attempts")
	}
	_, err = service.History(ctx, other, owner, "")
	return deny(err)
}
