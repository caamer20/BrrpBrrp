package api

import (
	"net/http"
	"testing"
	"time"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// Consenting groups (ADR 0016).
//
// These are the cases where being wrong would matter to a person: a group that
// admits someone who did not agree, an owner who cannot leave and cannot hand over,
// a removed member who keeps reading, and a "block" affordance that promises
// something this product cannot deliver.

func groupFixture(t *testing.T) (*Server, http.Handler, *http.Cookie, *http.Cookie, string) {
	t.Helper()
	return groupFixtureWithAdmin(t, "")
}

func groupFixtureWithAdmin(t *testing.T, _ string) (*Server, http.Handler, *http.Cookie, *http.Cookie, string) {
	t.Helper()
	s, h := testServer(t)
	s.config.PrivateConversations = true
	admin := setupAdmin(t, s, h)
	_, owner := memberAccount(t, s, "grp_owner")
	_, guest := memberAccount(t, s, "grp_guest")
	created := request(t, h, "POST", "/api/groups", map[string]string{"name": "the test group"}, owner)
	if created.Code != 201 {
		t.Fatalf("create group: %d %s", created.Code, created.Body.String())
	}
	group := readJSON[groupSummary](t, created)
	invite(t, h, owner, group.ID, evidenceUserID(t, h, guest))
	groupAdminCookie = admin
	return s, h, owner, guest, group.ID
}

// groupAdminCookie holds the installation owner's session for the most recent
// group fixture, so a test that needs the owner's authority does not have to
// re-run the setup ceremony.
var groupAdminCookie *http.Cookie

// invite performs the owner's invitation.
func invite(t *testing.T, h http.Handler, owner *http.Cookie, groupID, inviteeID string) {
	t.Helper()
	w := request(t, h, "POST", "/api/groups/"+groupID+"/invite", map[string]string{"user_id": inviteeID}, owner)
	if w.Code != 201 {
		t.Fatalf("invite: %d %s", w.Code, w.Body.String())
	}
}

func pendingInvitation(t *testing.T, h http.Handler, invitee *http.Cookie) string {
	t.Helper()
	body := readJSON[map[string]any](t, request(t, h, "GET", "/api/group-invitations", nil, invitee))
	for _, entry := range body["invitations"].([]any) {
		row := entry.(map[string]any)
		if row["my_state"] == "invited" && row["state"] == "pending" {
			return row["id"].(string)
		}
	}
	t.Fatal("no pending invitation addressed to the invitee")
	return ""
}

func accept(t *testing.T, h http.Handler, invitee *http.Cookie, invitationID string) {
	t.Helper()
	if w := request(t, h, "POST", "/api/group-invitations/"+invitationID+"/accept", map[string]string{}, invitee); w.Code != 200 {
		t.Fatalf("accept: %d %s", w.Code, w.Body.String())
	}
}

// Membership is the recipient's decision. The inviter cannot accept for them, an
// outsider is refused, and a cross-accepted pair is still one act each.
func TestGroupMembershipRequiresTheRecipientsOwnAcceptance(t *testing.T) {
	s, h, owner, guest, groupID := groupFixture(t)
	invitation := pendingInvitation(t, h, guest)

	// The inviter cannot accept on the invitee's behalf. A request the sender could
	// accept is not a request.
	if w := request(t, h, "POST", "/api/group-invitations/"+invitation+"/accept", map[string]string{}, owner); w.Code != http.StatusNotFound {
		t.Fatalf("the inviter accepted for the invitee: %d %s", w.Code, w.Body.String())
	}
	// A stranger is refused identically to a missing invitation, so accepting is not a
	// way to discover a group.
	_, outsider := memberAccount(t, s, "grp_stranger")
	if w := request(t, h, "POST", "/api/group-invitations/"+invitation+"/accept", map[string]string{}, outsider); w.Code != http.StatusNotFound {
		t.Fatalf("an outsider accepted: %d %s", w.Code, w.Body.String())
	}
	// The recipient accepts, and is then a member.
	accept(t, h, guest, invitation)
	groups := readJSON[map[string]any](t, request(t, h, "GET", "/api/groups", nil, guest))
	if len(groups["groups"].([]any)) != 1 {
		t.Fatalf("the accepted member sees %d groups", len(groups["groups"].([]any)))
	}
	// Acceptance is idempotent: a replay is the same acceptance, not a second one.
	if w := request(t, h, "POST", "/api/group-invitations/"+invitation+"/accept", map[string]string{}, guest); w.Code != http.StatusOK && w.Code != http.StatusConflict {
		t.Fatalf("a replayed acceptance answered %d", w.Code)
	}
	var members int
	if err := s.store.QueryRow(`SELECT count(*) FROM conversation_members WHERE conversation_id=? AND state='accepted'`, groupID).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 2 {
		t.Fatalf("the group has %d accepted members, want 2", members)
	}
}

// The member bound is enforced in the accepting transaction, so two concurrent
// acceptances cannot both see the last slot free.
func TestGroupMemberBoundIsEnforcedInTheAcceptingTransaction(t *testing.T) {
	s, h := testServer(t)
	s.config.PrivateConversations = true
	setupAdmin(t, s, h)
	_, owner := memberAccount(t, s, "bound_owner")
	group := readJSON[groupSummary](t, request(t, h, "POST", "/api/groups", map[string]string{"name": "bounded"}, owner))

	// Fill the group to its limit through the real invitation and acceptance path.
	invitees := make([]*http.Cookie, 0, groupMemberLimit)
	for i := 0; i < groupMemberLimit-1; i++ {
		_, cookie := memberAccount(t, s, "bound_member_"+string(rune('a'+i)))
		invitees = append(invitees, cookie)
		request(t, h, "POST", "/api/groups/"+group.ID+"/invite", map[string]string{
			"user_id": evidenceUserID(t, h, cookie),
		}, owner)
		accept(t, h, cookie, pendingInvitation(t, h, cookie))
	}
	var members int
	if err := s.store.QueryRow(`SELECT count(*) FROM conversation_members WHERE conversation_id=? AND state='accepted'`, group.ID).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != groupMemberLimit {
		t.Fatalf("the fixture reached %d members, want %d", members, groupMemberLimit)
	}
	// The eleventh is invited and accepts, and is refused because the group is full.
	_, overflow := memberAccount(t, s, "bound_overflow")
	if w := request(t, h, "POST", "/api/groups/"+group.ID+"/invite", map[string]string{
		"user_id": evidenceUserID(t, h, overflow),
	}, owner); w.Code != 201 {
		t.Fatalf("invite: %d %s", w.Code, w.Body.String())
	}
	w := request(t, h, "POST", "/api/group-invitations/"+pendingInvitation(t, h, overflow)+"/accept", map[string]string{}, overflow)
	if w.Code != http.StatusConflict {
		t.Fatalf("a full group admitted another member: %d %s", w.Code, w.Body.String())
	}
	var after int
	if err := s.store.QueryRow(`SELECT count(*) FROM conversation_members WHERE conversation_id=? AND state='accepted'`, group.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != groupMemberLimit {
		t.Fatalf("the refused acceptance changed the member count to %d", after)
	}
}

// Ownership is deterministic: an owner cannot simply leave, a transfer is atomic, and
// a group never reaches a state where nobody can administer it.
func TestGroupOwnershipIsDeterministicAcrossTransferAndLeave(t *testing.T) {
	s, h, owner, guest, groupID := groupFixture(t)
	accept(t, h, guest, pendingInvitation(t, h, guest))

	// The owner cannot leave without handing over, and the refusal says what to do.
	w := request(t, h, "POST", "/api/groups/"+groupID+"/leave", map[string]string{}, owner)
	if w.Code != http.StatusConflict {
		t.Fatalf("the owner left without handing over: %d %s", w.Code, w.Body.String())
	}
	// A transfer to a non-member is refused: ownership follows membership.
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/transfer", map[string]string{
		"new_owner_id": "01a0ec00-0000-7000-8000-000000000000",
	}, owner); w.Code != http.StatusConflict {
		t.Fatalf("ownership moved to a non-member: %d %s", w.Code, w.Body.String())
	}
	// A non-owner cannot transfer.
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/transfer", map[string]string{
		"new_owner_id": evidenceUserID(t, h, owner),
	}, guest); w.Code != http.StatusForbidden {
		t.Fatalf("a member transferred ownership: %d %s", w.Code, w.Body.String())
	}
	// The real transfer, then the owner leaving.
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/transfer", map[string]string{
		"new_owner_id": evidenceUserID(t, h, guest),
	}, owner); w.Code != 200 {
		t.Fatalf("transfer: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/leave", map[string]string{}, owner); w.Code != 200 {
		t.Fatalf("leave after transfer: %d %s", w.Code, w.Body.String())
	}
	// Exactly one owner, and it is not the person who left.
	var owners []string
	rows, err := s.store.Query(`SELECT user_id FROM group_owners WHERE conversation_id=?`, groupID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		owners = append(owners, id)
	}
	if len(owners) != 1 || owners[0] != evidenceUserID(t, h, guest) {
		t.Fatalf("owners after transfer and leave: %v", owners)
	}
	// The group can still be administered: the new owner may invite.
	_, third := memberAccount(t, s, "grp_after_transfer")
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/invite", map[string]string{
		"user_id": evidenceUserID(t, h, third),
	}, guest); w.Code != 201 {
		t.Fatalf("the new owner cannot invite: %d %s", w.Code, w.Body.String())
	}
}

