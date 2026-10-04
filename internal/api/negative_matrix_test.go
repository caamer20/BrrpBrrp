package api

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The negative matrix (plan §11.2, T-AUTH-01; roadmap phase C).
//
// T-AUTH-01 asks one question of every surface: an authenticated account that holds valid
// identifiers — a channel id, a conversation id, a message id, an upload id — and no
// relationship to them must get nothing. Not the content, not the metadata.
//
// Answering that by hand once is a review. Answering it *structurally* is what this file
// does: the route inventory already forces every endpoint to declare its policy
// (`TestEveryRouteDeclaresItsPolicy`), so the same inventory can force every endpoint to say
// *how an outsider is denied* — and a route added without that row fails the build rather
// than waiting for the next review to notice.
//
// The rows are built from two sources:
//
//   - the policy the route declares (a capability, the timeout scope, whether it resolves a
//     conversation itself), which is mechanical and cannot drift;
//   - a hand-written classification for the routes that carry no capability, because for
//     those the refusal is the handler's own scoping rather than the middleware's — a
//     response limited to the caller's own account, a conversation resolved before anything
//     is read, authorship, or an audience computed per delivery.
//
// `-update-negative-matrix` regenerates `planning/negative-matrix.json`; the checked-in file
// is what a reviewer reads and what CI compares against, so a change to a route's policy
// shows up as a diff rather than as silence.
var updateNegativeMatrix = flag.Bool("update-negative-matrix", false, "regenerate planning/negative-matrix.json")

// The closed vocabulary of refusals. Every route must name one, and the reason must say
// which mechanism does the work.
const (
	// deniedByCapability: the middleware refuses an account without the grant.
	deniedByCapability = "capability"
	// deniedBySelf: the response is the caller's own account state, and nothing else.
	deniedBySelf = "self"
	// deniedByConversation: the handler resolves the conversation first, so an outsider is
	// not a participant and there is nothing to read.
	deniedByConversation = "conversation"
	// deniedByChannel: channel access (a role or an explicit grant) decides, per channel.
	deniedByChannel = "channel"
	// deniedByAuthorship: only the author of the row may act on it.
	deniedByAuthorship = "authorship"
	// deniedByAudience: the answer is computed per delivery — a filtered snapshot, a scoped
	// event stream — so an outsider's answer is empty rather than refused.
	deniedByAudience = "audience"
	// deniedByRecipient: only the named recipient of the row may act on it.
	deniedByRecipient = "recipient"
	// deniedByFeature: the route is refused entirely while its feature gate is closed.
	deniedByFeature = "feature-gate"
)

