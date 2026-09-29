package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/jackc/pgx/v5"
)

func resolveFoodDependencies(
	ctx context.Context,
	tx pgx.Tx,
	p preparedFood,
	event preparedFoodEvent,
) (map[int64]string, error) {
	if err := resolveFoodEventDependency(ctx, tx, p, event); err != nil {
		return nil, err
	}
	actors := map[int64]bool{}
	for _, admin := range event.Catalog.Configuration.Admins {
		actors[admin.Owner] = true
	}
	for _, row := range event.Orders {
		o := row.Food
		for _, id := range []int64{o.Owner, o.Admin, o.MealPayment.ConfirmedBy, o.MealPayment.RejectedBy, o.ActivityPayment.ConfirmedBy, o.ActivityPayment.RejectedBy} {
			if id > 0 {
				actors[id] = true
			}
		}
	}
	for _, marker := range event.Markers {
		actors[marker.Owner] = true
	}
	owners := map[int64]string{}
	for id := range actors {
		user := p.Users[id]
		var owner string
		if err := tx.QueryRow(ctx, `SELECT r.owner FROM core.legacy_user_references r JOIN core.telegram_identities t ON t.owner=r.owner JOIN core.users u ON u.id=r.owner
WHERE r.source_key=$1 AND r.source_record_sha256=$2 AND t.bot_id=$3 AND t.telegram_id=$4 AND u.telegram_id=$4 FOR SHARE OF r,t,u`, user.Legacy.Key, user.Legacy.RecordSHA256, p.Plan.BotID, id).
			Scan(&owner); err != nil {
			return nil, errors.New("apply_owner_identity_unresolved")
		}
		owners[id] = owner
	}
	for _, marker := range event.Markers {
		var matched bool
		err := tx.QueryRow(ctx, `SELECT source_record_sha256=$3 FROM core.legacy_pass_import_references WHERE source_key=$1 AND bot_id=$2 FOR SHARE`, marker.Legacy.Key, p.Plan.BotID, marker.Legacy.RecordSHA256).
			Scan(&matched)
		if err != nil || !matched {
			return nil, errors.New("food_pass_identity_unresolved")
		}
	}
	return owners, nil
}

func insertFoodEvent(
	ctx context.Context,
	tx pgx.Tx,
	p preparedFood,
	event preparedFoodEvent,
	owners map[int64]string,
) error {
	c := event.Catalog.Configuration
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.food_events(event_id,bot_id,menu,menu_sha256,meal_prices,activity_prices,deadline,cacao_capacity,first_before,last_before,notify_after)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9*interval '1 second',$10*interval '1 second',$11*interval '1 second')`,
		c.Event,
		p.Plan.BotID,
		event.Menu,
		c.MenuSHA256,
		c.MealPrices,
		c.ActivityPrices,
		c.Deadline,
		c.Capacity,
		c.FirstBefore,
		c.LastBefore,
		c.NotifyAfter,
	); err != nil {
		return errors.New("food_target_conflict")
	}
	if err := insertFoodReference(
		ctx,
		tx,
		p.Plan.BotID,
		event.Catalog.Legacy,
		event.Catalog.Record,
		c.Event,
		"configuration",
		c.Event,
	); err != nil {
		return err
	}
	for _, a := range c.Admins {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO core.food_admins(event_id,owner,can_export,can_review,can_assign,instructions) VALUES($1,$2,$3,$4,$5,$6)`,
			c.Event,
			owners[a.Owner],
			a.Export,
			a.Review,
			a.Assign,
			a.Instructions,
		); err != nil {
			return errors.New("food_admin_conflict")
		}
	}
	for _, row := range event.Orders {
		if err := insertFoodOrder(ctx, tx, p, row, c, owners); err != nil {
			return err
		}
	}
	return insertFoodPassMarkers(ctx, tx, p, event, owners)
}