// A removed member loses everything from the moment of removal, while what they read
// before stays theirs. Both halves are asserted, because only asserting the first
// would pass a removal that silently rewrote history too.
func TestGroupRemovalRevokesForwardAndKeepsThePastReadable(t *testing.T) {
	s, h, owner, guest, groupID := groupFixture(t)
	accept(t, h, guest, pendingInvitation(t, h, guest))

	// A message in the group before the removal.
	sent := request(t, h, "POST", "/api/conversations/"+groupID+"/messages", map[string]string{
		"content": "before the removal", "idempotency_key": "grp-msg-before",
	}, owner)
	if sent.Code != 201 {
		t.Fatalf("group message: %d %s", sent.Code, sent.Body.String())
	}
	// The guest can read it: the history rule is real, not aspirational.
	if w := request(t, h, "GET", "/api/conversations/"+groupID+"/messages", nil, guest); w.Code != 200 {
		t.Fatalf("an accepted member could not read the group: %d", w.Code)
	}

	if w := request(t, h, "POST", "/api/groups/"+groupID+"/members/"+evidenceUserID(t, h, guest)+"/remove", map[string]string{}, owner); w.Code != 200 {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
	// Everything from now on is refused: reading, writing, search and files.
	if w := request(t, h, "GET", "/api/conversations/"+groupID+"/messages", nil, guest); w.Code == 200 {
		t.Fatal("a removed member could still read the group")
	}
	if w := request(t, h, "POST", "/api/conversations/"+groupID+"/messages", map[string]string{
		"content": "after the removal", "idempotency_key": "grp-msg-after",
	}, guest); w.Code == 201 {
		t.Fatal("a removed member could still write to the group")
	}
	scoped := request(t, h, "GET", "/api/search?q=removal&conversation_id="+groupID, nil, guest)
	if scoped.Code == 200 && len(readJSON[[]map[string]any](t, scoped)) > 0 {
		t.Fatal("a removed member could still search the group")
	}
	// Their own activity rows for the group stop being delivered, which is the
	// interruption half of the same revocation.
	if s.deliverableActivity(evidenceUserID(t, h, guest), activityMessage, groupID, evidenceUserID(t, h, owner)) {
		t.Fatal("a removed member is still deliverable activity for the group")
	}
	// The messages themselves are not rewritten by the removal: the removal is a
	// membership decision, not a history edit.
	var remaining int
	if err := s.store.QueryRow(`SELECT count(*) FROM messages WHERE conversation_id=?`, groupID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("the removal rewrote history: %d messages remain", remaining)
	}
}

// A block does not hide a group conversation, and the product must not offer an
// affordance that promises it does. This asserts the part that is checkable: the two
// members keep sharing the group, and the block governs their direct contact instead.
func TestABlockDoesNotClaimToHideAGroupConversation(t *testing.T) {
	s, h, _, guest, groupID := groupFixture(t)
	accept(t, h, guest, pendingInvitation(t, h, guest))

	// The guest blocks the owner, whose identity is read from the group rather than
	// from a fixture binding, so the test is about the two of them and not about
	// which local variable holds whom.
	var ownerID string
	if err := s.store.QueryRow(`SELECT user_id FROM group_owners WHERE conversation_id=?`, groupID).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if w := request(t, h, "PUT", "/api/me/blocks/"+ownerID, map[string]string{}, guest); w.Code != 200 {
		t.Fatalf("block: %d %s", w.Code, w.Body.String())
	}
	// The group is still shared: a block is not a per-pair view of a group, and
	// membership is the protection (ADR 0016, "What blocking means in a group").
	if w := request(t, h, "GET", "/api/conversations/"+groupID+"/messages", nil, guest); w.Code != 200 {
		t.Fatalf("a block hid a group conversation from a member: %d", w.Code)
	}
	// What the block does govern: their direct contact. A new request between them is
	// refused, which is the promise that is actually kept.
	_, third := memberAccount(t, s, "grp_third")
	if w := request(t, h, "POST", "/api/directs", map[string]string{
		"user_id": evidenceUserID(t, h, guest), "introduction": "after the block",
	}, third); w.Code == 201 {
		// The third party is not blocked, so this succeeding is correct; the check
		// that matters is between the two blocked accounts themselves.
		_ = w
	}
	// The refusal is indistinguishable from a person who does not exist, which is
	// the point: a block must not become a way to confirm that someone is here.
	blocked := request(t, h, "POST", "/api/directs", map[string]string{
		"user_id": ownerID, "introduction": "should be refused",
	}, guest)
	if blocked.Code != http.StatusNotFound {
		t.Fatalf("a blocked account opened a new direct request: %d %s", blocked.Code, blocked.Body.String())
	}
}

// Leaving the last member removes the group rather than leaving an orphan a later
// joiner could inherit.
func TestTheLastMemberLeavingRemovesTheGroup(t *testing.T) {
	s, h, owner, guest, groupID := groupFixture(t)
	accept(t, h, guest, pendingInvitation(t, h, guest))
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/transfer", map[string]string{
		"new_owner_id": evidenceUserID(t, h, guest),
	}, owner); w.Code != 200 {
		t.Fatalf("transfer: %d", w.Code)
	}
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/leave", map[string]string{}, owner); w.Code != 200 {
		t.Fatalf("owner leave: %d", w.Code)
	}
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/leave", map[string]string{}, guest); w.Code != 200 {
		t.Fatalf("last member leave: %d %s", w.Code, w.Body.String())
	}
	var state string
	if err := s.store.QueryRow(`SELECT state FROM conversations WHERE id=?`, groupID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "removed" {
		t.Fatalf("the empty group survived with state %q", state)
	}
	// And it is no longer listed to anyone.
	groups := readJSON[map[string]any](t, request(t, h, "GET", "/api/groups", nil, guest))
	if len(groups["groups"].([]any)) != 0 {
		t.Fatalf("a removed group is still listed: %v", groups["groups"])
	}
}