// handDenials classifies the routes whose declared policy carries no capability. For every
// other route the capability is the first half of the answer and this table is not needed;
// for these, the handler's own scoping is the whole answer, and the reason is the sentence a
// reviewer checks.
var handDenials = map[string]struct {
	Denial string
	Reason string
}{
	// The caller's own account and credentials. An outsider holds somebody else's id, which
	// every one of these ignores: they answer about the authenticated account only.
	"GET /api/me":                    {deniedBySelf, "the response is the caller's own account record"},
	"GET /api/me/mfa":                {deniedBySelf, "the caller's own enrolment state"},
	"POST /api/me/mfa/setup":         {deniedBySelf, "acts on the caller's own factor"},
	"POST /api/me/mfa/enable":        {deniedBySelf, "acts on the caller's own factor"},
	"POST /api/me/mfa/disable":       {deniedBySelf, "acts on the caller's own factor"},
	"POST /api/me/password":          {deniedBySelf, "changes the caller's own credential after reauthentication"},
	"GET /api/me/sessions":           {deniedBySelf, "lists the caller's own sessions"},
	"DELETE /api/me/sessions":        {deniedBySelf, "revokes the caller's own sessions"},
	"DELETE /api/me/sessions/{id}":   {deniedBySelf, "the session id is matched against the caller's own rows"},
	"POST /api/me/reauth":            {deniedBySelf, "reauthenticates the caller's own account and changes nothing an outsider holds"},
	"POST /api/logout":               {deniedBySelf, "ends the caller's own session"},
	"GET /ws":                        {deniedByAudience, "every event is resolved against the account's own audience before delivery"},
	"GET /api/state":                 {deniedByAudience, "the snapshot is filtered to what the caller may read; an outsider's is empty"},
	"GET /api/unread":                {deniedByAudience, "counts are computed per account from what it may read"},
	"GET /api/channels":              {deniedByAudience, "only channels the caller's roles or grants admit are listed"},
	"GET /api/categories":            {deniedByAudience, "categories are listed with the channels they hold that the caller may read"},
	"GET /api/search":                {deniedByAudience, "the query is scoped to conversations the caller may read; an outsider finds nothing"},
	"GET /api/uploads/{id}":          {deniedByConversation, "the upload is served only to an account that may read the conversation it belongs to"},
	"GET /api/messages/{id}":         {deniedByConversation, "the message's conversation access is resolved before anything is returned"},
	"GET /api/messages/{id}/context": {deniedByConversation, "the surrounding window is read under the message's own conversation access"},
	"GET /api/messages/{id}/replies": {deniedByConversation, "the replies are read under the message's own conversation access"},
	"PATCH /api/messages/{id}":       {deniedByAuthorship, "only the author may edit, and the edit is re-authorized inside the transaction"},
	"DELETE /api/messages/{id}":      {deniedByAuthorship, "only the author (or a capability that says otherwise) may delete"},

	// Channels and threads. Every one of these resolves access first, so an outsider holding
	// a valid id is refused by the same rule that would refuse a member of another community.
	"GET /api/channels/{id}/messages":      {deniedByChannel, "channel access is resolved before the history is read"},
	"POST /api/channels/{id}/read":         {deniedByChannel, "the cursor is written only for a readable channel"},
	"GET /api/channels/{id}/first-unread":  {deniedByChannel, "the marker is computed from a readable channel"},
	"GET /api/channels/{id}/threads":       {deniedByChannel, "the thread list follows the channel's access"},
	"GET /api/channels/{id}/pins":          {deniedByChannel, "pins are channel content and follow channel access"},
	"GET /api/threads/{id}":                {deniedByChannel, "the thread resolves its channel access before returning anything (ADR 0017)"},
	"GET /api/threads/{id}/messages":       {deniedByChannel, "the channel's read rule applies to the thread's pages"},
	"POST /api/threads/{id}/read":          {deniedByChannel, "a thread cursor is written only for a readable thread"},
	"POST /api/threads/{id}/follow":        {deniedByChannel, "following requires the thread to be readable"},
	"DELETE /api/threads/{id}/follow":      {deniedBySelf, "unfollowing removes only the caller's own row, which an outsider does not have"},
	"PUT /api/channels/{id}/notifications": {deniedByChannel, "the preference is refused for a channel the caller cannot read"},

	// Private conversations. An outsider is not a participant, so the answer is the same
	// non-disclosing refusal a member of the community who is not in the conversation gets.
	"GET /api/conversations/{id}/messages":     {deniedByConversation, "participation is resolved before the history is read"},
	"GET /api/conversations/{id}/first-unread": {deniedByConversation, "the marker is computed from a readable conversation"},
	"POST /api/conversations/{id}/read":        {deniedByConversation, "the cursor is written only for a readable conversation"},
	"GET /api/conversations/{id}/pins":         {deniedByConversation, "pins are conversation content and follow its access"},

	// Requests, blocks and contacts: the caller's own state, or an act only the recipient may
	// take.
	"GET /api/directs":                 {deniedBySelf, "only the caller's own requests and conversations are listed"},
	"POST /api/directs":                {deniedBySelf, "the request is created between the caller and the named peer, and creates nothing without their acceptance"},
	"POST /api/directs/{id}/accept":    {deniedByRecipient, "only the named recipient may accept a request"},
	"POST /api/directs/{id}/reject":    {deniedByRecipient, "only the named recipient may reject a request"},
	"PUT /api/directs/{id}/visibility": {deniedBySelf, "the visibility preference belongs to the caller's own side of the conversation"},
	"GET /api/me/contact-preferences":  {deniedBySelf, "the caller's own contact policy"},
	"PUT /api/me/contact-preferences":  {deniedBySelf, "the caller's own contact policy"},
	"GET /api/me/blocks":               {deniedBySelf, "the caller's own block list"},
	"PUT /api/me/blocks/{id}":          {deniedBySelf, "the caller's own block list"},
	"DELETE /api/me/blocks/{id}":       {deniedBySelf, "the caller's own block list"},

	// Notifications and push: the caller's own devices and preferences.
	"POST /api/push/subscribe":             {deniedBySelf, "the registration belongs to the caller"},
	"GET /api/push/devices":                {deniedBySelf, "the caller's own registered devices and nobody else's"},
	"DELETE /api/push/subscribe":           {deniedBySelf, "the caller's own registration, matched against its own rows"},
	"GET /api/me/notification-preferences": {deniedBySelf, "the caller's own notification preferences"},
	"PUT /api/me/notification-preferences": {deniedBySelf, "the caller's own notification preferences"},
	"GET /api/me/channel-notifications":    {deniedBySelf, "the caller's own per-channel choices, filtered to channels it may read"},

	// Exports: the caller's own data, and the capability to ask is not enough — the request
	// is bound to the account that made it.
	"POST /api/me/export":        {deniedBySelf, "an export is created for the caller and for nobody else"},
	"GET /api/me/export":         {deniedBySelf, "only the caller's own exports are listed"},
	"GET /api/me/export/{id}":    {deniedBySelf, "the export id is matched against the caller's own rows"},
	"DELETE /api/me/export/{id}": {deniedBySelf, "only the owner of the export may remove it"},

	// Reporting: the reporter must be able to read what they report, and the receipt is their
	// own.
	"POST /api/messages/{id}/report":                {deniedByConversation, "the message's conversation access is resolved before a report is accepted"},
	"GET /api/messages/{id}/private-report-preview": {deniedByConversation, "the preview is the server's own snapshot, shown only to an account that may read it"},
	"POST /api/messages/{id}/private-report":        {deniedByConversation, "only a participant may submit private evidence, and only their own snapshot"},
	"GET /api/directs/{id}/report-preview":          {deniedByConversation, "the preview requires accepted read access to the conversation"},
	"POST /api/directs/{id}/report-and-block":       {deniedByRecipient, "only a participant may report and block, and the block acts on their own side"},
	"GET /api/me/report-receipt":                    {deniedBySelf, "the receipt belongs to the reporter"},
}

