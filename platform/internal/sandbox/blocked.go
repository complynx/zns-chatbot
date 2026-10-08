package sandbox

import (
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/api"
)

func (f *Fake) labBlocked(w http.ResponseWriter, r *http.Request) {
	if !labRequest(w, r) {
		return
	}
	var input struct {
		User    int64 `json:"user"`
		Blocked bool  `json:"blocked"`
	}
	if api.Decode(w, r, &input) != nil {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	if _, ok := f.domainOwner(input.User); !ok {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.blocked == nil {
		f.blocked = map[int64]bool{}
	}
	f.blocked[input.User] = input.Blocked
	if err := f.save(r.Context()); err != nil {
		api.JSON(w, http.StatusServiceUnavailable, nil)
		return
	}
	api.JSON(w, http.StatusOK, input)
}