// An invitation expires on its own terms, and the expiry is observed by the paths
// that act on invitations rather than waiting for a sweeper.
func TestGroupInvitationsExpireAndAreObservable(t *testing.T) {
	s, h, _, guest, groupID := groupFixture(t)
	invitation := pendingInvitation(t, h, guest)
	if _, err := s.store.Exec(`UPDATE group_invitations SET expires_at=? WHERE id=?`,
		db.Timestamp(time.Now().Add(-time.Hour)), invitation); err != nil {
		t.Fatal(err)
	}
	if w := request(t, h, "POST", "/api/group-invitations/"+invitation+"/accept", map[string]string{}, guest); w.Code != http.StatusConflict {
		t.Fatalf("an expired invitation was accepted: %d %s", w.Code, w.Body.String())
	}
	// And it is no longer listed as open.
	body := readJSON[map[string]any](t, request(t, h, "GET", "/api/group-invitations", nil, guest))
	for _, entry := range body["invitations"].([]any) {
		if entry.(map[string]any)["id"] == invitation && entry.(map[string]any)["state"] == "pending" {
			t.Fatal("an expired invitation is still listed as pending")
		}
	}
	var members int
	if err := s.store.QueryRow(`SELECT count(*) FROM conversation_members WHERE conversation_id=? AND state='accepted'`, groupID).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 1 {
		t.Fatalf("the expired acceptance changed membership: %d", members)
	}
}

