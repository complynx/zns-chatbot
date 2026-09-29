// Package core provides shared errors, read contracts and capability evidence.
package core

import "github.com/jackc/pgx/v5/pgxpool"

type ProblemError struct {
	Status int    `json:"-"`
	Code   string `json:"code"`
}

func (p *ProblemError) Error() string { return p.Code }

type Service struct {
	DB               *pgxpool.Pool
	LegacyOrderBotID int64
}
