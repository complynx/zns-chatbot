package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

// Frozen terms are quoted data. Fence growth prevents event labels from escaping.
func massageBookingDescription(snapshot db.MassageBookingSnapshot, language string, cancel bool) (string, error) {
	terms := snapshot.Terms
	zone, err := time.LoadLocation(terms.Timezone)
	if err != nil {
		return "", err
	}
	label := func(value massage.ScheduleLabel) string {
		if language == "ru" {
			return value.RU
		}
		return value.EN
	}
	title, event, party, specialist, service := "Book massage?", "Event", "Party", "Specialist", "Service"
	price, start, end, occupied := "Price", "Start", "Treatment ends", "Occupied until"
	effect := "Reserves the selected time. This does not confirm payment."
	if language == "ru" {
		title, event, party, specialist, service = "Записаться на массаж?", "Эвент", "Вечеринка", "Специалист", "Услуга"
		price, start, end, occupied = "Цена", "Начало", "Конец процедуры", "Место занято до"
		effect = "Резервирует выбранное время. Оплату не подтверждает."
	}
	if snapshot.Purpose == "staff_self" {
		title, effect = "Reserve your own specialist time?", "Reserves your own time with yourself as client and specialist. This does"+
			" not confirm payment."
		if language == "ru" {
			title, effect = "Зарезервировать своё время специалиста?", "Резервирует своё время: вы участник и специалист этой записи. Оплату не "+
				"подтверждает."
		}
	}
	if cancel {
		title, effect = "Cancel this massage booking?", "Releases this reservation and retains history. No refund or payment chan"+
			"ge is created."
		if language == "ru" {
			title, effect = "Отменить эту запись на массаж?", "Освобождает эту запись и сохраняет историю. Возврат или изменение оплаты"+
				" не создаётся."
		}
	}
	body := fmt.Sprintf("%s: %s\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n%s", event, snapshot.EventName,
		party, label(terms.WindowName), specialist, label(terms.SpecialistName), service, label(terms.ServiceName),
		price, orderMoney(terms.PriceMinor, terms.CurrencyScale, terms.Currency),
		start, terms.StartsAt.In(zone).Format(time.RFC3339), end, terms.TreatmentEndsAt.In(zone).Format(time.RFC3339),
		occupied, terms.OccupancyEndsAt.In(zone).Format(time.RFC3339)+" ("+terms.Timezone+")", effect)
	fence := preparationCodeFence
	for strings.Contains(body, fence) {
		fence += "`"
	}
	return title + "\n\n" + fence + "\n" + body + "\n" + fence, nil
}