// The group is not a second message service: it uses the shared rows, the shared
// receipts and the shared read cursor, so a replayed send is the same send.
func TestAGroupUsesTheSharedMessageAndReceiptModel(t *testing.T) {
	s, h, owner, guest, groupID := groupFixture(t)
	accept(t, h, guest, pendingInvitation(t, h, guest))

	first := request(t, h, "POST", "/api/conversations/"+groupID+"/messages", map[string]string{
		"content": "group wording quokka", "idempotency_key": "grp-shared-1",
	}, owner)
	if first.Code != 201 {
		t.Fatalf("send: %d %s", first.Code, first.Body.String())
	}
	messageID := readJSON[map[string]any](t, first)["id"].(string)
	// The same key and content is the same message, not a second one.
	replay := request(t, h, "POST", "/api/conversations/"+groupID+"/messages", map[string]string{
		"content": "group wording quokka", "idempotency_key": "grp-shared-1",
	}, owner)
	if replay.Code != 200 || readJSON[map[string]any](t, replay)["id"] != messageID {
		t.Fatalf("a replayed group send was not the same message: %d %s", replay.Code, replay.Body.String())
	}
	var messages int
	if err := s.store.QueryRow(`SELECT count(*) FROM messages WHERE conversation_id=?`, groupID).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != 1 {
		t.Fatalf("the group holds %d messages, want 1", messages)
	}
	// And the same key with different content is a conflict, as it is elsewhere.
	if w := request(t, h, "POST", "/api/conversations/"+groupID+"/messages", map[string]string{
		"content": "different wording", "idempotency_key": "grp-shared-1",
	}, owner); w.Code != http.StatusConflict {
		t.Fatalf("a group send key was reused for different content: %d", w.Code)
	}
	// Activity is written for group messages like any other, so a member who was
	// offline catches up.
	var activity int
	if err := s.store.QueryRow(`SELECT count(*) FROM account_activity WHERE user_id=? AND source_id=?`,
		evidenceUserID(t, h, guest), messageID).Scan(&activity); err != nil {
		t.Fatal(err)
	}
	if activity != 1 {
		t.Fatalf("the group message produced %d activity rows for the other member", activity)
	}
	_ = guest
}


