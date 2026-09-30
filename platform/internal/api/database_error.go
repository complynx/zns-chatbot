package api

import (
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func markDatabaseFailure(w http.ResponseWriter, err error) bool {
	if !core.IsDatabaseFailure(err) {
		return false
	}
	w.Header().Set(core.DatabaseFailureHeader, "1")
	return true
}
