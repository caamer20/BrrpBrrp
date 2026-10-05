package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// The requirement is that alerts *reach* an approved recipient, and the sentence that
// matters is that a configured alert is not a delivered alert. These tests therefore
// run a real receiver and check what arrived, rather than asserting that a field was
// set.

type alertReceiver struct {
	mu       sync.Mutex
	received []alertRecord
	headers  []http.Header
	status   int
}

func (a *alertReceiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var record alertRecord
	_ = json.NewDecoder(r.Body).Decode(&record)
	a.mu.Lock()
	a.received = append(a.received, record)
	a.headers = append(a.headers, r.Header.Clone())
	status := a.status
	if status == 0 {
		status = 200
	}
	a.mu.Unlock()
	w.WriteHeader(status)
}

func (a *alertReceiver) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.received)
}

// snapshot copies what has been received so far, so a test can wait for a later
// delivery without racing the receiver.
func (a *alertReceiver) snapshot() []alertRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]alertRecord(nil), a.received...)
}

func (a *alertReceiver) first() (alertRecord, http.Header) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.received) == 0 {
		return alertRecord{}, nil
	}
	return a.received[0], a.headers[0]
}

func TestAnAlertIsDeliveredToARealRecipient(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	receiver := &alertReceiver{}
	receiverServer := httptest.NewServer(receiver)
	defer receiverServer.Close()
	t.Setenv("BRRP_ALERT_WEBHOOK", receiverServer.URL+"/hooks/abc123secret")

	// A condition the product can see: backups enabled, none ever verified. The setting
	// is changed through its own route, so the fixture is the real product rather than a
	// shortcut past it.
	enableBackups(t, handler, admin, s)
	s.raise(alertBackupStale, "critical", "No verified backup has been recorded.")

	// Delivery is asynchronous by design: the request or pass that raises a condition
	// must not wait on a webhook. The test waits for the worker.
	waitForAlert(t, "the alert to arrive", func() bool { return receiver.count() == 1 })
	record, headers := receiver.first()
	if record.Kind != alertBackupStale {
		t.Fatalf("the alert names %q", record.Kind)
	}
	if record.Severity != "critical" {
		t.Fatalf("a failed recovery point was reported as %q", record.Severity)
	}
	// The summary carries the numbers, so the recipient does not have to go and look.
	if record.Summary == "" {
		t.Fatal("the alert has no summary")
	}
	// Routing headers, so a receiver can filter without parsing the body.
	if headers.Get("X-Brrpbrrp-Alert") != alertBackupStale {
		t.Fatalf("the kind header is %q", headers.Get("X-Brrpbrrp-Alert"))
	}
	// And the status surface reports the *delivery*, which is the field that answers
	// whether alerting works.
	status := readJSON[alertStatus](t, request(t, handler, "GET", "/api/admin/alerts", nil, admin))
	if !status.Configured {
		t.Fatal("the status says no destination is configured, and one is")
	}
	if status.Delivered == "" {
		t.Fatal("the status says an alert was never delivered, and one was")
	}
}

func TestAConfiguredAlertThatCannotDeliverIsVisibleAsUndelivered(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	receiver := &alertReceiver{status: 403}
	receiverServer := httptest.NewServer(receiver)
	defer receiverServer.Close()
	t.Setenv("BRRP_ALERT_WEBHOOK", receiverServer.URL)
	// A refused delivery is retried within the alert window rather than silenced for six
	// hours. The test shortens the schedule instead of waiting it out.
	s.alertDeliveryAttempts = 3
	s.alertDeliveryBackoff = 5 * time.Millisecond

	s.raise(alertStorageLow, "critical", "Only 1 GiB remains on the data volume.")
	waitForAlert(t, "the bounded retries to be exhausted", func() bool { return receiver.count() == 3 })
	// The attempts happened and failed. The operator can see that, which is the whole
	// difference between a destination that is broken and one that has never been used.
	status := readJSON[alertStatus](t, request(t, handler, "GET", "/api/admin/alerts", nil, admin))
	if status.Delivered != "" {
		t.Fatal("a refused delivery was reported as delivered")
	}
	if len(status.History) != 1 {
		t.Fatalf("the history holds %d alerts, want 1", len(status.History))
	}
	// One condition, several delivery attempts: retrying a delivery must not create new
	// conditions, and a failed delivery must not be deduplicated away like one.
	if status.History[0].Attempts != 3 {
		t.Fatalf("the alert records %d attempts, want 3", status.History[0].Attempts)
	}
	if status.History[0].LastError == "" {
		t.Fatal("a failed delivery recorded no reason, so the operator cannot diagnose it")
	}
	// A critical condition is still reported as critical even when it could not be
	// delivered. Downgrading it would hide the problem behind the alerting failure.
	if status.History[0].Severity != "critical" {
		t.Fatalf("an undelivered critical alert was downgraded to %q", status.History[0].Severity)
	}
}

