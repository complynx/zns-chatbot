package sandbox

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func providerFaultTestFake() *Fake {
	f := &Fake{Token: "TOKEN", delay: &editDelay{
		key: strings.Repeat("k", 24), state: "idle", dataSlots: make(chan struct{}, delayDataConnections),
	}}
	f.delay.armGuard = f.providerFaultEditGuard
	return f
}

func providerFaultTestRequest(f *Fake, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("X-Sandbox", "1")
	r.Header.Set("X-R104-Control", f.delay.key)
	w := httptest.NewRecorder()
	f.Handler().ServeHTTP(w, r)
	return w
}

func providerFaultTestArm(t *testing.T, f *Fake, key string, spec providerFaultSpec) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(providerFaultRequest{Case: key, Action: "arm", providerFaultSpec: spec})
	require.NoError(t, err)
	return providerFaultTestRequest(f, http.MethodPost, "/lab/provider-fault", string(body))
}

func providerFaultTestSpec() providerFaultSpec {
	return providerFaultSpec{
		Mode:            providerFaultRateLimit,
		Method:          "sendMessage",
		Chat:            101,
		Count:           4,
		LifetimeSeconds: 600,
	}
}

func providerFaultTestRead(t *testing.T, f *Fake, key string) providerFaultCase {
	t.Helper()
	w := providerFaultTestRequest(f, http.MethodGet, "/lab/provider-fault?case="+key, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var item providerFaultCase
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &item))
	return item
}

func TestProviderFaultAuthAndStrictBounds(t *testing.T) {
	t.Parallel()
	f := providerFaultTestFake()
	w := httptest.NewRecorder()
	(&Fake{}).Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/lab/provider-fault?case=a", nil))
	require.Equal(t, http.StatusNotFound, w.Code)
	w = httptest.NewRecorder()
	f.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/lab/provider-fault?case=a", nil))
	require.Equal(t, http.StatusForbidden, w.Code)
	invalid := []string{
		`{"case":"a","action":"arm","mode":"rate_limit","method":"sendMessage","count":1,"lifetime_seconds":600,"unknown":true}`,
		`{"case":"a","action":"arm","mode":"rate_limit","method":"sendMessage","count":17,"lifetime_seconds":600}`,
		`{"case":"a","action":"arm","mode":"rate_limit","method":"sendMessage","count":1,"lifetime_seconds":601}`,
		`{"case":"a","action":"arm","mode":"rate_limit","method":"getUpdates","count":1,"lifetime_seconds":600}`,
		`{"case":"a","action":"arm","mode":"rate_limit","method":"sendMessage","chat":909,"count":1,"lifetime_seconds":600}`,
		`{"case":"a","action":"arm","mode":"credential","method":"sendMessage","chat":101,"count":1,"lifetime_seconds":600}`,
		`{"case":"a","action":"arm","mode":"rate_limit","method":"sendDocument","chat":-1009002,"thread":101,"count":1,"lifetime_seconds":600}`,
		`{"case":"a","action":"arm","mode":"rate_limit","method":"sendMessage","count":1,"lifetime_seconds":600} {}`,
		strings.Repeat(" ", 1025),
	}
	for _, body := range invalid {
		w = providerFaultTestRequest(f, http.MethodPost, "/lab/provider-fault", body)
		require.Equal(t, http.StatusBadRequest, w.Code, body)
	}
	require.Nil(t, f.providerFaults)
	for _, path := range []string{"/lab/provider-fault", "/lab/provider-fault?case=a&case=b"} {
		w = providerFaultTestRequest(f, http.MethodGet, path, "")
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
}

func TestProviderFaultCountsOnlyValidSelectedRequests(t *testing.T) {
	t.Parallel()
	f := providerFaultTestFake()
	spec := providerFaultTestSpec()
	spec.Chat, spec.Thread = forumID, 101
	retryAfter := int64(2)
	spec.RetryAfter = &retryAfter
	require.Equal(t, http.StatusOK, providerFaultTestArm(t, f, "repeated", spec).Code)
	for _, trial := range []struct {
		path, body string
		status     int
	}{
		{"/botWRONG/sendMessage", `{"chat_id":-1009002,"message_thread_id":101,"text":"private canary"}`, http.StatusUnauthorized},
		{"/botTOKEN/sendMessage", `{"chat_id":-1009002,"message_thread_id":101,"text":5}`, http.StatusBadRequest},
		{"/botTOKEN/sendMessage", `{"chat_id":909,"text":"private canary"}`, http.StatusBadRequest},
		{"/botTOKEN/sendMessage", `{"chat_id":-1009002,"message_thread_id":102,"text":"other topic"}`, http.StatusOK},
	} {
		w := providerFaultTestRequest(f, http.MethodPost, trial.path, trial.body)
		require.Equal(t, trial.status, w.Code, w.Body.String())
	}
	for range spec.Count {
		w := providerFaultTestRequest(
			f,
			http.MethodPost,
			"/botTOKEN/sendMessage",
			`{"chat_id":"@sandbox_forum","message_thread_id":101,"text":"private canary"}`,
		)
		require.Equal(t, http.StatusTooManyRequests, w.Code)
		require.JSONEq(
			t,
			`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":2}}`,
			w.Body.String(),
		)
	}
	item := providerFaultTestRead(t, f, "repeated")
	require.Equal(t, providerFaultExhausted, item.State)
	require.Zero(t, item.Remaining)
	require.Len(t, item.Consumptions, spec.Count)
	require.NoError(t, validateProviderFaults(f.providerFaults))
	require.Len(t, f.messages, 1)
	require.Equal(t, http.StatusOK, providerFaultTestArm(t, f, "repeated", spec).Code)
	w := providerFaultTestRequest(
		f,
		http.MethodPost,
		"/botTOKEN/sendMessage",
		`{"chat_id":-1009002,"message_thread_id":101,"text":"after exhaustion"}`,
	)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, providerFaultTestRead(t, f, "repeated").Consumptions, spec.Count)
	raw, err := json.Marshal(f.providerFaults)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private canary")
	require.NotContains(t, string(raw), "TOKEN")
}