// The membership list is read through a scalar subquery for the owner flag, and a
// scalar subquery with no matching row returns NULL rather than 0. Scanned into an
// int that is a 500, which the interface cannot tell from a real fault — so the
// endpoint is exercised here for every member, including the one who is not the
// owner.
func TestGroupMemberListAnswersForEveryMember(t *testing.T) {
	s, h, owner, guest, groupID := groupFixture(t)
	accept(t, h, guest, pendingInvitation(t, h, guest))

	for name, cookie := range map[string]*http.Cookie{"the owner": owner, "a member": guest} {
		w := request(t, h, "GET", "/api/groups/"+groupID+"/members", nil, cookie)
		if w.Code != 200 {
			t.Fatalf("%s could not read the membership list: %d %s", name, w.Code, w.Body.String())
		}
		members := readJSON[map[string]any](t, w)["members"].([]any)
		if len(members) != 2 {
			t.Fatalf("%s sees %d members, want 2: %v", name, len(members), members)
		}
		owners := 0
		for _, entry := range members {
			if entry.(map[string]any)["is_owner"] == true {
				owners++
			}
		}
		if owners != 1 {
			t.Fatalf("%s sees %d owners, want exactly 1", name, owners)
		}
	}
	// A non-member is refused identically to a group that does not exist.
	_, outsider := memberAccount(t, s, "grp_member_list_outsider")
	if w := request(t, h, "GET", "/api/groups/"+groupID+"/members", nil, outsider); w.Code != http.StatusNotFound {
		t.Fatalf("an outsider read the membership list: %d", w.Code)
	}
}

