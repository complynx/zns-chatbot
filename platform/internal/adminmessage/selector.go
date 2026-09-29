package adminmessage

import (
	"context"
	"encoding/json"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type selector map[string]any

func parseSelector(raw string) (selector, error) {
	if len(raw) > selectorInputBytes {
		return nil, rejected("admin_message_selector_budget")
	}
	var value selector
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, invalid()
	}
	delete(value, "bot_id") // The deployment scope overrides the source command's top-level bot_id.
	nodes := 128
	if err := validateSelector(value, 0, &nodes); err != nil {
		return nil, err
	}
	return value, nil
}

func selectorField(field string) bool {
	switch field {
	case "user_id",
		"bot_id",
		"username",
		"first_name",
		"last_name",
		"print_name",
		"language_code",
		"known_names",
		"informal_name",
		"legal_name",
		"role",
		"passport_number",
		"legal_name_frozen",
		"massage_specialist":
		return true
	}
	if strings.HasPrefix(field, "inner_name_") && len(field) <= 100 && !strings.ContainsAny(field, ".$\x00") {
		return true
	}
	return false
}

func validateSelector(value map[string]any, depth int, nodes *int) error {
	*nodes -= len(value)
	if depth > 8 || *nodes < 0 {
		return rejected("admin_message_selector_budget")
	}
	for field, predicate := range value {
		if isSelectorGroup(field) {
			if err := validateSelectorGroup(predicate, depth, nodes); err != nil {
				return err
			}
			continue
		}
		if !selectorField(field) {
			return rejected("admin_message_selector_field_unsupported")
		}
		if operators, ok := predicate.(map[string]any); ok {
			if err := validateSelectorOperators(operators, nodes); err != nil {
				return err
			}
		}
	}
	return nil
}

func isSelectorGroup(field string) bool {
	return field == selectorAnd || field == "$or" || field == "$nor"
}

func validateSelectorGroup(predicate any, depth int, nodes *int) error {
	children, ok := predicate.([]any)
	if !ok || len(children) == 0 {
		return invalid()
	}
	for _, child := range children {
		object, valid := child.(map[string]any)
		if !valid {
			return invalid()
		}
		if err := validateSelector(object, depth+1, nodes); err != nil {
			return err
		}
	}
	return nil
}

func validateSelectorOperators(operators map[string]any, nodes *int) error {
	for operator, argument := range operators {
		*nodes--
		if *nodes < 0 {
			return rejected("admin_message_selector_budget")
		}
		if err := validateSelectorOperator(operator, argument); err != nil {
			return err
		}
	}
	return nil
}

func validateSelectorOperator(operator string, argument any) error {
	switch operator {
	case "$eq", "$ne", "$gt", "$gte", "$lt", "$lte":
		if _, nested := argument.(map[string]any); nested {
			return invalid()
		}
	case "$in", selectorNin:
		if _, valid := argument.([]any); !valid {
			return invalid()
		}
	case "$exists":
		if _, valid := argument.(bool); !valid {
			return invalid()
		}
	default:
		return rejected("admin_message_selector_operator_unsupported")
	}
	return nil
}

func matchesSelector(fields map[string]any, value selector) bool {
	for field, predicate := range value {
		if isSelectorGroup(field) {
			if !matchesSelectorGroup(fields, field, predicate) {
				return false
			}
			continue
		}
		actual, present := fields[field]
		if !matchesFieldPredicate(actual, present, predicate) {
			return false
		}
	}
	return true
}

func matchesSelectorGroup(fields map[string]any, group string, predicate any) bool {
	children, _ := predicate.([]any)
	matched := 0
	for _, child := range children {
		object, _ := child.(map[string]any)
		if matchesSelector(fields, object) {
			matched++
		}
	}
	switch group {
	case selectorAnd:
		return matched == len(children)
	case "$or":
		return matched > 0
	case "$nor":
		return matched == 0
	}
	return false
}

func matchesFieldPredicate(actual any, present bool, predicate any) bool {
	operators, ok := predicate.(map[string]any)
	if !ok {
		return selectorEqual(actual, present, predicate)
	}
	for operator, expected := range operators {
		if !matchesPredicate(actual, present, operator, expected) {
			return false
		}
	}
	return true
}
func selectorEqual(actual any, present bool, expected any) bool {
	if expected == nil {
		return !present || actual == nil
	}
	if !present {
		return false
	}
	if a, ok := actual.(json.Number); ok {
		if b, valid := expected.(json.Number); valid {
			left, lok := new(big.Rat).SetString(string(a))
			right, rok := new(big.Rat).SetString(string(b))
			return lok && rok && left.Cmp(right) == 0
		}
	}
	if reflect.DeepEqual(actual, expected) {
		return true
	}
	if array, ok := actual.([]any); ok {
		for _, item := range array {
			if selectorEqual(item, true, expected) {
				return true
			}
		}
	}
	return false
}

func matchesPredicate(actual any, present bool, operator string, expected any) bool {
	switch operator {
	case "$exists":
		return present == expected
	case "$eq":
		return selectorEqual(actual, present, expected)
	case "$ne":
		return !selectorEqual(actual, present, expected)
	case "$in", selectorNin:
		values, _ := expected.([]any)
		matched := false
		for _, value := range values {
			matched = matched || selectorEqual(actual, present, value)
		}
		if operator == selectorNin {
			return !matched
		}
		return matched
	default:
		comparison, ok := selectorCompare(actual, expected)
		if !present || !ok {
			return false
		}
		switch operator {
		case "$gt":
			return comparison > 0
		case "$gte":
			return comparison >= 0
		case "$lt":
			return comparison < 0
		case "$lte":
			return comparison <= 0
		}
	}
	return false
}

func selectorCompare(left, right any) (int, bool) {
	if a, ok := left.(string); ok {
		b, valid := right.(string)
		return strings.Compare(a, b), valid
	}
	if a, ok := left.(json.Number); ok {
		if b, valid := right.(json.Number); valid {
			l, lok := new(big.Rat).SetString(string(a))
			r, rok := new(big.Rat).SetString(string(b))
			if lok && rok {
				return l.Cmp(r), true
			}
		}
	}
	return 0, false
}

func (s Service) resolveSelector(ctx context.Context, tx pgx.Tx, raw string) ([]Destination, error) {
	predicate, err := parseSelector(raw)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(
		ctx,
		`SELECT u.telegram_id,p.fields||p.overrides FROM core.users u LEFT JOIN core.admin_broadcast_profiles p ON p.owner=u.id ORDER BY u.telegram_id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Destination
	for rows.Next() {
		var id int64
		var data []byte
		if err = rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		if data == nil {
			return nil, rejected("admin_message_selector_profile_unavailable")
		}
		var fields map[string]any
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.UseNumber()
		if err = decoder.Decode(&fields); err != nil {
			return nil, err
		}
		fields["user_id"] = json.Number(strconv.FormatInt(id, 10))
		s.scopeProfile(fields)
		if s.BotID > 0 {
			fields["bot_id"] = json.Number(strconv.FormatInt(s.BotID, 10))
		}
		if matchesSelector(fields, predicate) {
			result = append(result, Destination{Chat: strconv.FormatInt(id, 10)})
		}
	}
	return result, rows.Err()
}