func TestProviderFaultRetryAfterAndCredentialEnvelopes(t *testing.T) {
	t.Parallel()
	for _, retry := range []*int64{nil, new(int64(-1)), new(int64(math.MaxInt64))} {
		f := providerFaultTestFake()
		spec := providerFaultTestSpec()
		spec.RetryAfter = retry
		require.Equal(t, http.StatusOK, providerFaultTestArm(t, f, "raw", spec).Code)
		w := providerFaultTestRequest(
			f,
			http.MethodPost,
			"/botTOKEN/sendMessage",
			`{"chat_id":101,"text":"raw cooldown"}`,
		)
		require.Equal(t, http.StatusTooManyRequests, w.Code)
		var envelope struct {
			Parameters map[string]int64 `json:"parameters"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
		if retry == nil {
			require.Nil(t, envelope.Parameters)
		} else {
			require.Equal(t, *retry, envelope.Parameters["retry_after"])
		}
	}
	f := providerFaultTestFake()
	spec := providerFaultSpec{
		Mode:            providerFaultCredential,
		Method:          providerFaultAllDelivery,
		Count:           4,
		LifetimeSeconds: 600,
	}
	require.Equal(t, http.StatusOK, providerFaultTestArm(t, f, "credentials", spec).Code)
	for _, body := range []string{`{"chat_id":101,"text":"one"}`, `{"chat_id":202,"text":"two"}`, `{"chat_id":303,"message_id":7,"text":"edit"}`} {
		method := "sendMessage"
		if strings.Contains(body, "message_id") {
			method = editMessageTextMethod
		}
		w := providerFaultTestRequest(f, http.MethodPost, "/botTOKEN/"+method, body)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.JSONEq(t, `{"ok":false,"error_code":401,"description":"Unauthorized"}`, w.Body.String())
	}
	w := providerFaultTestRequest(f, http.MethodPost, "/botTOKEN/getMe", `{}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, providerFaultTestRead(t, f, "credentials").Consumptions, 3)
	require.Empty(t, f.messages)
}

func TestProviderFaultFiniteCustodyAndSnapshotValidation(t *testing.T) {
	t.Parallel()
	f := providerFaultTestFake()
	spec := providerFaultTestSpec()
	require.Equal(t, http.StatusOK, providerFaultTestArm(t, f, "first", spec).Code)
	original := providerFaultTestRead(t, f, "first")
	require.Equal(t, http.StatusOK, providerFaultTestArm(t, f, "first", spec).Code)
	require.Equal(t, original, providerFaultTestRead(t, f, "first"))
	changed := spec
	changed.Count++
	require.Equal(t, http.StatusConflict, providerFaultTestArm(t, f, "first", changed).Code)
	require.Equal(t, http.StatusConflict, providerFaultTestArm(t, f, "second", spec).Code)
	item := f.providerFaults.Cases["first"]
	item.ArmedAt = time.Now().UTC().Add(-601 * time.Second)
	item.Deadline = item.ArmedAt.Add(600 * time.Second)
	f.providerFaults.Cases["first"] = item
	require.Equal(t, providerFaultExpired, providerFaultTestRead(t, f, "first").State)
	require.Equal(t, http.StatusOK, providerFaultTestArm(t, f, "first", spec).Code)
	require.Equal(t, providerFaultExpired, providerFaultTestRead(t, f, "first").State)
	for _, key := range []string{"b", "c", "d", "e", "f", "g", "h"} {
		require.Equal(t, http.StatusOK, providerFaultTestArm(t, f, key, spec).Code)
		w := providerFaultTestRequest(
			f,
			http.MethodPost,
			"/lab/provider-fault",
			`{"case":"`+key+`","action":"release"}`,
		)
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(
			t,
			http.StatusOK,
			providerFaultTestRequest(
				f,
				http.MethodPost,
				"/lab/provider-fault",
				`{"case":"`+key+`","action":"release"}`,
			).Code,
		)
		require.Equal(t, http.StatusOK, providerFaultTestArm(t, f, key, spec).Code)
		require.Equal(t, providerFaultReleased, providerFaultTestRead(t, f, key).State)
	}
	require.Equal(t, http.StatusConflict, providerFaultTestArm(t, f, "ninth", spec).Code)
	require.NoError(t, validateProviderFaults(f.providerFaults))
	item = f.providerFaults.Cases["first"]
	item.Remaining++
	f.providerFaults.Cases["first"] = item
	require.Error(t, validateProviderFaults(f.providerFaults))
}

func TestProviderFaultConcurrentEditAndLegacyCustody(t *testing.T) {
	t.Parallel()
	for range 8 {
		f, d := delayTestFake(t, "replacement", delayModeBefore)
		d.state = "idle"
		d.armGuard = f.providerFaultEditGuard
		body, err := json.Marshal(d.arm)
		require.NoError(t, err)
		faultBody, err := json.Marshal(
			providerFaultRequest{Case: "race", Action: "arm", providerFaultSpec: providerFaultTestSpec()},
		)
		require.NoError(t, err)
		start := make(chan struct{})
		results := make(chan int, 2)
		var group sync.WaitGroup
		group.Go(func() {
			<-start
			results <- providerFaultTestRequest(f, http.MethodPost, "/lab/provider-fault", string(faultBody)).Code
		})
		group.Go(func() {
			<-start
			r := httptest.NewRequest(http.MethodPost, "/control/arm", strings.NewReader(string(body)))
			r.Header.Set("X-R104-Control", d.key)
			w := httptest.NewRecorder()
			d.control(w, r)
			results <- w.Code
		})
		close(start)
		group.Wait()
		require.ElementsMatch(t, []int{http.StatusOK, http.StatusConflict}, []int{<-results, <-results})
		require.False(t, f.providerFaultEditArm)
		require.Equal(
			t,
			http.StatusConflict,
			providerFaultTestRequest(f, http.MethodPost, "/lab/fault", `{"mode":"transient"}`).Code,
		)
	}
	f := providerFaultTestFake()
	require.Equal(
		t,
		http.StatusOK,
		providerFaultTestRequest(f, http.MethodPost, "/lab/fault", `{"mode":"transient"}`).Code,
	)
	require.Equal(t, http.StatusConflict, providerFaultTestArm(t, f, "blocked", providerFaultTestSpec()).Code)
	_, allowed := f.providerFaultEditGuard()
	require.False(t, allowed)
	require.Equal(
		t,
		http.StatusTooManyRequests,
		providerFaultTestRequest(f, http.MethodPost, "/botTOKEN/sendMessage", `{"chat_id":202,"text":"legacy"}`).Code,
	)
	require.Equal(t, http.StatusOK, providerFaultTestArm(t, f, "after", providerFaultTestSpec()).Code)
}

func providerFaultEditArm(t *testing.T, d *editDelay) int {
	t.Helper()
	body, err := json.Marshal(d.arm)
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/control/arm", strings.NewReader(string(body)))
	r.Header.Set("X-R104-Control", d.key)
	w := httptest.NewRecorder()
	d.control(w, r)
	return w.Code
}

func TestProviderFaultEditExclusionBothDirectionsAndFailedReservation(t *testing.T) {
	t.Parallel()
	f, d := delayTestFake(t, "replacement", delayModeBefore)
	d.state, d.armGuard = "idle", f.providerFaultEditGuard
	require.Equal(t, http.StatusOK, providerFaultTestArm(t, f, "first", providerFaultTestSpec()).Code)
	require.Equal(t, http.StatusConflict, providerFaultEditArm(t, d))
	require.False(t, f.providerFaultEditArm)
	w := providerFaultTestRequest(f, http.MethodPost, "/lab/provider-fault", `{"case":"first","action":"release"}`)
	require.Equal(t, http.StatusOK, w.Code)
	d.state = "completed"
	require.Equal(t, http.StatusConflict, providerFaultEditArm(t, d))
	require.False(t, f.providerFaultEditArm, "failed edit arm releases its reservation")
	d.state = "idle"
	require.Equal(
		t,
		http.StatusOK,
		providerFaultTestRequest(f, http.MethodPost, "/lab/fault", `{"mode":"transient"}`).Code,
	)
	require.Equal(t, http.StatusConflict, providerFaultEditArm(t, d))
	require.False(t, f.providerFaultEditArm)
	require.Equal(t, http.StatusOK, providerFaultTestRequest(f, http.MethodPost, "/lab/fault", `{"mode":"none"}`).Code)
	require.Equal(t, http.StatusOK, providerFaultEditArm(t, d))
	require.False(t, f.providerFaultEditArm, "successful edit arm releases its reservation")
	require.Equal(t, http.StatusConflict, providerFaultTestArm(t, f, "second", providerFaultTestSpec()).Code)
	require.Equal(
		t,
		http.StatusConflict,
		providerFaultTestRequest(f, http.MethodPost, "/lab/fault", `{"mode":"edit_missing"}`).Code,
	)
}