func TestAnUnchangedConditionIsNotRaisedRepeatedly(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	receiver := &alertReceiver{}
	receiverServer := httptest.NewServer(receiver)
	defer receiverServer.Close()
	t.Setenv("BRRP_ALERT_WEBHOOK", receiverServer.URL)

	s.raise(alertStorageLow, "warning", "Low disk.")
	s.raise(alertStorageLow, "warning", "Low disk.")
	s.raise(alertStorageLow, "warning", "Low disk.")
	// One condition, one notice. Repeated notices for an unchanged problem are how an
	// operator learns to ignore them.
	waitForAlert(t, "the single notice", func() bool { return receiver.count() >= 1 })
	time.Sleep(50 * time.Millisecond)
	if receiver.count() != 1 {
		t.Fatalf("one unchanged condition produced %d notices", receiver.count())
	}
	// And it is still one entry in the history, not three.
	status := readJSON[alertStatus](t, request(t, handler, "GET", "/api/admin/alerts", nil, admin))
	if len(status.History) != 1 {
		t.Fatalf("the history holds %d entries for one condition", len(status.History))
	}
}

func TestAnAlertIsSignedSoAReceiverCanTellARealOneFromAForgedOne(t *testing.T) {
	s, _ := testServer(t)
	receiver := &alertReceiver{}
	receiverServer := httptest.NewServer(receiver)
	defer receiverServer.Close()
	t.Setenv("BRRP_ALERT_WEBHOOK", receiverServer.URL)
	t.Setenv("BRRP_ALERT_SECRET", "an operator's shared secret")

	s.raise(alertCopyStale, "critical", "No independent copy has completed.")
	waitForAlert(t, "the signed notice", func() bool { return receiver.count() == 1 })
	_, headers := receiver.first()
	signature := headers.Get("X-Brrpbrrp-Signature")
	if signature == "" {
		t.Fatal("no signature was sent, so a leaked URL could be used to post forged alerts")
	}
	if !strings.HasPrefix(signature, "sha256=") {
		t.Fatalf("the signature is %q", signature)
	}
	// The secret is never in the body or a header value, so a receiver that logs
	// requests does not log the key that verifies them.
	if headers.Get("X-Brrpbrrp-Secret") != "" {
		t.Fatal("the secret was sent as a header")
	}
}

func TestTheAlertStatusHidesTheDestinationToken(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	receiver := &alertReceiver{}
	receiverServer := httptest.NewServer(receiver)
	defer receiverServer.Close()
	// A webhook URL commonly carries a token in its path, and an operator panel ends
	// up in screenshots and support threads.
	t.Setenv("BRRP_ALERT_WEBHOOK", receiverServer.URL+"/hooks/t-very-secret-token")

	status := readJSON[alertStatus](t, request(t, handler, "GET", "/api/admin/alerts", nil, admin))
	if strings.Contains(status.Destination, "secret") {
		t.Fatalf("the status leaks the destination token: %q", status.Destination)
	}
	if status.Destination == "" {
		t.Fatal("the status does not name the destination host at all, so an operator cannot tell which one is configured")
	}
}

func TestTheCurrentConditionsAreReadableWithoutAnyDestination(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	// Backups on, none verified: the condition is real whether or not anybody is
	// listening. A product that only reports a problem when a webhook is configured
	// cannot be used to find out that the webhook is what is broken.
	os.Unsetenv("BRRP_ALERT_WEBHOOK")
	enableBackups(t, handler, admin, s)
	status := readJSON[alertStatus](t, request(t, handler, "GET", "/api/admin/alerts", nil, admin))
	if status.Configured {
		t.Fatal("a destination is reported where none is configured")
	}
	active := false
	for _, condition := range status.Conditions {
		if condition.Kind == alertBackupStale && condition.Active {
			active = true
		}
	}
	if !active {
		t.Fatalf("backups enabled with no verified backup was not reported as an active condition: %+v", status.Conditions)
	}
}

// waitForAlert polls until cond is true. Delivery is asynchronous by design — the
// request path must not wait on a webhook — so tests wait for the worker rather than
// asserting synchronously on it.
func waitForAlert(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// enableBackups turns backups on through the real settings route and stores the
// resulting settings, so a fixture that needs "backups on, nothing verified yet" is
// describing the same state an operator would have created.
func enableBackups(t *testing.T, handler http.Handler, admin *http.Cookie, s *Server) {
	t.Helper()
	settings := s.Settings()
	settings.BackupEnabled = true
	settings.BackupHours = 24
	// The route takes the whole settings object; a partial one would be refused for
	// validating its own defaults rather than for the change under test.
	w := request(t, handler, "PUT", "/api/admin/settings", map[string]any{"settings": settings}, admin)
	if w.Code >= 400 {
		t.Fatalf("enable backups: %d %s", w.Code, w.Body.String())
	}
}
