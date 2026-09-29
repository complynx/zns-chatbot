package orders

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// Change records committed state, including the previous extras so a bounded
// history can explain removals without relying on an earlier retained event.
type Change struct {
	Dishes         []DishChange     `json:"dishes,omitempty"`
	CustomerFields []string         `json:"customer_fields,omitempty"`
	OrderID        string           `json:"order_id"`
	Version        int64            `json:"version"`
	Origin         string           `json:"origin"`
	Action         string           `json:"action"`
	State          string           `json:"state"`
	BeforeExtras   map[string]Money `json:"before_extras"`
	Extras         map[string]Money `json:"extras"`
	Total          Money            `json:"total"`
	At             time.Time        `json:"at"`
}

type DishChange struct {
	Day    string `json:"day"`
	Meal   string `json:"meal"`
	Name   string `json:"name"`
	Before int64  `json:"before"`
	After  int64  `json:"after"`
}

type dishKey struct{ day, meal, name string }

func dishCounts(choice Choice) map[dishKey]int64 {
	result := map[dishKey]int64{}
	for day, meals := range choice.Days {
		for meal, value := range meals.Mealtimes {
			for _, item := range value.Dishes {
				result[dishKey{day, meal, item.Name}] += item.Count
			}
		}
	}
	return result
}

func dishChanges(before, after Choice) []DishChange {
	old, current := dishCounts(before), dishCounts(after)
	result := []DishChange{}
	for key, count := range old {
		if count != current[key] {
			result = append(result, DishChange{key.day, key.meal, key.name, count, current[key]})
		}
	}
	for key, count := range current {
		if _, exists := old[key]; !exists {
			result = append(result, DishChange{key.day, key.meal, key.name, 0, count})
		}
	}
	slices.SortFunc(result, func(a, b DishChange) int {
		return cmp.Or(cmp.Compare(a.Day, b.Day), cmp.Compare(a.Meal, b.Meal), cmp.Compare(a.Name, b.Name))
	})
	return result
}

func customerChanges(before, after Choice) []string {
	result := []string{}
	for _, field := range [][3]string{{"customer", before.Customer, after.Customer}, {"first_name", before.FirstName, after.FirstName},
		{"last_name", before.LastName, after.LastName}, {"patronymic", before.Patronymic, after.Patronymic}} {
		if field[1] != field[2] {
			result = append(result, field[0])
		}
	}
	return result
}

func recordChange(
	ctx context.Context,
	tx pgx.Tx,
	actor, origin, action string,
	before Choice,
	order Order,
) error {
	change := Change{OrderID: order.ID, Version: order.Version, Origin: origin, Action: action,
		State: order.State, BeforeExtras: before.Extras, Extras: order.Choice.Extras, Total: order.Choice.Total,
		Dishes: dishChanges(before, order.Choice), CustomerFields: customerChanges(before, order.Choice)}
	_, err := tx.Exec(ctx, `INSERT INTO core.order_audit(order_id,actor,origin,action,version,snapshot)
		VALUES($1,$2,$3,$4,$5,$6)`, order.ID, actor, origin, action, order.Version, change)
	return err
}

// History returns only the authenticated owner's recent committed changes.
// Pre-migration audit rows have no snapshots; their missing details are not inferred.
func (s Service) History(ctx context.Context, owner, event string) ([]Change, error) {
	rows, err := s.DB.Query(ctx, `SELECT snapshot,created_at FROM (
		SELECT a.id,a.snapshot,a.created_at FROM core.order_audit a
		JOIN core.orders o ON o.id=a.order_id
		WHERE o.owner=$1 AND o.event_id=$2 AND a.snapshot IS NOT NULL
		ORDER BY a.id DESC LIMIT 30) recent ORDER BY id`, owner, event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Change{}
	for rows.Next() {
		var change Change
		if err = rows.Scan(&change, &change.At); err != nil {
			return nil, err
		}
		result = append(result, change)
	}
	return result, rows.Err()
}
