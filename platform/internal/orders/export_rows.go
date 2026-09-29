package orders

import (
	"errors"
	"maps"
	"math"
	"slices"
	"strconv"
	"time"
)

type exportAggregate struct {
	Count int64
	Sum   int64
}

const bynToRUB = 30

type exportExtra struct{ Key, Name string }

type exportReport struct {
	catalog  Catalog
	dishes   []string
	services []string
	extras   []exportExtra
	food     map[string]exportAggregate
	ware     map[string]exportAggregate
	activity map[string]int64
}

func newExportReport(catalog Catalog) *exportReport {
	return &exportReport{
		catalog:  catalog,
		dishes:   slices.Sorted(maps.Keys(catalog.Dishes)),
		services: slices.Sorted(maps.Keys(catalog.ServiceItems)),
		food:     map[string]exportAggregate{},
		ware:     map[string]exportAggregate{},
		activity: map[string]int64{},
		extras: []exportExtra{
			{"preparty", "Препати"}, {"excursion_minsk", "Экскурсия-квест Минск"}, {"shuttle", "Трансфер Минск–Гродно"},
			{"excursion_grodno_overview", "Гродно: обзорная экскурсия"},
			{"excursion_grodno_gorodnitsa", "Гродно: экскурсия «Городница»"},
			{"excursion_grodno", "Экскурсия по Гродно (вариант не указан)"},
		},
	}
}

func exportName(definitions map[string]Definition, key string) string {
	if name := definitions[key].NameRU; name != "" {
		return name
	}
	return key
}

func (r *exportReport) headers(w *exportWorkbook) error {
	headers := []any{"ID заказа", "Пользователь", "Клиент", "Создан", "Обновлён", "Страна оплаты",
		"Администратор оплаты", "Оплачено", "Подтверждено администратором", "Сумма BYN", "Сумма RUB"}
	for _, extra := range r.extras {
		headers = append(headers, extra.Name)
	}
	for _, key := range r.dishes {
		headers = append(headers, exportName(r.catalog.Dishes, key))
	}
	for _, key := range r.services {
		headers = append(headers, exportName(r.catalog.ServiceItems, key))
	}
	if err := w.sheet(exportOrdersSheet, headers); err != nil {
		return err
	}
	return w.sheet(
		exportDetailsSheet,
		[]any{
			"Пользователь",
			"ID заказа",
			"Клиент",
			"День",
			"Приём пищи",
			"Блюдо / Активность",
			"Оплачено",
			"Количество",
		},
	)
}

func (r *exportReport) order(w *exportWorkbook, entry exportOrder) error {
	order := entry.Order
	food, ware := map[string]int64{}, map[string]int64{}
	for _, day := range slices.Sorted(maps.Keys(order.Choice.Days)) {
		meals := order.Choice.Days[day].Mealtimes
		for _, meal := range slices.Sorted(maps.Keys(meals)) {
			if err := r.lines(w, entry, day, meal, meals[meal].Dishes, food, false); err != nil {
				return err
			}
			if err := r.lines(w, entry, day, meal, meals[meal].Service.Items, ware, true); err != nil {
				return err
			}
		}
	}
	row := []any{
		order.ID,
		strconv.FormatInt(entry.TelegramID, 10),
		order.Choice.Customer,
		order.CreatedAt.UTC().Format(time.RFC3339),
		entry.UpdatedAt.UTC().Format(time.RFC3339),
		order.Country,
		entry.PaymentAdmin,
		order.reserves(),
		order.State == statePaid,
		exportMoney(int64(order.Choice.Total)),
		exportMoney(int64(order.Choice.Total) * bynToRUB),
	}
	for _, extra := range r.extras {
		count := 0
		if _, selected := order.Choice.Extras[extra.Key]; selected {
			count = 1
			if err := w.append(
				exportDetailsSheet,
				detailRow(entry, "friday", "активности", extra.Name, 1),
			); err != nil {
				return err
			}
			if order.reserves() {
				r.activity[extra.Key]++
			}
		}
		row = append(row, count)
	}
	for _, key := range r.dishes {
		row = append(row, food[key])
	}
	for _, key := range r.services {
		row = append(row, ware[key])
	}
	return w.append(exportOrdersSheet, row)
}

func (r *exportReport) lines(
	w *exportWorkbook,
	entry exportOrder,
	day, meal string,
	lines []Line,
	counts map[string]int64,
	service bool,
) error {
	definitions, totals := r.catalog.Dishes, r.food
	if service {
		definitions, totals = r.catalog.ServiceItems, r.ware
	}
	for _, line := range lines {
		if err := w.append(
			exportDetailsSheet,
			detailRow(entry, day, meal, exportName(definitions, line.Name), line.Count),
		); err != nil {
			return err
		}
		if _, known := definitions[line.Name]; !known {
			continue
		}
		if line.Count < 0 || counts[line.Name] > math.MaxInt64-line.Count {
			return errors.New("invalid historical item count")
		}
		counts[line.Name] += line.Count
		if entry.Order.reserves() {
			aggregate, err := addExportAggregate(totals[line.Name], line)
			if err != nil {
				return err
			}
			totals[line.Name] = aggregate
		}
	}
	return nil
}

func addExportAggregate(value exportAggregate, line Line) (exportAggregate, error) {
	if line.Price < 0 || line.Count < 0 || value.Count > math.MaxInt64-line.Count ||
		(line.Count > 0 && int64(line.Price) > (math.MaxInt64-value.Sum)/line.Count) {
		return value, errors.New("historical aggregate out of range")
	}
	return exportAggregate{Count: value.Count + line.Count, Sum: value.Sum + line.Count*int64(line.Price)}, nil
}

func detailRow(entry exportOrder, day, meal, name string, count int64) []any {
	days := map[string]string{"friday": "Пятница", "saturday": "Суббота", "sunday": "Воскресенье"}
	meals := map[string]string{"lunch": "Обед", "dinner": "Ужин"}
	if label := days[day]; label != "" {
		day = label
	}
	if label := meals[meal]; label != "" {
		meal = label
	}
	return []any{
		strconv.FormatInt(entry.TelegramID, 10),
		entry.Order.ID,
		entry.Order.Choice.Customer,
		day,
		meal,
		name,
		entry.Order.reserves(),
		count,
	}
}

func (r *exportReport) totals(w *exportWorkbook) error {
	if err := r.aggregateSheet(w, "Итоги", "Блюдо", r.dishes, r.catalog.Dishes, r.food); err != nil {
		return err
	}
	if err := r.aggregateSheet(w, "Посуда", "Позиция", r.services, r.catalog.ServiceItems, r.ware); err != nil {
		return err
	}
	const sheet = "Активности"
	if err := w.sheet(sheet, []any{"Активность", "Кол-во оплаченных заказов"}); err != nil {
		return err
	}
	for _, extra := range r.extras {
		if err := w.append(sheet, []any{extra.Name, r.activity[extra.Key]}); err != nil {
			return err
		}
	}
	return nil
}

func (r *exportReport) aggregateSheet(
	w *exportWorkbook,
	name, header string,
	keys []string,
	definitions map[string]Definition,
	totals map[string]exportAggregate,
) error {
	if err := w.sheet(name, []any{header, "Количество оплачено", "Сумма BYN оплачено"}); err != nil {
		return err
	}
	for _, key := range keys {
		value := totals[key]
		if err := w.append(name, []any{exportName(definitions, key), value.Count, exportMoney(value.Sum)}); err != nil {
			return err
		}
	}
	return nil
}

func exportMoney(cents int64) float64 { return float64(cents) / centsPerUnit }
