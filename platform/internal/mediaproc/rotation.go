package mediaproc

import (
	"encoding/json"
	"strconv"
	"strings"
)

// supportedDisplayRotation accepts only unscaled quarter-turn display matrices.
// Translation, reflection, perspective and other side data remain unsupported.
func supportedDisplayRotation(entries []json.RawMessage) bool {
	if len(entries) == 0 {
		return true
	}
	if len(entries) != 1 {
		return false
	}
	var entry struct {
		Type     string `json:"side_data_type"`
		Matrix   string `json:"displaymatrix"`
		Rotation *int   `json:"rotation"`
	}
	if json.Unmarshal(entries[0], &entry) != nil || entry.Type != "Display Matrix" || entry.Rotation == nil {
		return false
	}
	matrix, ok := displayMatrix(entry.Matrix)
	if !ok {
		return false
	}
	const unit = 1 << 16
	const homogeneous = 1 << 30
	const quarterTurn = 90
	const halfTurn = 180
	switch matrix {
	case [9]int64{unit, 0, 0, 0, unit, 0, 0, 0, homogeneous}:
		return *entry.Rotation == 0
	case [9]int64{0, -unit, 0, unit, 0, 0, 0, 0, homogeneous}:
		return *entry.Rotation == quarterTurn
	case [9]int64{0, unit, 0, -unit, 0, 0, 0, 0, homogeneous}:
		return *entry.Rotation == -quarterTurn
	case [9]int64{-unit, 0, 0, 0, -unit, 0, 0, 0, homogeneous}:
		return *entry.Rotation == halfTurn || *entry.Rotation == -halfTurn
	default:
		return false
	}
}
func displayMatrix(text string) ([9]int64, bool) {
	var matrix [9]int64
	rows := strings.Split(strings.TrimSpace(text), "\n")
	const dimension = 3
	if len(rows) != dimension {
		return matrix, false
	}
	for row, line := range rows {
		_, values, ok := strings.Cut(line, ":")
		fields := strings.Fields(values)
		if !ok || len(fields) != dimension {
			return matrix, false
		}
		for column, field := range fields {
			value, err := strconv.ParseInt(field, 10, 64)
			if err != nil {
				return matrix, false
			}
			matrix[row*dimension+column] = value
		}
	}
	return matrix, true
}
