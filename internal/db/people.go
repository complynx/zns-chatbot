package db

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrPeopleDenied = errors.New("people search scope denied")
var ErrPeopleInvalid = errors.New("invalid people search")

const (
	peopleScopePublic        = "public"
	peopleScopeAll           = "all"
	peopleScopeRegistrations = "event_registrations"
	peopleScopeMassage       = "massage_clients"
	peopleReadTimeout        = 3 * time.Second
	peopleInfoTimeout        = 4 * time.Second
	peopleMaxPage            = 20
	peopleCandidateTokens    = 12
	peopleMaxCandidates      = 1000
	peopleBigramLength       = 2
	peopleFullNameParts      = 3
)

type PeopleQuery struct {
	Query   string    `json:"query" minLength:"2" maxLength:"128"`
	EventID uuid.UUID `json:"event_id,omitempty"`
	Limit   int       `json:"limit,omitempty" minimum:"1" maximum:"20"`
}

type BasicPerson struct {
	Gender         string    `json:"gender,omitempty"`
	Pronouns       string    `json:"pronouns,omitempty"`
	GenderSource   string    `json:"gender_source,omitempty"`
	PronounsSource string    `json:"pronouns_source,omitempty"`
	UserID         uuid.UUID `json:"user_id"`
	FullName       string    `json:"full_name"`
	Username       string    `json:"username,omitempty"`
	ProfileURL     string    `json:"profile_url"`
}

type PersonMatch struct {
	BasicPerson

	MatchedName string `json:"matched_name"`
	NameSource  string `json:"name_source"`
	Historical  bool   `json:"historical"`
	Score       int    `json:"score"`
}

type PeopleResult struct {
	Scope               string        `json:"scope"`
	Matches             []PersonMatch `json:"matches"`
	HasMore             bool          `json:"has_more"`
	CandidatesTruncated bool          `json:"candidates_truncated"`
}

