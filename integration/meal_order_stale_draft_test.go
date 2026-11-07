package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMealOrderHTTPStaleDraftCanBeSavedAgainAfterConfigurationChange(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"event", "catalog"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f, _, session, args := mealOrderAppFixture(t)
			scope := ownerProfileScope(t, f.H, f.OwnerID)
			draft, err := f.H.store.OwnMealDraft(t.Context(), scope, args.EventID)
			require.NoError(t, err)
			catalog, err := f.H.store.OwnMealCatalog(t.Context(), scope, args.EventID, 0)
			require.NoError(t, err)
			if change == "event" {
				doc := f.Event.Document
				doc.Name = "Updated event name"
				code, raw := adminRequest(t, f.H, f.Admin, http.MethodPut,
					"/internal/admin/events/"+args.EventID.String(), map[string]any{
						"operation_id": uuid.NewString(), "expected_version": f.Event.Version, "document": doc,
					})
				require.Equal(t, http.StatusOK, code, string(raw))
				require.NoError(t, json.Unmarshal(raw, &f.Event))
				args.ExpectedEventVersion = f.Event.Version
			} else {
				published := publishMealCatalog(t, f, catalog.Version, catalog.Document)
				args.ExpectedCatalogVersion = published.Version
			}
			path := mealsAppPath(args.EventID)
			code, raw, _ := appCall(t, f.H, http.MethodPost, path+"/submit/prepare",
				paymentAppJSON(t, orderAppPrepareBody(uuid.New(), args)), session.Token, appTestOrigin)
			require.Equal(t, http.StatusConflict, code, string(raw))
			code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, mealOrderAgentPath+"submit/prepare",
				map[string]any{"protocol_version": 1, "operation_id": uuid.New(), "arguments": args})
			require.Equal(t, http.StatusConflict, code, string(raw))
			mealOrderCounts(t, f, 0, 0)
			saved, err := f.H.store.SaveOwnMealDraft(t.Context(), scope, db.MealDraftChange{
				OperationID: uuid.New(), EventID: args.EventID, ExpectedEventVersion: args.ExpectedEventVersion,
				ExpectedCatalogVersion: args.ExpectedCatalogVersion, ExpectedDraftVersion: draft.Version,
				Choices: draft.Evaluation.Choices,
			})
			require.NoError(t, err)
			args.ExpectedDraftVersion = saved.Version
			operation := uuid.New()
			prepared := prepareMealApp(t, f, session, operation, args)
			code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/submit",
				paymentAppJSON(t, orderAppSubmitBody(operation, prepared)), session.Token, appTestOrigin)
			require.Equal(t, http.StatusOK, code, string(raw))
			mealOrderCounts(t, f, 1, 1)
		})
	}
}
