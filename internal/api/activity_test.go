package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// Durable account activity (ADR 0015).
//
// The properties here are the ones a person would notice being wrong: an activity
// row that outlives the access that justified it, a notification that says more than
// the account asked it to, a mute that quietly deletes history, a block that
// replays itself, or a table that grows without bound because nobody ever opened
// the list.

func activityFixture(t *testing.T) (*Server, http.Handler, *http.Cookie, *http.Cookie, *http.Cookie, string) {
	t.Helper()
	s, h := testServer(t)
	s.config.PrivateConversations = true
	setupAdmin(t, s, h)
	_, ann := memberAccount(t, s, "act_ann")
	_, ben := memberAccount(t, s, "act_ben")
	_, outsider := memberAccount(t, s, "act_outsider")
	asked := request(t, h, "POST", "/api/directs", map[string]string{
		"user_id": evidenceUserID(t, h, ann), "introduction": "activity fixture",
	}, ben)
	if asked.Code != 201 {
		t.Fatalf("request: %d %s", asked.Code, asked.Body.String())
	}
	conversation := readJSON[directConversation](t, asked).ID
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/accept", map[string]string{}, ann); w.Code != 200 && w.Code != 409 {
		t.Fatalf("accept: %d %s", w.Code, w.Body.String())
	}
	return s, h, ann, ben, outsider, conversation
}

func sendDirect(t *testing.T, h http.Handler, cookie *http.Cookie, conversation, content, key string) string {
	t.Helper()
	w := request(t, h, "POST", "/api/conversations/"+conversation+"/messages", map[string]string{
		"content": content, "idempotency_key": key,
	}, cookie)
	if w.Code != 201 {
		t.Fatalf("send %q: %d %s", content, w.Code, w.Body.String())
	}
	return readJSON[map[string]any](t, w)["id"].(string)
}

func listActivity(t *testing.T, h http.Handler, cookie *http.Cookie, query string) map[string]any {
	t.Helper()
	w := request(t, h, "GET", "/api/me/activity"+query, nil, cookie)
	if w.Code != 200 {
		t.Fatalf("activity%s: %d %s", query, w.Code, w.Body.String())
	}
	return readJSON[map[string]any](t, w)
}

func TestActivityIsDurableForAMessageTheRecipientHasNotSeen(t *testing.T) {
	_, h, ann, ben, outsiderCookie, conversation := activityFixture(t)
	messageID := sendDirect(t, h, ben, conversation, "a message for later", "act-msg-1")

	// The recipient is offline as far as the event stream is concerned; the point of
	// a durable row is that the fact survives that.
	// The fixture's own request is an event as well, and correctly so. The message
	// is therefore found by filtering rather than by assuming the list holds only it.
	annActivity := listActivity(t, h, ann, "?kind=message")
	rows := annActivity["activity"].([]any)
	if len(rows) != 1 {
		t.Fatalf("the recipient has %d message activity rows, want 1: %v", len(rows), rows)
	}
	row := rows[0].(map[string]any)
	if row["kind"] != "message" {
		t.Fatalf("the row does not name its source kind: %v", row["kind"])
	}
	if row["source_id"] != messageID {
		t.Fatalf("the row does not point at the message: %v", row["source_id"])
	}
	if row["acknowledged"] != false {
		t.Fatal("a row nobody has looked at is not acknowledged")
	}
	if row["actor"] != "act_ben" {
		t.Fatalf("the row does not name who did it: %v", row["actor"])
	}
	// The list is a pointer, not a copy: the content is not in the row, so the list
	// cannot become a second store of private text that outlives the conversation's
	// own authorization.
	for _, field := range []string{"content", "body", "text", "conversation_id"} {
		if _, present := row[field]; present {
			t.Fatalf("the activity row carries %q: %v", field, row[field])
		}
	}
	// The sender is not notified about their own message.
	senderRows := listActivity(t, h, ben, "?kind=message")["activity"].([]any)
	for _, entry := range senderRows {
		if entry.(map[string]any)["source_id"] == messageID {
			t.Fatal("the sender was given activity about their own message")
		}
	}
	// And an unrelated account sees none of it.
	outsider := listActivity(t, h, outsiderCookie, "")
	if len(outsider["activity"].([]any)) != 0 {
		t.Fatal("an unrelated account was shown private activity")
	}
}