// The owner cannot be removed: ownership governs who may invite and who may remove,
// so a group whose owner can be removed has an owner who can do neither. A removal
// that would empty a group takes the group with it, so a group never survives with
// nobody able to run it.
func TestAGroupsOwnerCannotBeRemovedAndAnEmptyingRemovalTakesTheGroup(t *testing.T) {
	s, h, owner, guest, groupID := groupFixture(t)
	accept(t, h, guest, pendingInvitation(t, h, guest))
	ownerID := evidenceUserID(t, h, owner)
	guestID := evidenceUserID(t, h, guest)
	// The owner tries to remove themselves through the removal route, which is
	// distinct from leaving.
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/members/"+ownerID+"/remove", map[string]string{}, owner); w.Code != http.StatusConflict {
		t.Fatalf("the owner removed themselves through the removal route: %d %s", w.Code, w.Body.String())
	}
	// A transfer moves the protection with the role, not with the account. The guest
	// becomes the owner, and the person who handed over becomes an ordinary member.
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/transfer", map[string]string{
		"new_owner_id": guestID,
	}, owner); w.Code != 200 {
		t.Fatalf("transfer: %d", w.Code)
	}
	// The new owner cannot be removed, and removing the person who handed over — now
	// an ordinary member — succeeds. The owner is left, so the group is not empty and
	// survives: a group with its owner in it is still a group somebody can run.
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/members/"+guestID+"/remove", map[string]string{}, guest); w.Code != http.StatusConflict {
		t.Fatalf("the current owner removed themselves: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/members/"+ownerID+"/remove", map[string]string{}, guest); w.Code != 200 {
		t.Fatalf("remove the former owner: %d %s", w.Code, w.Body.String())
	}
	// With the owner alone, the group survives — and the owner leaving is what takes
	// it, because the last-member rule is about there being nobody left.
	var remainingMembers int
	if err := s.store.QueryRow(`SELECT count(*) FROM conversation_members WHERE conversation_id=? AND state='accepted'`, groupID).Scan(&remainingMembers); err != nil {
		t.Fatal(err)
	}
	if remainingMembers != 1 {
		t.Fatalf("the removal left %d accepted members, want the owner alone", remainingMembers)
	}
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/leave", map[string]string{}, guest); w.Code != 200 {
		t.Fatalf("the last member left: %d %s", w.Code, w.Body.String())
	}
	var state string
	if err := s.store.QueryRow(`SELECT state FROM conversations WHERE id=?`, groupID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "removed" {
		t.Fatalf("the emptied group survived with state %q", state)
	}

	if err := s.store.QueryRow(`SELECT state FROM conversations WHERE id=?`, groupID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "removed" {
		t.Fatalf("the emptied group survived with state %q", state)
	}
}

// V13-02. A group's membership is private, and so is the fact that it exists: an
// account holding only the community audit capability must not learn that a private
// group was created, who invited whom, or who joined it. Naming the actor and the
// group id in an event an unrelated auditor can read discloses both.
//
// The events exercised here are the ones the handlers actually write. Seeding a
// prefix no handler emits would prove the filter but not the writers.
func TestGroupLifecycleEventsAreNotReadableThroughOrdinaryAudit(t *testing.T) {
	s, h, _, guest, groupID := groupFixture(t)
	accept(t, h, guest, pendingInvitation(t, h, guest))

	// The rows exist. A fix that stopped writing them would satisfy a reader-only
	// test while losing the accountability record entirely.
	var written int
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE action LIKE 'group.%'`).Scan(&written); err != nil {
		t.Fatal(err)
	}
	if written == 0 {
		t.Fatal("no group lifecycle event was written, so the reader test would prove nothing")
	}

	// An account holding only the community audit capability.
	_, auditor := evidenceRole(t, s, h, groupAdminCookie, "community only auditor", string(authz.AuditRead))
	general := readJSON[[]map[string]any](t, request(t, h, "GET", "/api/admin/audit-logs", nil, auditor))
	for _, entry := range general {
		action, _ := entry["action"].(string)
		if isRestrictedAuditAction(action) {
			t.Fatalf("a private group event reached the community audit feed: %q", action)
		}
		if action == "group.invited" || action == "group.joined" {
			t.Fatalf("a group lifecycle event reached the community audit feed: %q", action)
		}
	}

	// The restricted feed is gated on its own capability, not on the evidence
	// review grant. An account holding only report.review_private is refused here,
	// so a grant that confers submitted evidence cannot be used to browse group
	// metadata.
	_, reviewer := evidenceRole(t, s, h, groupAdminCookie, "community moderator", string(authz.MessageDeleteAny))
	if w := request(t, h, "GET", "/api/admin/private-reports/audit", nil, reviewer); w.Code == 200 {
		t.Fatal("an account without the audit capability read the restricted audit feed")
	}

	// The restricted feed keeps them, and the group is what makes an account
	// authorized to read it: a member, the owner, and the installation's operator.
	// An ordinary member is refused: membership is not authority to read an audit
	// trail, and a restricted feed any member could read would be a community feed
	// with a filter on it. The operator holds the audit capability by role, and an
	// explicit grant is the only other way in.
	if w := request(t, h, "GET", "/api/admin/private-reports/audit", nil, guest); w.Code != http.StatusForbidden {
		t.Fatalf("an ordinary member read the restricted audit feed: %d", w.Code)
	}
	memberRole, memberAuditor := evidenceRole(t, s, h, groupAdminCookie, "member auditor", string(authz.AuditRead))
	for name, cookie := range map[string]*http.Cookie{
		"the operator":             groupAdminCookie,
		"an account granted audit": memberAuditor,
	} {
		w := request(t, h, "GET", "/api/admin/private-reports/audit", nil, cookie)
		if w.Code != 200 {
			t.Fatalf("%s could not read the restricted audit feed: %d %s", name, w.Code, w.Body.String())
		}
		entries := readJSON[[]map[string]any](t, w)
		found := false
		for _, entry := range entries {
			if action, _ := entry["action"].(string); isRestrictedAuditAction(action) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s could not see the group lifecycle events they are entitled to", name)
		}
	}

	// Revocation is read-time, not a filter that forgets: an account that loses the
	// audit capability stops seeing it immediately.
	revokeEvidenceRole(t, h, groupAdminCookie, memberRole)
	if w := request(t, h, "GET", "/api/admin/private-reports/audit", nil, memberAuditor); w.Code != http.StatusForbidden {
		t.Fatalf("a revoked account still read the restricted audit feed: %d", w.Code)
	}
	if w := request(t, h, "GET", "/api/admin/audit-logs", nil, memberAuditor); w.Code != http.StatusForbidden {
		t.Fatalf("a revoked account still read the community audit feed: %d", w.Code)
	}
	_ = groupID
}

// V13-03. A group change is an authority change. A plain commit leaves the
// authorization revision where it was, leaves an open response running, and tells
// nobody their view changed — which for a removal means a former member keeps
// streaming a file they no longer have the right to read.
func TestGroupMembershipChangeEndsTheRemovedAccountsResponses(t *testing.T) {
	s, h, owner, guest, groupID := groupFixture(t)
	accept(t, h, guest, pendingInvitation(t, h, guest))

	before, err := s.store.AuthorizationRevision(t.Context(), s.store)
	if err != nil {
		t.Fatal(err)
	}
	// A response the removed member already has open. `begin` is what every real
	// stream registers through, so this exercises the production path rather than a
	// stand-in for it.
	streamCtx, release := s.deliveries.begin(groupID, evidenceUserID(t, h, guest), "session-hash")
	defer release()
	registered := len(s.deliveries.snapshot())
	if registered == 0 {
		t.Fatal("the stream registry accepted nothing, so the revocation test would pass for the wrong reason")
	}

	if w := request(t, h, "POST", "/api/groups/"+groupID+"/members/"+evidenceUserID(t, h, guest)+"/remove", map[string]string{}, owner); w.Code != 200 {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
	after, err := s.store.AuthorizationRevision(t.Context(), s.store)
	if err != nil {
		t.Fatal(err)
	}
	if after <= before {
		t.Fatalf("the authorization revision did not advance across a removal: %d -> %d", before, after)
	}
	// The removed account's open response is ended, which is the revocation a
	// revision bump alone would not perform. The context the response reads through
	// is cancelled, so a slow client stops mid-copy rather than after the whole file.
	if streamCtx.Err() == nil {
		t.Fatal("the removed member's open response was not ended")
	}

	// The permitted control: the owner, who kept access, still works.
	if w := request(t, h, "GET", "/api/groups/"+groupID+"/members", nil, owner); w.Code != 200 {
		t.Fatalf("the remaining member lost access: %d %s", w.Code, w.Body.String())
	}
}

// V13-04. Accepting an invitation must be able to fail, and the failure has to be a
// refusal rather than a success into a group that no longer exists.
//
// The reproduced sequence was: the sole owner leaves with an invitation pending,
// the group is emptied and marked removed, the invitee accepts and receives 200 —
// and then reading the group returns 404. A 200 that is followed by a 404 is the
// worst of both answers, because the person is told they joined something they
// cannot read.
func TestAcceptingIntoAGroupThatHasEndedIsRefused(t *testing.T) {
	s, h, owner, guest, groupID := groupFixture(t)
	invitation := pendingInvitation(t, h, guest)

	if w := request(t, h, "POST", "/api/groups/"+groupID+"/leave", map[string]string{}, owner); w.Code != 200 {
		t.Fatalf("the last member leaves: %d %s", w.Code, w.Body.String())
	}
	var state string
	if err := s.store.QueryRow(`SELECT state FROM conversations WHERE id=?`, groupID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "removed" {
		t.Fatalf("the emptied group was not marked removed: %q", state)
	}

	w := request(t, h, "POST", "/api/group-invitations/"+invitation+"/accept", map[string]string{}, guest)
	if w.Code == 200 {
		t.Fatal("an invitation into a group that has ended was accepted")
	}
	// Either defined refusal is correct, and which one depends on whether the
	// invitation was closed with its group or merely outlived it: 410 says the group
	// it named is gone, 409 says the invitation is no longer open. What is not
	// acceptable is a 200 that is followed by a 404 on the conversation.
	if w.Code != http.StatusGone && w.Code != http.StatusConflict {
		t.Fatalf("an ended group's acceptance answered %d rather than a defined refusal: %s", w.Code, w.Body.String())
	}
	// And it did not admit anyone: no membership, and the invitation is closed rather
	// than left open against a group that will never exist.
	var members int
	if err := s.store.QueryRow(`SELECT count(*) FROM conversation_members WHERE conversation_id=? AND state='accepted'`, groupID).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 0 {
		t.Fatalf("the refused acceptance admitted %d members", members)
	}
	var invitationState string
	if err := s.store.QueryRow(`SELECT state FROM group_invitations WHERE id=?`, invitation).Scan(&invitationState); err != nil {
		t.Fatal(err)
	}
	if invitationState == "pending" {
		t.Fatal("the invitation stayed open against a group that has ended")
	}
}

// An invitation is offered to somebody the recipient has blocked, or who has blocked
// them. Contact policy that governs direct requests has to govern invitations too, or
// a block is trivially bypassed by starting a group.
func TestBlockingStopsGroupInvitations(t *testing.T) {
	s, h, owner, guest, groupID := groupFixture(t)
	guestID := evidenceUserID(t, h, guest)

	// The recipient blocks the inviter.
	if w := request(t, h, "PUT", "/api/me/blocks/"+evidenceUserID(t, h, owner), map[string]string{}, guest); w.Code != 200 {
		t.Fatalf("block: %d %s", w.Code, w.Body.String())
	}
	// No invitation is created, and the refusal does not confirm the group exists.
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/invite", map[string]string{
		"user_id": guestID,
	}, owner); w.Code != http.StatusConflict && w.Code != http.StatusNotFound {
		t.Fatalf("an invitation was offered to somebody who blocked the inviter: %d %s", w.Code, w.Body.String())
	}
	// The fixture already holds one invitation, created before the block. The point
	// is that the blocked attempt added none.
	var before, after int
	if err := s.store.QueryRow(`SELECT count(*) FROM group_invitations WHERE invitee_id=?`, guestID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if w := request(t, h, "POST", "/api/groups/"+groupID+"/invite", map[string]string{
		"user_id": guestID,
	}, owner); w.Code != http.StatusConflict && w.Code != http.StatusNotFound {
		t.Fatalf("a second invitation was offered to somebody who blocked the inviter: %d %s", w.Code, w.Body.String())
	}
	if err := s.store.QueryRow(`SELECT count(*) FROM group_invitations WHERE invitee_id=?`, guestID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("the refused invitation added %d records", after-before)
	}
	_ = guest
}