// matrixEntry is one route's row in the generated file.
type matrixEntry struct {
	Route           string   `json:"route"`
	Method          string   `json:"method"`
	Capability      string   `json:"capability,omitempty"`
	FreshAuth       bool     `json:"fresh_auth,omitempty"`
	BeyondCommunity bool     `json:"beyond_community,omitempty"`
	Mixed           bool     `json:"mixed_conversation,omitempty"`
	Denial          string   `json:"outsider_denied_by"`
	Reason          string   `json:"reason"`
	Evidence        []string `json:"evidence,omitempty"`
}

// evidenceNames are the tests that prove the refusal for the surfaces where a mechanism is
// not self-evident from the route's own policy. A name is `Package::TestName`; the test
// verifies each one exists where it says it does, so a renamed or deleted test fails this
// build rather than quietly leaving the row unsupported.
var evidenceNames = map[string][]string{
	"GET /api/state":                                {"api::TestPrivateChannelLifecycleReachesOnlyItsAudience", "api::TestRestrictedChannelPrivacyAcrossDeliverySurfaces"},
	"GET /api/channels/{id}/messages":               {"api::TestPrivateChannelLifecycleReachesOnlyItsAudience"},
	"GET /api/threads/{id}":                         {"api::TestRestrictedChannelPrivacyAcrossDeliverySurfaces"},
	"GET /api/conversations/{id}/messages":          {"api::TestDirectMessagePipelinePreservesScopeAndBlocksOperatorBypass"},
	"GET /api/messages/{id}":                        {"api::TestPrivateMessageEditDoesNotRevealExistenceToOutsiders", "api::TestReplyContextPinsAndDeletion"},
	"GET /api/uploads/{id}":                         {"api::TestMemberExportContainsTheirOwnConversationsAndNothingElse"},
	"GET /ws":                                       {"api::TestRealtimeEventsAreAudienceScoped", "api::TestTheRevokedAudienceIsNotDeliveredOnARealSocket"},
	"GET /api/me/export":                            {"api::TestMemberExportIsOnlyTheAccountThatRequestedIt", "api::TestExportDeliveryReauthorizesEveryRequestAndEveryByte"},
	"GET /api/search":                               {"api::TestRestrictedChannelPrivacyAcrossDeliverySurfaces"},
	"GET /api/directs":                              {"api::TestDirectConsentIsCanonicalExplicitAndNeverAdministrative"},
	"GET /api/unread":                               {"api::TestActivityIsPrivateToItsOwnerAndGatedByTheFeature"},
	"POST /api/messages/{id}/private-report":        {"api::TestPrivateEvidenceDeniesEveryoneWithoutAcceptedReadAccess"},
	"GET /api/messages/{id}/private-report-preview": {"api::TestPrivateEvidenceNeedsItsOwnCapabilityAndGrantsNoConversationAccess"},
}