// SearchPeople rechecks local identity and event authority under the same event
// fence used by role and specialist-binding writes. Candidate names never cross scope.
func (s *Store) SearchPeople(ctx context.Context, scope PaymentScope, a PeopleQuery) (PeopleResult, error) {
	result := PeopleResult{Matches: []PersonMatch{}}

	if utf8.RuneCountInString(strings.TrimSpace(a.Query)) < 2 || utf8.RuneCountInString(a.Query) > 128 ||
		a.Limit < 0 || a.Limit > 20 {
		return result, ErrPeopleInvalid
	}
	if a.Limit == 0 {
		a.Limit = 10
	}
	ctx, cancel := context.WithTimeout(ctx, peopleReadTimeout)
	defer cancel()
	tx, mode, err := s.peopleReadTx(ctx, scope, a.EventID)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result.Scope = mode
	var query []string
	if err = tx.QueryRow(ctx, `SELECT shuttle_name_tokens($1)`, a.Query).Scan(&query); err != nil {
		return result, err
	}
	if len(query) == 0 || len(query) > 6 {
		return result, ErrPeopleInvalid
	}
	filter, arguments := peopleCandidateFilter(scope, a.EventID, mode, query)
	rows, err := tx.Query(ctx, `WITH candidates AS (SELECT u.id,coalesce(nullif(p.full_name,''),
 btrim(concat_ws(' ',u.profile->>'first_name',u.profile->>'last_name'))) AS full_name,
 coalesce(u.profile->>'username','') AS username,u.tg_user_id,
 coalesce(p.gender,'') AS gender,coalesce(p.pronouns,'') AS pronouns,
 coalesce(p.gender_source,'') AS gender_source,coalesce(p.pronouns_source,'') AS pronouns_source,
 n.name AS matched_name,n.source,
 coalesce(CASE n.source WHEN 'profile' THEN n.name=left(btrim(coalesce(p.full_name,'')),512)
 WHEN 'latin' THEN n.name=left(btrim(concat_ws(' ',p.first_name_latin,p.last_name_latin)),512)
 WHEN 'telegram' THEN n.name=left(btrim(concat_ws(' ',u.profile->>'first_name',u.profile->>'last_name')),512)
 ELSE n.name=left(btrim(coalesce(u.profile->>'username','')),512) END,false) AS current_name,u.visible
 FROM shuttle_user_names n JOIN shuttle_users u ON u.id=n.user_id
 LEFT JOIN shuttle_user_profiles p ON p.user_id=u.id
 WHERE `+peopleAccessSQL+filter+`
 ) SELECT id,full_name,username,tg_user_id,gender,pronouns,gender_source,pronouns_source,
 matched_name,source,current_name
  ,shuttle_name_tokens(matched_name) FROM candidates
 WHERE $3<>'public' OR current_name
 ORDER BY current_name DESC,id,source,matched_name
 LIMIT 1001`, arguments...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	return rankPeopleCandidates(ctx, rows, query, a.Limit, result)
}

func rankPeopleCandidates(
	ctx context.Context,
	rows pgx.Rows,
	query []string,
	limit int,
	result PeopleResult,
) (PeopleResult, error) {
	bestByUser := make(map[uuid.UUID]PersonMatch)
	count := 0

	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		count++
		if count > peopleMaxCandidates {
			result.CandidatesTruncated, result.HasMore = true, true
			break
		}
		var candidate []string
		var match PersonMatch
		var current bool
		var telegramID int64
		if err := rows.Scan(
			&match.UserID,
			&match.FullName,
			&match.Username,
			&telegramID,
			&match.Gender, &match.Pronouns, &match.GenderSource, &match.PronounsSource,
			&match.MatchedName,
			&match.NameSource,
			&current, &candidate,
		); err != nil {
			return result, err
		}
		best := bestByUser[match.UserID]
		if len(candidate) > peopleCandidateTokens {
			continue
		}
		match.Score = nameMatch(query, candidate)
		match.ProfileURL = fmt.Sprintf("tg://user?id=%d", telegramID)
		match.Historical = !current
		if result.Scope == peopleScopePublic {
			match.MatchedName, match.NameSource = match.FullName, "current"
			if match.MatchedName == "" {
				match.MatchedName = match.Username
			}
		}
		if match.Score > best.Score || match.Score == best.Score && best.Historical && current {
			bestByUser[match.UserID] = match
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	for _, best := range bestByUser {
		result.addMatch(best, limit)
	}
	return result, ctx.Err()
}

// Each token has an indexable GIN overlap predicate. Final matching still checks
// distinct tokens and ranking; the SQL filter is deliberately a superset.
func peopleCandidateFilter(scope PaymentScope, event uuid.UUID, mode string, tokens []string) (string, []any) {
	arguments := []any{scope.BotID, scope.Organization, mode, optionalUUID(event), scope.UserID, nil}
	filters := make([]string, 0, len(tokens))
	for _, token := range tokens {
		arguments = append(arguments, token)
		parameter := len(arguments)
		filter := fmt.Sprintf("n.search_keys && shuttle_name_keys($%d,'i:')", parameter)
		if utf8.RuneCountInString(token) >= peopleBigramLength {
			filter = fmt.Sprintf("n.search_keys && shuttle_name_keys($%d,'f:')", parameter)
			filter = fmt.Sprintf("(%s OR n.search_keys @> shuttle_name_keys($%d,'g:'))", filter, parameter)
		}
		filters = append(filters, "("+filter+")")
	}
	// Three-part queries may omit one patronymic when the profile has two parts.
	// The ranker applies that restriction; the indexed filter admits any two here.
	if len(filters) == peopleFullNameParts {
		return " AND ((" + filters[0] + " AND " + filters[1] + ") OR (" + filters[0] + " AND " + filters[2] +
			") OR (" + filters[1] + " AND " + filters[2] + "))", arguments
	}
	return " AND " + strings.Join(filters, " AND "), arguments
}

func optionalUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func (result *PeopleResult) addMatch(best PersonMatch, limit int) {
	if best.Score == 0 {
		return
	}
	result.Matches = append(result.Matches, best)
	sort.Slice(result.Matches, func(i, j int) bool {
		x, y := result.Matches[i], result.Matches[j]
		if x.Score != y.Score {
			return x.Score > y.Score
		}
		return x.UserID.String() < y.UserID.String()
	})
	if len(result.Matches) > limit {
		result.HasMore = true
		result.Matches = result.Matches[:limit]
	}
}
