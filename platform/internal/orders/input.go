package orders

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// JSON null must not silently become an empty order component. Omitted optional
// components remain valid, as in the existing web ordering contract.
func decodeObject(data []byte, target any, nonNull ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return fmt.Errorf("expected object")
	}
	for _, key := range nonNull {
		if bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return fmt.Errorf("%s must not be null", key)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func (in *ChoiceInput) UnmarshalJSON(data []byte) error {
	type input ChoiceInput
	return decodeObject(
		data,
		(*input)(in),
		"days",
		"extras",
		"customer",
		"customer_first_name",
		"customer_last_name",
		"customer_patronymus",
	)
}

func (in *DayInput) UnmarshalJSON(data []byte) error {
	type input DayInput
	return decodeObject(data, (*input)(in), "mealtimes")
}

func (in *MealInput) UnmarshalJSON(data []byte) error {
	type input MealInput
	return decodeObject(data, (*input)(in), "dishes")
}