// buildNegativeMatrix reads the live route inventory and produces the rows.
func buildNegativeMatrix(t *testing.T, routes []route) []matrixEntry {
	t.Helper()
	entries := make([]matrixEntry, 0, len(routes))
	for _, registered := range routes {
		key := registered.Method + " " + registered.Path
		entry := matrixEntry{
			Route:           registered.Path,
			Method:          registered.Method,
			Capability:      string(registered.Policy.Capability),
			FreshAuth:       registered.Policy.FreshAuth,
			BeyondCommunity: registered.Policy.BeyondCommunity,
			Mixed:           registered.Policy.MixedConversation,
			Evidence:        evidenceNames[key],
		}
		if entry.Capability != "" {
			entry.Denial = deniedByCapability
			entry.Reason = "the middleware refuses an account without " + entry.Capability + " before the handler runs"
		} else {
			hand, ok := handDenials[key]
			if !ok {
				t.Errorf("%s declares no capability and has no hand classification: add one to handDenials with the mechanism that denies an outsider", key)
				continue
			}
			entry.Denial = hand.Denial
			entry.Reason = hand.Reason
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(a, b int) bool {
		if entries[a].Route != entries[b].Route {
			return entries[a].Route < entries[b].Route
		}
		return entries[a].Method < entries[b].Method
	})
	return entries
}

var testFunction = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)

// evidenceExists reports whether `Package::TestName` names a real test function, so a row
// cannot point at a test that was renamed or deleted.
func evidenceExists(t *testing.T, reference string) bool {
	t.Helper()
	parts := strings.SplitN(reference, "::", 2)
	if len(parts) != 2 {
		t.Errorf("evidence %q must be written Package::TestName", reference)
		return false
	}
	dir := filepath.Join("..", parts[0])
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Errorf("evidence %q names no package directory: %v", reference, err)
		return false
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		source, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			t.Errorf("read %s: %v", file.Name(), err)
			return false
		}
		if testFunction.MatchString(string(source)) {
			for _, match := range testFunction.FindAllStringSubmatch(string(source), -1) {
				if match[1] == parts[1] {
					return true
				}
			}
		}
	}
	t.Errorf("evidence %q names no test function in %s", reference, dir)
	return false
}

const negativeMatrixPath = "../../planning/negative-matrix.json"

func TestEveryRouteDeclaresHowAnOutsiderIsDenied(t *testing.T) {
	s := &Server{}
	entries := buildNegativeMatrix(t, s.routes())

	// Every route the table names must be a route that exists, so a deleted endpoint does
	// not leave a row behind claiming somebody checked it.
	live := map[string]bool{}
	for _, registered := range s.routes() {
		live[registered.Method+" "+registered.Path] = true
	}
	for key := range handDenials {
		if !live[key] {
			t.Errorf("handDenials names %s, which the router does not serve: delete the row or restore the route", key)
		}
	}

	vocabulary := map[string]bool{
		deniedByCapability: true, deniedBySelf: true, deniedByConversation: true,
		deniedByChannel: true, deniedByAuthorship: true, deniedByAudience: true,
		deniedByRecipient: true, deniedByFeature: true,
	}
	for _, entry := range entries {
		if !vocabulary[entry.Denial] {
			t.Errorf("%s %s: %q is not one of the denial mechanisms", entry.Method, entry.Route, entry.Denial)
		}
		if len(strings.Fields(entry.Reason)) < 5 {
			t.Errorf("%s %s: the reason must say which mechanism denies an outsider, not just name it", entry.Method, entry.Route)
		}
		for _, reference := range entry.Evidence {
			evidenceExists(t, reference)
		}
	}

	generated, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	generated = append(generated, '\n')

	if *updateNegativeMatrix {
		// A table missing a route is not a table worth writing: the generator refuses to
		// produce a file that the test would immediately reject.
		if t.Failed() {
			t.Fatalf("refusing to write %s while the classification is incomplete", negativeMatrixPath)
		}
		if err := os.WriteFile(negativeMatrixPath, generated, 0o644); err != nil {
			t.Fatalf("write %s: %v", negativeMatrixPath, err)
		}
		t.Logf("wrote %s with %d routes", negativeMatrixPath, len(entries))
		return
	}

	checkedIn, err := os.ReadFile(negativeMatrixPath)
	if err != nil {
		t.Fatalf("read %s: %v (run go test ./internal/api -run TestEveryRouteDeclaresHowAnOutsiderIsDenied -update-negative-matrix)", negativeMatrixPath, err)
	}
	if string(checkedIn) != string(generated) {
		t.Fatalf("%s is stale: a route's policy or the classification changed. Re-run with -update-negative-matrix and review the diff.", negativeMatrixPath)
	}
}
