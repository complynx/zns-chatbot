package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// CreateDueMassageSpecialistNotifications captures one bounded page of upcoming
// sessions. Disabled preferences consume that session's notice, as in the legacy
// notifier. Late sessions catch up only while their event has not ended.
// Each insert rechecks eligibility; the booking/kind key survives partial batches.
func (s *Store) CreateDueMassageSpecialistNotifications(ctx context.Context, scope EventScope) error {
	bookings, err := s.q.DueMassageSpecialistSoonBookings(ctx, sqlc.DueMassageSpecialistSoonBookingsParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, PageLimit: NotificationRepairPageSize,
	})
	if err != nil {
		return err
	}
	for _, booking := range bookings {
		err = s.q.InsertMassageSpecialistSoonNotification(ctx, sqlc.InsertMassageSpecialistSoonNotificationParams{
			ID: uuid.New(), BookingID: booking, BotID: scope.BotID, OrganizationID: scope.Organization,
		})
		if err != nil {
			return err
		}
	}
	return nil
}
