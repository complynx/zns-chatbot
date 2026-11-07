package db

// OrderKind is business identity. OrderSource separately identifies transport.
type OrderKind string

const (
	OrderKindService OrderKind = "service"
	OrderKindMeal    OrderKind = "meal"
)
