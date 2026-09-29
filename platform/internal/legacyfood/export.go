package legacyfood

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type Export struct {
	Orders  []byte `json:"orders"`
	Summary []byte `json:"summary"`
}

type exportIdentity struct {
	ID                             string
	Telegram                       int64
	Username, PrintName, LegalName string
	Aggregate                      bool
}
type exportItem struct {
	Day, Meal, Key, Title string
	Price                 Amount
	Component             bool
}

func (s Service) Export(ctx context.Context, actor, event string) (Export, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Export{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err = allowed(ctx, tx, actor); err != nil {
		return Export{}, err
	}
	if err = adminAllowed(ctx, tx, actor, event, "export"); err != nil {
		return Export{}, err
	}
	config, err := s.event(ctx, tx, event, false)
	if err != nil {
		return Export{}, err
	}
	var menu Menu
	if err = json.Unmarshal(config.Menu, &menu); err != nil {
		return Export{}, err
	}
	rows, err := tx.Query(ctx, `SELECT o.id,u.telegram_id,u.username,u.print_name,COALESCE(p.legal_name,'')
 FROM core.food_orders o JOIN core.users u ON u.id=o.owner LEFT JOIN core.pass_profiles p ON p.owner=o.owner
 WHERE o.event_id=$1 ORDER BY o.created_at,o.id LIMIT 10001`, event)
	if err != nil {
		return Export{}, err
	}
	identities, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (exportIdentity, error) {
		var id exportIdentity
		scanErr := row.Scan(&id.ID, &id.Telegram, &id.Username, &id.PrintName, &id.LegalName)
		return id, scanErr
	})
	if err != nil {
		return Export{}, err
	}
	if len(identities) > exportLimit {
		return Export{}, problem("food_export_limit")
	}
	return exportFoodRows(ctx, tx, event, menu, config.MealPrices, identities)
}

func exportFoodRows(
	ctx context.Context,
	tx pgx.Tx,
	event string,
	menu Menu,
	prices MealPrices,
	identities []exportIdentity,
) (Export, error) {
	var err error
	days := menuDays(menu)
	details := [][]string{
		{"ID Пользователя", "Username", "Print Name", "Legal Name", "Оплачено", "Платеж Подтвержден", "Итого Сумма"},
	}
	for _, day := range days {
		if len(menu[day].Lunch) > 0 {
			details[0] = append(details[0], dayName(day)+" Обед")
		}
		if len(menu[day].Dinner) > 0 {
			details[0] = append(details[0], dayName(day)+" Ужин")
		}
	}
	counts := map[exportItem]int{}
	for _, identity := range identities {
		order, loadErr := loadOrder(ctx, tx, event, identity.ID, "")
		if loadErr != nil {
			return Export{}, loadErr
		}
		identity.Aggregate, loadErr = aggregatePayment(ctx, tx, order.MealPayment)
		if loadErr != nil {
			return Export{}, loadErr
		}
		line := foodExportLine(identity, order, menu, prices, days, counts)
		details = append(details, line)
	}
	result := Export{}
	result.Orders, err = csvBytes(details)
	if err != nil {
		return result, err
	}
	result.Summary, err = csvBytes(summaryRows(counts))
	return result, err
}

func menuDays(menu Menu) []string {
	days := make([]string, 0, len(menu))
	for day := range menu {
		days = append(days, day)
	}
	sort.Strings(days)
	return days
}
func dayName(day string) string {
	if name := map[string]string{"friday": "Пятница", "saturday": "Суббота", "sunday": "Воскресенье"}[day]; name != "" {
		return name
	}
	return day
}
func yesNo(value bool) string {
	if value {
		return "Да"
	}
	return "Нет"
}
func amountText(value Amount) string {
	return fmt.Sprintf("%d.%02d", value/centsPerRuble, value%centsPerRuble)
}

func csvBytes(rows [][]string) ([]byte, error) {
	var out bytes.Buffer
	writer := csv.NewWriter(&out)
	for _, row := range rows {
		for index, value := range row {
			trimmed := strings.TrimLeft(value, " \t\r\n")
			if strings.ContainsAny(trimmed[:min(1, len(trimmed))], "=+-@") {
				row[index] = "'" + value
			}
		}
		if err := writer.Write(row); err != nil {
			return nil, err
		}
		if out.Len() > 20<<20 {
			return nil, problem("food_export_limit")
		}
	}
	writer.Flush()
	return out.Bytes(), writer.Error()
}

func itemNames(menu []Dish, indices []Index) string {
	names := []string{}
	for _, index := range indices {
		if index >= 0 && int(index) < len(menu) {
			names = append(names, menu[index].TitleRU)
		}
	}
	return strings.Join(names, ", ")
}

func comboIndices(lunch *LunchSelection) []Index {
	var fields map[string]*Index
	_ = json.Unmarshal(lunch.Items, &fields)
	keys := []string{comboMain, comboSide, comboSalad}
	if lunch.Type == lunchSoup {
		keys = append([]string{comboSoup}, keys...)
	}
	indices := []Index{}
	for _, key := range keys {
		if value := fields[key]; value != nil {
			indices = append(indices, *value)
		}
	}
	return indices
}

func lunchText(menu []Dish, lunch *LunchSelection) string {
	if lunch == nil {
		return ""
	}
	switch lunch.Type {
	case lunchNone:
		return "Без обеда"
	case lunchIndividual:
		var indices []Index
		_ = json.Unmarshal(lunch.Items, &indices)
		return itemNames(menu, indices)
	case lunchSoup, lunchWithoutSoup:
		name := comboName(lunch.Type)
		items := itemNames(menu, comboIndices(lunch))
		if items != "" {
			name += ": " + items
		}
		return name
	default:
		return ""
	}
}

func comboName(kind string) string {
	if kind == lunchSoup {
		return "Комбо с супом"
	}
	return "Комбо без супа"
}

func foodExportLine(
	identity exportIdentity,
	order Order,
	menu Menu,
	prices MealPrices,
	days []string,
	counts map[exportItem]int,
) []string {
	line := []string{
		strconv.FormatInt(identity.Telegram, 10),
		identity.Username,
		identity.PrintName,
		identity.LegalName,
		yesNo(order.MealPayment.Status == Paid),
		yesNo(order.MealPayment.ConfirmedAt != nil),
		amountText(order.MealTotal),
	}
	for _, day := range days {
		chosen, catalog := order.Meals[day], menu[day]
		if len(catalog.Lunch) > 0 {
			line = append(line, lunchText(catalog.Lunch, chosen.Lunch))
		}
		if len(catalog.Dinner) > 0 {
			line = append(line, itemNames(catalog.Dinner, chosen.Dinner))
		}
		if identity.Aggregate {
			for _, item := range exportItems(day, catalog, chosen, prices) {
				counts[item]++
			}
		}
	}

	return line
}

// Python's aggregate checks field existence; its detail CSV checks a non-null date.
func aggregatePayment(ctx context.Context, tx pgx.Tx, payment Payment) (bool, error) {
	if payment.Status != Paid {
		return false, nil
	}
	if payment.ConfirmedAt != nil {
		return true, nil
	}
	if payment.Generation != 0 || payment.LegacySourceKey == "" {
		return false, nil
	}
	var present bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.legacy_food_import_references
 WHERE source_key=$1 AND source_kind='food' AND source_record ? 'payment_confirmed_date')`,
		payment.LegacySourceKey).Scan(&present)
	return present, err
}