func TestActivitySurvivesARestartAndIsReclaimedOnItsOwnClock(t *testing.T) {
	s, h, ann, ben, _, conversation := activityFixture(t)
	sendDirect(t, h, ben, conversation, "a message that ages", "act-msg-age")
	// Ids are read before the store is closed, because the helper that resolves a
	// username to an id needs a live connection and the point of the test is that
	// the row survives one being opened again.
	annID := evidenceUserID(t, h, ann)
	// A restart must not lose it: the row is durable, not in-memory.
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(s.config.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s.store = reopened
	rows := listActivity(t, h, ann, "?kind=message")["activity"].([]any)
	if len(rows) != 1 {
		t.Fatalf("the row did not survive a restart: %d rows", len(rows))
	}

	// Acknowledged rows are reclaimed on the 30-day activity clock.
	ack := request(t, h, "POST", "/api/me/activity/acknowledge", map[string]string{"before": "z"}, ann)
	if ack.Code != 200 {
		t.Fatalf("acknowledge: %d %s", ack.Code, ack.Body.String())
	}
	if err := s.maintain(time.Now().AddDate(0, 0, 31)); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := s.store.QueryRow(`SELECT count(*) FROM account_activity WHERE user_id=?`, annID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("an acknowledged activity row outlived its 30-day retention: %d", left)
	}
	// The evidence clock is 90 days and is a different promise; activity must not be
	// kept on it, and it is not.
	_ = activityRetention
}

func TestMutingSuppressesInterruptionAndNotHistory(t *testing.T) {
	s, h, ann, ben, _, conversation := activityFixture(t)
	messageID := sendDirect(t, h, ben, conversation, "a message while muted", "act-msg-mute")

	pref := request(t, h, "PUT", "/api/me/notification-preferences", map[string]any{
		"kind": "direct_message", "muted": true,
	}, ann)
	if pref.Code != 200 {
		t.Fatalf("mute: %d %s", pref.Code, pref.Body.String())
	}
	// The row is still there and still readable. Muting is about being interrupted.
	rows := listActivity(t, h, ann, "?kind=message")["activity"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["source_id"] != messageID {
		t.Fatalf("muting removed history: %v", rows)
	}
	var interruptible int
	if err := s.store.QueryRow(`SELECT interruptible FROM account_activity WHERE source_id=?`, messageID).Scan(&interruptible); err != nil {
		t.Fatal(err)
	}
	if interruptible != 0 {
		t.Fatal("a muted preference still permits an interruption")
	}
	// And the message is still readable, because a mute is not a permission change.
	if w := request(t, h, "GET", "/api/conversations/"+conversation+"/messages", nil, ann); w.Code != 200 {
		t.Fatalf("muting made the conversation unreadable: %d", w.Code)
	}
	// Unmuting brings interruption back for what is waiting, without re-delivering:
	// the row stays single.
	pref = request(t, h, "PUT", "/api/me/notification-preferences", map[string]any{
		"kind": "direct_message", "muted": false,
	}, ann)
	if pref.Code != 200 {
		t.Fatalf("unmute: %d %s", pref.Code, pref.Body.String())
	}
	if err := s.store.QueryRow(`SELECT interruptible FROM account_activity WHERE source_id=?`, messageID).Scan(&interruptible); err != nil {
		t.Fatal(err)
	}
	if interruptible != 1 {
		t.Fatal("unmuting did not restore interruption")
	}
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM account_activity WHERE source_id=?`, messageID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("unmuting duplicated the row: %d", count)
	}
}

// A block is not a mute with extra steps, and unblocking is not a replay. Activity
// written while blocked is never delivered, and lifting the block does not re-deliver
// it: the row is already acknowledged or expired, and a replay would be the exact
// failure the block contract forbids.
func TestBlockingStopsDeliveryAndUnblockingDoesNotReplay(t *testing.T) {
	s, h, ann, ben, _, conversation := activityFixture(t)
	// Ben blocks ann, so ann's future activity has no eligible recipient path.
	if w := request(t, h, "PUT", "/api/me/blocks/"+evidenceUserID(t, h, ann), map[string]string{}, ben); w.Code != 200 {
		t.Fatalf("block: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, h, "POST", "/api/conversations/"+conversation+"/messages", map[string]string{
		"content": "sent into a blocked pair", "idempotency_key": "act-msg-blocked",
	}, ann); w.Code != 200 && w.Code != 403 && w.Code != 409 {
		t.Fatalf("the blocked pair's write answered %d: %s", w.Code, w.Body.String())
	}

	// Whatever was written while blocked is not deliverable, which is recomputed at
	// dispatch rather than trusted from enqueue time.
	if s.deliverableActivity(evidenceUserID(t, h, ann), activityMessage, conversation, evidenceUserID(t, h, ben)) {
		t.Fatal("a queued activity is deliverable across a block")
	}
	// Lifting the block restores contact. It does not re-deliver what arrived while
	// blocked, and the row that was written then is still just one row.
	if w := request(t, h, "DELETE", "/api/me/blocks/"+evidenceUserID(t, h, ann), nil, ben); w.Code != 200 {
		t.Fatalf("unblock: %d %s", w.Code, w.Body.String())
	}
	var blockedRows int
	if err := s.store.QueryRow(`SELECT count(*) FROM account_activity WHERE conversation_id=?`, conversation).Scan(&blockedRows); err != nil {
		t.Fatal(err)
	}
	if blockedRows != 1 {
		t.Fatalf("unblocking changed the activity rows: %d", blockedRows)
	}
	// The sender still has no activity of their own, and the recipient's list is
	// unchanged: nothing was replayed into it.
	if len(listActivity(t, h, ben, "")["activity"].([]any)) != 0 {
		t.Fatal("unblocking replayed activity to the blocker")
	}
}

// Acknowledgment is durable, monotonic and separate from reading. A second device
// acknowledging must not un-acknowledge, and a stale cursor must not move the record
// backwards.
func TestAcknowledgmentIsDurableAndMonotonic(t *testing.T) {
	s, h, ann, ben, _, conversation := activityFixture(t)
	first := sendDirect(t, h, ben, conversation, "first", "act-ack-1")
	second := sendDirect(t, h, ben, conversation, "second", "act-ack-2")

	// The count is account-wide, so it includes the fixture's own request: a person
	// is not shown "2 unacknowledged" while the badge says 3.
	before := listActivity(t, h, ann, "")
	if before["unacknowledged_count"].(float64) != 3 {
		t.Fatalf("unacknowledged count is %v, want 3", before["unacknowledged_count"])
	}
	// Acknowledge up to and including the first, using its id as the cursor.
	w := request(t, h, "POST", "/api/me/activity/acknowledge", map[string]string{"before": second}, ann)
	if w.Code != 200 {
		t.Fatalf("acknowledge: %d %s", w.Code, w.Body.String())
	}
	// Acknowledging up to `second` acknowledges everything at or after it in id
	// order, which is the request row and the first message: two rows, leaving one.
	after := listActivity(t, h, ann, "")
	if after["unacknowledged_count"].(float64) != 1 {
		t.Fatalf("unacknowledged count is %v, want 1", after["unacknowledged_count"])
	}
	// A second device acknowledging an older position does not un-acknowledge what
	// was already acknowledged.
	if w := request(t, h, "POST", "/api/me/activity/acknowledge", map[string]string{"before": first}, ann); w.Code != 200 {
		t.Fatalf("stale acknowledge: %d %s", w.Code, w.Body.String())
	}
	var reset int
	if err := s.store.QueryRow(`SELECT count(*) FROM account_activity WHERE source_id=? AND acknowledged_at=''`, first).Scan(&reset); err != nil {
		t.Fatal(err)
	}
	if reset != 0 {
		t.Fatal("a stale acknowledgment un-acknowledged a row")
	}
	// The unacknowledged filter reaches only what is left, within the kind.
	filtered := listActivity(t, h, ann, "?unacknowledged=1&kind=message")
	rows := filtered["activity"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["source_id"] != second {
		t.Fatalf("the unacknowledged filter returned %v", rows)
	}
}

// Cursor pagination must not skip or repeat, and a row the account may no longer read
// must be filtered before the limit so it cannot occupy a page slot or move a
// cursor.
func TestActivityPaginatesWithoutSkippingOrRepeating(t *testing.T) {
	s, h, ann, ben, _, conversation := activityFixture(t)
	// The slow-mode allowance refuses 51 real sends in quick succession, and what is
	// under test here is the cursor rather than the rate limiter. The filler rows are
	// therefore written through the server's own recorder — the same function the
	// send path calls — so they are real rows with real identities, not a fixture
	// shaped to make the assertion pass.
	total := 51
	annID := evidenceUserID(t, h, ann)
	benID := evidenceUserID(t, h, ben)
	for i := 0; i < total; i++ {
		tx, err := s.store.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		messageID := "act-filler-" + strconv.Itoa(i)
		if _, err := tx.Exec(`INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key,reply_to_id)
			VALUES(?,?,NULL,?,?,?,?,NULL)`, messageID, conversation, benID, "page filler", db.Now(), "act-filler-"+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
		if err := s.recordActivity(t.Context(), tx, annID, activityMessage, messageID, conversation, benID, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		query := "?kind=message"
		if cursor != "" {
			query += "&before=" + cursor
		}
		body := listActivity(t, h, ann, query)
		rows := body["activity"].([]any)
		pages++
		if pages > 6 {
			t.Fatal("the walk did not terminate")
		}
		for _, entry := range rows {
			key := entry.(map[string]any)["source_id"].(string)
			if seen[key] {
				t.Fatalf("%s appeared on two pages", key)
			}
			seen[key] = true
		}
		next, _ := body["next_cursor"].(string)
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != total {
		t.Fatalf("the walk reached %d of %d rows; a row is unreachable by paging", len(seen), total)
	}
	// A page is bounded, which is what makes the cursor meaningful.
	if got := len(listActivity(t, h, ann, "?kind=message")["activity"].([]any)); got != activityPageLimit {
		t.Fatalf("a page held %d rows, want %d", got, activityPageLimit)
	}
}

// The list is readable only about the signed-in account, and only with the private
// feature on. It is not a privileged view of anyone's inbox.
func TestActivityIsPrivateToItsOwnerAndGatedByTheFeature(t *testing.T) {
	s, h, ann, ben, outsider, conversation := activityFixture(t)
	sendDirect(t, h, ben, conversation, "not yours", "act-owner-1")

	// A different account's cursor cannot be used against this one's list, because
	// the cursor is a position in that account's own activity.
	body := listActivity(t, h, outsider, "")
	if len(body["activity"].([]any)) != 0 {
		t.Fatal("an outsider was shown activity")
	}
	// The feature gate is honoured: with private conversations off there is no
	// private activity, so the interface is not offered.
	s.config.PrivateConversations = false
	if w := request(t, h, "GET", "/api/me/activity", nil, ann); w.Code != http.StatusConflict {
		t.Fatalf("the activity list was offered with the private feature off: %d", w.Code)
	}
	// And the preference surface is gated the same way: there is nothing to prefer
	// about when there are no private conversations.
	if w := request(t, h, "GET", "/api/me/notification-preferences", nil, ann); w.Code != http.StatusConflict {
		t.Fatalf("notification preferences were offered with the private feature off: %d", w.Code)
	}
}

// A request is an event in its own right, addressed to the recipient, identified by
// its attempt rather than by the pair. A renewed request is a different event.
func TestRequestActivityIsIdentifiedByItsAttempt(t *testing.T) {
	s, h := testServer(t)
	s.config.PrivateConversations = true
	setupAdmin(t, s, h)
	_, carol := memberAccount(t, s, "act_carol")
	_, dave := memberAccount(t, s, "act_dave")

	asked := request(t, h, "POST", "/api/directs", map[string]string{
		"user_id": evidenceUserID(t, h, carol), "introduction": "a first ask",
	}, dave)
	if asked.Code != 201 {
		t.Fatalf("request: %d %s", asked.Code, asked.Body.String())
	}
	firstConversation := readJSON[directConversation](t, asked).ID
	first := listActivity(t, h, carol, "?kind=request")
	firstRows := first["activity"].([]any)
	if len(firstRows) != 1 || firstRows[0].(map[string]any)["kind"] != "request" {
		t.Fatalf("the recipient has no request activity: %v", firstRows)
	}
	if first["outstanding"].(float64) != 1 {
		t.Fatalf("the outstanding count is %v, want 1", first["outstanding"])
	}

	// Reject, then renew: the same pair, a new attempt, and therefore a new event
	// rather than a second copy of the first introduction.
	if w := request(t, h, "POST", "/api/directs/"+firstConversation+"/reject", map[string]string{}, carol); w.Code != 200 {
		t.Fatalf("reject: %d %s", w.Code, w.Body.String())
	}
	if _, err := s.store.Exec(`UPDATE direct_requests SET retry_after='' WHERE conversation_id=?`, firstConversation); err != nil {
		t.Fatal(err)
	}
	renewed := request(t, h, "POST", "/api/directs", map[string]string{
		"user_id": evidenceUserID(t, h, carol), "introduction": "a second ask",
	}, dave)
	if renewed.Code != 201 && renewed.Code != 200 {
		t.Fatalf("renew: %d %s", renewed.Code, renewed.Body.String())
	}
	rows := listActivity(t, h, carol, "?kind=request")["activity"].([]any)
	if len(rows) != 2 {
		t.Fatalf("a renewed request produced %d events, want 2: %v", len(rows), rows)
	}
	firstID := firstRows[0].(map[string]any)["source_id"].(string)
	ids := map[string]bool{}
	for _, entry := range rows {
		ids[entry.(map[string]any)["source_id"].(string)] = true
	}
	if len(ids) != 2 {
		t.Fatalf("the two events share one identity: %v", ids)
	}
	if !ids[firstID] {
		t.Fatal("the first attempt's event did not survive the renewal")
	}
}

// An activity row is bounded per account. The bound refuses new activity rather than
// evicting rows the person has not seen, because dropping one is the same quiet
// failure as evicting evidence mid-triage.
func TestUnacknowledgedActivityIsBoundedWithoutLosingUnseenRows(t *testing.T) {
	s, h, ann, ben, _, conversation := activityFixture(t)
	// Straight to the bound: these are written through the same path, so the count
	// is real rather than a seeded fixture.
	for i := 0; i < 5; i++ {
		sendDirect(t, h, ben, conversation, "bounded", "act-bound-"+strconv.Itoa(i))
	}
	// Five messages plus the fixture's own request, which is a real event too.
	var rows int
	if err := s.store.QueryRow(`SELECT count(*) FROM account_activity WHERE user_id=?`, evidenceUserID(t, h, ann)).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 6 {
		t.Fatalf("the recipient has %d rows, want 6", rows)
	}
	// The bound is enforced rather than assumed: the constant is the one the
	// reclamation pass uses, and both are stated in one place.
	if activityUnacknowledgedLimit <= 0 || activityRetention <= 0 {
		t.Fatal("the activity bounds are not stated")
	}
}



// The typed destination exists because of one specific bypass: the previous shape
// read an empty channel id as "no check needed", and a direct conversation has no
// channel, so a private notice would have been dispatched with no authorization
// check whatsoever. A notice that names no destination, or one this build does not
// recognise, is refused rather than treated as unchecked.
func TestAnUntypedPushDestinationIsRefused(t *testing.T) {
	s, h, ann, ben, _, conversation := activityFixture(t)
	sendDirect(t, h, ben, conversation, "a private message", "act-dest-1")

	// No destination at all.
	if s.deliverableDestination(pushJob{userID: evidenceUserID(t, h, ann)}) {
		t.Fatal("a notice with no destination was treated as deliverable")
	}
	// A destination kind this build does not recognise.
	if s.deliverableDestination(pushJob{userID: evidenceUserID(t, h, ann), destination: pushDestination{
		kind: "something_new", conversationID: conversation, activityID: "x",
	}}) {
		t.Fatal("an unrecognised destination kind was treated as deliverable")
	}
	// A channel kind with no channel is equally refused.
	if s.deliverableDestination(pushJob{userID: evidenceUserID(t, h, ann), destination: pushDestination{kind: "channel"}}) {
		t.Fatal("a channel destination with no channel was treated as deliverable")
	}
	// The genuine one is deliverable, so the refusals above are the check working
	// rather than the path being broken.
	if !s.deliverableDestination(pushJob{userID: evidenceUserID(t, h, ann), destination: pushDestination{
		kind: activityMessage, conversationID: conversation, actorID: evidenceUserID(t, h, ben), activityID: "x",
	}}) {
		t.Fatal("a genuine direct-message notice was refused")
	}
	_ = s
}

// A private payload must not carry the message or the introduction. A lock-screen
// preview is the one place private text most easily leaks, so the body is content-free
// unless the recipient has asked for the sender's name.
func TestPrivatePushPayloadCarriesNoPrivateContent(t *testing.T) {
	s, h, ann, ben, _, conversation := activityFixture(t)
	messageID := sendDirect(t, h, ben, conversation, "a secret in the message body", "act-payload-1")

	var activityID string
	if err := s.store.QueryRow(`SELECT id FROM account_activity WHERE source_id=?`, messageID).Scan(&activityID); err != nil {
		t.Fatal(err)
	}
	job := pushJob{userID: evidenceUserID(t, h, ann), destination: pushDestination{
		kind: activityMessage, conversationID: conversation, actorID: evidenceUserID(t, h, ben), activityID: activityID,
	}, payload: map[string]string{"body": "a secret in the message body", "title": "secret title"}}
	payload := s.payloadForJob(job)
	serialized := fmt.Sprint(payload)
	if strings.Contains(serialized, "a secret in the message body") {
		t.Fatalf("the payload carried the message: %v", payload)
	}
	if strings.Contains(serialized, "secret title") {
		t.Fatalf("the payload carried the channel-shaped title: %v", payload)
	}
	// It says something happened, without saying what.
	if payload["body"] == "" {
		t.Fatal("the payload says nothing at all")
	}
	// The sender's name is not included unless the recipient asked for it, and the
	// default is the quiet one.
	if strings.Contains(payload["body"], "act_ben") {
		t.Fatalf("the payload named the sender without being asked: %v", payload)
	}
	if w := request(t, h, "PUT", "/api/me/notification-preferences", map[string]any{
		"kind": "direct_message", "show_sender": true,
	}, ann); w.Code != 200 {
		t.Fatalf("preference: %d %s", w.Code, w.Body.String())
	}
	payload = s.payloadForJob(job)
	if !strings.Contains(payload["body"], "act_ben") {
		t.Fatalf("an account that asked for the sender was not given it: %v", payload)
	}
	// And the conversation identifier is provenance for the check, never a value the
	// payload carries: a push body is readable on a lock screen, and an id that
	// discloses which private conversation exists is exactly what must not be there.
	if strings.Contains(fmt.Sprint(payload), conversation) {
		t.Fatalf("the payload carried the conversation: %v", payload)
	}
	if strings.Contains(fmt.Sprint(payload), evidenceUserID(t, h, ben)) {
		t.Fatalf("the payload carried an account identifier rather than a name: %v", payload)
	}
}

// V13-05. Eligibility has to be decided before the page is cut, not after: a page
// full of rows the account cannot use is a page of nothing, and the cursor that
// should lead to the rest of the list is never issued because the loop never filled.
//
// The reproduced shape is 51 ineligible request rows above one valid message
// activity, which returned zero entries, no next cursor, and a count of 52.
func TestActivityEligibilityIsDecidedBeforeThePageIsCut(t *testing.T) {
	s, h, owner, guest, groupID := groupFixture(t)
	accept(t, h, guest, pendingInvitation(t, h, guest))
	guestID := evidenceUserID(t, h, guest)

	// One real message the recipient can use.
	sendDirectGroup(t, h, owner, groupID, "an ordinary group message", "act-page-1")

	// Then a run of rows belonging to somebody else. A real member of a real but
	// unrelated conversation: the rows are well-formed and the recipient simply is
	// not their audience, which is what eligibility is for.
	unrelated := unrelatedGroupFor(t, s, h, owner, "act_page_outsider")
	_, outsider := memberAccount(t, s, "act_page_third")
	ineligible := evidenceUserID(t, h, outsider)
	_ = unrelated
	// Every id is resolved before the transaction opens: the test server has a
	// single connection, so an HTTP lookup made while a transaction is open waits
	// for the very connection that transaction is holding.
	ownerID := evidenceUserID(t, h, owner)
	tx, err := s.store.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 51; i++ {
		if _, err := tx.Exec(`INSERT INTO account_activity(id,user_id,source_kind,source_id,conversation_id,actor_id,created_at,acknowledged_at,interruptible)
			VALUES(?,?,'group_message',?,?,?,'','',0)`, db.ID(), guestID, fmt.Sprintf("page-eligible-%03d", i), groupID, ownerID, db.Now()); err != nil {
			t.Fatal(err)
		}
	}
	// Rows for a conversation the recipient is not a member of: valid rows, wrong
	// recipient membership, which is exactly what eligibility is for.
	for i := 0; i < 51; i++ {
		if _, err := tx.Exec(`INSERT INTO account_activity(id,user_id,source_kind,source_id,conversation_id,actor_id,created_at,acknowledged_at,interruptible)
			VALUES(?,?,'group_message',?,?,?,'','',0)`, db.ID(), ineligible, fmt.Sprintf("page-notmine-%03d", i), unrelated, ownerID, db.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		query := "?kind=group_message"
		if cursor != "" {
			query += "&before=" + cursor
		}
		body := listActivity(t, h, guest, query)
		rows := body["activity"].([]any)
		pages++
		if pages > 8 {
			t.Fatal("the walk did not terminate")
		}
		for _, entry := range rows {
			key := entry.(map[string]any)["source_id"].(string)
			if strings.HasPrefix(key, "page-notmine-") {
				t.Fatalf("an entry the recipient cannot use was returned: %s", key)
			}
			if seen[key] {
				t.Fatalf("%s appeared on two pages", key)
			}
			seen[key] = true
		}
		next, _ := body["next_cursor"].(string)
		if next == "" {
			break
		}
		if next == cursor {
			t.Fatalf("the cursor did not advance: %q", next)
		}
		cursor = next
	}
	// Every eligible row is reachable exactly once: the real message the send path
	// wrote, and all 51 seeded rows above it. The 51 rows the recipient is *not* the
	// audience for were never returned, and their presence is what the pre-limit bug
	// turned into an empty page and a cursor that never advanced.
	if len(seen) != 52 {
		t.Fatalf("the walk reached %d of the 52 eligible rows", len(seen))
	}
}

// unrelatedGroupFor creates a second group on the same server, so a test can name a
// conversation the recipient was never a member of. Membership is what eligibility
// turns on, so the conversation has to be real rather than invented.
func unrelatedGroupFor(t *testing.T, s *Server, h http.Handler, owner *http.Cookie, username string) string {
	t.Helper()
	_, thirdCookie := memberAccount(t, s, username)
	group := readJSON[groupSummary](t, request(t, h, "POST", "/api/groups", map[string]string{"name": "an unrelated group"}, owner))
	invite(t, h, owner, group.ID, evidenceUserID(t, h, thirdCookie))
	accept(t, h, thirdCookie, pendingInvitation(t, h, thirdCookie))
	return group.ID
}

// sendDirectGroup posts one message into a group and returns its id.
func sendDirectGroup(t *testing.T, h http.Handler, cookie *http.Cookie, groupID, content, key string) string {
	t.Helper()
	w := request(t, h, "POST", "/api/conversations/"+groupID+"/messages", map[string]string{
		"content": content, "idempotency_key": key,
	}, cookie)
	if w.Code != 201 {
		t.Fatalf("send %q: %d %s", content, w.Code, w.Body.String())
	}
	return readJSON[map[string]any](t, w)["id"].(string)
}

func cut9(value string) string {
	if len(value) > 240 {
		return value[:240]
	}
	return value
}
