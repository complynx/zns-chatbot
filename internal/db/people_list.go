package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type PeopleCursor struct {
	EventID uuid.UUID `json:"event_id,omitempty"`
	Sort    string    `json:"sort"`
	Key     string    `json:"key" maxLength:"256"`
	UserID  uuid.UUID `json:"user_id"`
}
type PeopleListQuery struct {
	EventID uuid.UUID     `json:"event_id,omitempty"`
	Sort    string        `json:"sort,omitempty" enum:"name,name_desc,recent_activity"`
	After   *PeopleCursor `json:"after,omitempty"`
	Limit   int           `json:"limit,omitempty" minimum:"1" maximum:"20"`
}
type PeoplePage struct {
	Scope string        `json:"scope"`
	Users []BasicPerson `json:"users"`
	Next  *PeopleCursor `json:"next,omitempty"`
}

// ListPeople is a staff capability. Public discovery deliberately requires a search query.
func (s *Store) ListPeople(ctx context.Context, scope PaymentScope, a PeopleListQuery) (PeoplePage, error) {
	result := PeoplePage{Users: []BasicPerson{}}
	if a.Sort == "" {
		a.Sort = "name"
	}
	if a.Limit == 0 {
		a.Limit = 20
	}
	if a.Limit < 1 || a.Limit > 20 {
		return result, ErrPeopleInvalid
	}
	key, direction, compare, err := peopleListSort(a.Sort)
	if err != nil {
		return result, err
	}

	var afterKey any
	afterID := uuid.Nil
	if c := a.After; c != nil {
		if c.EventID != a.EventID || c.Sort != a.Sort || c.UserID == uuid.Nil || len(c.Key) > 1024 {
			return result, ErrPeopleInvalid
		}
		afterKey, afterID = c.Key, c.UserID
	}
	ctx, cancel := context.WithTimeout(ctx, peopleReadTimeout)
	defer cancel()
	tx, mode, err := s.peopleReadTx(ctx, scope, a.EventID)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if mode == peopleScopePublic {
		return result, ErrPeopleDenied
	}
	result.Scope = mode
	// Both SQL fragments come only from the enum above. Cursor values remain parameters.
	statement := `WITH people AS (SELECT ` + basicPersonSQL + `
 FROM shuttle_users u LEFT JOIN shuttle_user_profiles p ON p.user_id=u.id WHERE ` + peopleAccessSQL + `),
 ordered AS (SELECT people.id,people.full_name,people.username,people.tg_user_id,
 people.gender,people.pronouns,people.gender_source,people.pronouns_source,` + key + ` AS sort_key FROM people)
 SELECT id,full_name,username,tg_user_id,gender,pronouns,gender_source,pronouns_source,sort_key FROM ordered
 WHERE ($7::text IS NULL OR (sort_key,id) ` + compare + ` ($7,$8::uuid))
 ORDER BY sort_key ` + direction + `,id ` + direction + ` LIMIT $9`
	rows, err := tx.Query(
		ctx,
		statement,
		scope.BotID,
		scope.Organization,
		mode,
		optionalUUID(a.EventID),
		scope.UserID,
		nil,
		afterKey,
		afterID,
		a.Limit+1,
	)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	var lastKey string
	for rows.Next() {
		var person BasicPerson
		var telegramID int64
		var cursorKey string
		if err = rows.Scan(
			&person.UserID,
			&person.FullName,
			&person.Username,
			&telegramID,
			&person.Gender,
			&person.Pronouns,
			&person.GenderSource,
			&person.PronounsSource,
			&cursorKey,
		); err != nil {
			return result, err
		}
		if len(result.Users) == a.Limit {
			result.Next = &PeopleCursor{
				EventID: a.EventID,
				Sort:    a.Sort,
				Key:     lastKey,
				UserID:  result.Users[len(result.Users)-1].UserID,
			}
			break
		}
		person.ProfileURL = fmt.Sprintf("tg://user?id=%d", telegramID)
		result.Users = append(result.Users, person)
		lastKey = cursorKey
	}
	return result, rows.Err()
}

func peopleListSort(sort string) (string, string, string, error) {
	direction, compare := "ASC", ">"
	key := `left(lower(full_name),256)`
	switch sort {
	case "name":
	case "name_desc":
		direction, compare = "DESC", "<"
	case "recent_activity":
		key = `to_char(coalesce((SELECT max(t.created_at) FROM shuttle_tasks t
 WHERE t.user_id=people.id),'epoch'::timestamptz) AT TIME ZONE 'UTC','YYYY-MM-DD HH24:MI:SS.US')`
		direction, compare = "DESC", "<"
	default:
		return "", "", "", ErrPeopleInvalid
	}
	return key, direction, compare, nil
}
