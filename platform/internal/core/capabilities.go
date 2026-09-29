package core

import "context"

// BusinessCapabilities is current permission evidence, not a grant to execute.
type BusinessCapabilities struct {
	CanBook         bool `json:"can_book"`
	CanExportOrders bool `json:"can_export_orders"`
}

func (s Service) Capabilities(ctx context.Context, owner, event string) (BusinessCapabilities, error) {
	var value BusinessCapabilities
	err := s.DB.QueryRow(ctx, `SELECT u.can_book, u.can_book AND EXISTS(
	 SELECT 1 FROM core.order_admins a WHERE a.owner=u.id AND a.event_id=$2)
	 FROM core.users u WHERE u.id=$1`, owner, event).Scan(&value.CanBook, &value.CanExportOrders)
	return value, err
}