func insertFoodReference(
	ctx context.Context,
	tx pgx.Tx,
	bot int64,
	legacy UserLegacyReference,
	raw json.RawMessage,
	event, kind, target string,
) error {
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.legacy_food_import_references(source_key,bot_id,event_id,source_kind,source_record_sha256,target_id,source_record) VALUES($1,$2,$3,$4,$5,$6,$7)`,
		legacy.Key,
		bot,
		event,
		kind,
		legacy.RecordSHA256,
		target,
		raw,
	)
	if err != nil {
		return errors.New("food_reference_conflict")
	}
	return nil
}

func insertFoodOrder(
	ctx context.Context,
	tx pgx.Tx,
	p preparedFood,
	row FoodPlanRecord,
	c *FoodConfiguration,
	owners map[int64]string,
) error {
	o := row.Food
	if err := insertFoodReference(ctx, tx, p.Plan.BotID, row.Legacy, row.Record, o.Event, "food", o.ID); err != nil {
		return err
	}
	activityTotal, err := foodActivityTotal(c.ActivityPrices, o.Activities)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.food_orders(id,event_id,owner,version,meals,meal_total,complete,activities,activity_total,payment_admin,created_at,last_updated)
VALUES($1,$2,$3,1,$4,$5,$6,$7,$8,NULLIF($9,''),$10,$11)`,
		o.ID,
		o.Event,
		owners[o.Owner],
		o.Meals,
		o.Total,
		o.Complete,
		o.Activities,
		activityTotal,
		owners[o.Admin],
		o.Created,
		o.Updated,
	)
	if err != nil {
		return errors.New("food_order_conflict")
	}
	for _, part := range []struct {
		kind, field string
		payment     FoodPayment
	}{{"meals", registrationProofField, o.MealPayment}, {"activities", "activities_proof_file", o.ActivityPayment}} {
		if err = insertFoodPayment(ctx, tx, p, row, part.kind, part.field, part.payment, owners); err != nil {
			return err
		}
	}
	for phase, sent := range map[string]bool{"first": o.FirstSent, "last": o.LastSent} {
		if sent {
			if err = insertFoodMarker(
				ctx,
				tx,
				o.Event,
				owners[o.Owner],
				"reminder_"+phase,
				o.ID,
				row.Legacy.Key,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func foodActivityTotal(prices map[string]json.RawMessage, activities map[string]bool) (int64, error) {
	classes := 0
	var total int64
	for _, name := range []string{foodYoga, foodCacao, foodSoundHealing} {
		if activities[name] {
			classes++
			price, err := orderMoney(prices[name])
			if err != nil {
				return 0, err
			}
			total += price
		}
	}
	key := ""
	if activities["open"] {
		key = "party"
		if classes > 0 {
			key = "party_and_classes"
		}
	} else if classes == foodClassCount {
		key = "all_classes"
	}
	if key != "" {
		return orderMoney(prices[key])
	}
	if total > maxOrderMoney {
		return 0, errors.New("food_total_invalid")
	}
	return total, nil
}

func insertFoodPayment(
	ctx context.Context,
	tx pgx.Tx,
	p preparedFood,
	row FoodPlanRecord,
	kind, field string,
	payment FoodPayment,
	owners map[int64]string,
) error {
	proofID := ""
	if proof, exists := p.Proofs[row.Legacy.Key+":"+field]; exists {
		key := hashBytes([]byte(row.Legacy.Key + ":" + field))
		if !proof.Reference.Unavailable {
			proofID = "legacy-food-proof:" + key
			if _, err := tx.Exec(
				ctx,
				`INSERT INTO core.order_proofs(id,owner,filename,body) VALUES($1,$2,$3,$4)`,
				proofID,
				owners[row.Food.Owner],
				payment.Proof,
				proof.Body,
			); err != nil {
				return errors.New("food_proof_conflict")
			}
		}
		legacy := UserLegacyReference{Key: key, RecordSHA256: hashBytes(proof.Raw)}
		if err := insertFoodReference(
			ctx,
			tx,
			p.Plan.BotID,
			legacy,
			proof.Raw,
			row.Food.Event,
			"proof",
			row.Food.ID+":"+field,
		); err != nil {
			return err
		}
	}
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.food_payments(order_id,kind,generation,status,proof_id,proof_source,receiver,received_at,confirmed_by,confirmed_at,rejected_by,rejected_at,legacy_source_key)
VALUES($1,$2,0,$3,NULLIF($4,''),$5,NULLIF($6,''),$7,NULLIF($8,''),$9,NULLIF($10,''),$11,$12)`,
		row.Food.ID,
		kind,
		payment.Status,
		proofID,
		payment.Proof,
		"",
		payment.Received,
		owners[payment.ConfirmedBy],
		payment.ConfirmedAt,
		owners[payment.RejectedBy],
		payment.RejectedAt,
		row.Legacy.Key,
	)
	if err != nil {
		return errors.New("food_payment_conflict")
	}
	return nil
}

func insertFoodMarker(ctx context.Context, tx pgx.Tx, event, owner, kind, subject, key string) error {
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.food_notifications(event_id,owner,kind,subject,legacy_source_key,imported_sent) VALUES($1,$2,$3,$4,$5,true)`,
		event,
		owner,
		kind,
		subject,
		key,
	)
	if err != nil {
		return errors.New("food_marker_conflict")
	}
	return nil
}

func completeFoodDeferrals(ctx context.Context, tx pgx.Tx, p preparedFood) error {
	for id, dep := range p.Users {
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT source_record FROM core.legacy_user_deferred_domains WHERE source_key=$1 AND domain='food' FOR UPDATE`, dep.Legacy.Key).
			Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return errors.New("food_deferral_unresolved")
		}
		var events map[string]json.RawMessage
		if json.Unmarshal(raw, &events) != nil {
			return errors.New("food_deferral_unresolved")
		}
		for event, record := range events {
			if !foodDeferralMatches(p.Plan.Markers, id, event, record) {
				return errors.New("food_deferral_marker_missing")
			}
		}
		if _, err = tx.Exec(
			ctx,
			`UPDATE core.legacy_user_deferred_domains SET completed=true WHERE source_key=$1 AND domain='food'`,
			dep.Legacy.Key,
		); err != nil {
			return errors.New("food_deferral_unresolved")
		}
	}
	return completeFoodPassDeferrals(ctx, tx, p)
}

func completeFoodPassDeferrals(ctx context.Context, tx pgx.Tx, p preparedFood) error {
	for _, marker := range p.Plan.Markers {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(marker.Record, &fields)
		if _, dedicated := fields["pass_key"]; !dedicated {
			continue
		}
		projection := map[string]json.RawMessage{}
		for _, key := range []string{foodFirstMarker, foodLastMarker} {
			if raw, exists := fields[key]; exists {
				projection[key] = raw
			}
		}
		var matched bool
		err := tx.QueryRow(ctx, `SELECT d.source_record=$2::jsonb AND r.source_record_sha256=$3 FROM core.legacy_pass_deferred_domains d JOIN core.legacy_pass_import_references r USING(source_key) WHERE d.source_key=$1 AND d.domain='food' FOR UPDATE OF d`, marker.Legacy.Key, projection, marker.Legacy.RecordSHA256).
			Scan(&matched)
		if err != nil || !matched {
			return errors.New("food_pass_deferral_unresolved")
		}
		if _, err = tx.Exec(
			ctx,
			`UPDATE core.legacy_pass_deferred_domains SET completed=true WHERE source_key=$1 AND domain='food'`,
			marker.Legacy.Key,
		); err != nil {
			return errors.New("food_pass_deferral_unresolved")
		}
	}
	return nil
}

func insertFoodPassMarkers(
	ctx context.Context,
	tx pgx.Tx,
	p preparedFood,
	event preparedFoodEvent,
	owners map[int64]string,
) error {
	for _, marker := range event.Markers {
		target := owners[marker.Owner] + ":" + marker.Legacy.Key
		if err := insertFoodReference(
			ctx,
			tx,
			p.Plan.BotID,
			marker.Legacy,
			marker.Record,
			event.Catalog.Configuration.Event,
			"pass_marker",
			target,
		); err != nil {
			return err
		}
		if marker.Shadowed {
			continue
		}
		for phase, sent := range map[string]*bool{"first": marker.First, "last": marker.Last} {
			if sent != nil && *sent {
				if err := insertFoodMarker(
					ctx,
					tx,
					event.Catalog.Configuration.Event,
					owners[marker.Owner],
					"no_order_"+phase,
					"",
					marker.Legacy.Key,
				); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func foodDeferralMatches(markers []FoodPassMarker, id int64, event string, record json.RawMessage) bool {
	found := false
	for _, marker := range markers {
		if marker.Owner != id || marker.Event != event {
			continue
		}
		var parent map[string]json.RawMessage
		_ = json.Unmarshal(marker.Record, &parent)
		var expected, actual any
		_ = json.Unmarshal(record, &expected)
		_ = json.Unmarshal(parent[event], &actual)
		if reflect.DeepEqual(expected, actual) {
			found = true
		}
	}
	return found
}
