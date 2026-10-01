package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
	"brrpbrrp/internal/ws"
)

type principalKey struct{}

// principalOf returns the authority the route guard resolved for this request.
// Every handler that returns or changes community content must use it instead of
// inspecting users.is_admin.
func principalOf(r *http.Request) authz.Principal {
	principal, _ := r.Context().Value(principalKey{}).(authz.Principal)
	return principal
}

func withPrincipal(r *http.Request, principal authz.Principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), principalKey{}, principal))
}

// sessionKey carries the hash of the session that made the request, so a write
// transaction can prove the session still exists before it commits.
type sessionKey struct{}

func withSession(ctx context.Context, hash string) context.Context {
	if hash == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionKey{}, hash)
}

func sessionHashOf(ctx context.Context) string {
	hash, _ := ctx.Value(sessionKey{}).(string)
	return hash
}

// resolvePrincipal loads the caller's roles, ownership state and the committed
// authorization revision.
func (s *Server) resolvePrincipal(ctx context.Context, q db.Queryer, user db.User) (authz.Principal, error) {
	return s.authz.Resolve(ctx, q, user)
}

// viewer is the access identity used by scoped storage queries. It is built from
// the policy engine's own decision about this principal, so a scoped query and a
// per-resource check cannot answer differently.
func viewer(principal authz.Principal) db.Viewer {
	return db.ViewerForPrincipal(principal.UserID, principal.Allowed(authz.ChannelManage), principal.Allowed(authz.ChannelRead), principal.RoleIDs())
}

func (s *Server) requireCapability(w http.ResponseWriter, principal authz.Principal, capability authz.Capability) bool {
	if principal.Allowed(capability) {
		return true
	}
	problem(w, http.StatusForbidden, "Your role does not include "+string(capability)+".")
	return false
}

// hidden is the single response for a resource the caller may not know exists.
// It is deliberately identical to a missing resource so an outsider cannot
// confirm a guessed identifier.
func hidden(w http.ResponseWriter) { problem(w, http.StatusNotFound, "Not found.") }

// channelAccess loads one channel with its explicit grants and decides access
// through the policy engine.
func (s *Server) channelAccess(ctx context.Context, principal authz.Principal, channelID string) (db.Channel, db.ChannelAccess, bool, error) {
	channel, err := s.store.Channel(ctx, s.store, channelID)
	if errors.Is(err, sql.ErrNoRows) {
		return db.Channel{}, db.ChannelAccess{}, false, nil
	}
	if err != nil {
		return db.Channel{}, db.ChannelAccess{}, false, err
	}
	lists, err := s.store.ChannelAccessLists(ctx, s.store, channel.ID)
	if err != nil {
		return db.Channel{}, db.ChannelAccess{}, false, err
	}
	access := lists[channel.ID]
	return channel, access, principal.MayReadChannel(channel, access), nil
}

// requireChannel resolves a channel for a reading request. A hidden channel is
// answered exactly like a missing one.
func (s *Server) requireChannel(w http.ResponseWriter, r *http.Request, principal authz.Principal, channelID string) (db.Channel, db.ChannelAccess, bool) {
	channel, access, readable, err := s.channelAccess(r.Context(), principal, channelID)
	if err != nil {
		s.failed(w, err)
		return db.Channel{}, db.ChannelAccess{}, false
	}
	if !readable {
		hidden(w)
		return db.Channel{}, db.ChannelAccess{}, false
	}
	return channel, access, true
}

// requireChannelPost applies the reading decision plus the send capability and
// the channel's posting policy.
func (s *Server) requireChannelPost(w http.ResponseWriter, r *http.Request, principal authz.Principal, channelID string) (db.Channel, bool) {
	channel, access, ok := s.requireChannel(w, r, principal, channelID)
	if !ok {
		return db.Channel{}, false
	}
	if !principal.Allowed(authz.MessageSend) {
		problem(w, http.StatusForbidden, "Your role does not include message.send.")
		return db.Channel{}, false
	}
	if !principal.MayPostChannel(channel, access) {
		problem(w, http.StatusForbidden, "Only channel managers can post in an announcement channel.")
		return db.Channel{}, false
	}
	return channel, true
}

// requireMessage resolves a message through its conversation, applying the same
// rule as ordinary history so a context or reply path cannot leak one message.
func (s *Server) requireMessage(w http.ResponseWriter, r *http.Request, principal authz.Principal, messageID string) (db.Message, db.Channel, bool) {
	message, err := s.store.Message(r.Context(), principal.UserID, messageID)
	if errors.Is(err, sql.ErrNoRows) {
		hidden(w)
		return db.Message{}, db.Channel{}, false
	}
	if err != nil {
		s.failed(w, err)
		return db.Message{}, db.Channel{}, false
	}
	scope, _, ok := s.requireScope(w, r, db.User{ID: principal.UserID}, message.ConversationID, false)
	if !ok {
		return db.Message{}, db.Channel{}, false
	}
	return message, scope.Channel, true
}

// authorizeInTx re-resolves authority inside a security-sensitive write
// transaction. A guard evaluated before the transaction cannot be trusted once
// the transaction begins: revocation may have committed in between.
//
// Three things are therefore re-established from the transaction's own view: the
// account's owner/administrator/active state, the roles and ownership record, and
// the session that made the request. Anything that changed while the request was
// being prepared is seen here, and the request is refused instead of committing
// against authority it no longer has.
func (s *Server) authorizeInTx(ctx context.Context, tx *sql.Tx, user db.User) (authz.Principal, error) {
	principal, err := s.authz.Resolve(ctx, tx, user)
	if err != nil {
		return authz.Principal{}, err
	}
	if hash := sessionHashOf(ctx); hash != "" {
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE token_hash=? AND user_id=? AND expires_at>?`, hash, user.ID, db.Now()).Scan(&active); err != nil {
			return authz.Principal{}, err
		}
		if active != 1 {
			return authz.Principal{}, authz.ErrSessionEnded
		}
	}
	if principal.Inactive {
		return authz.Principal{}, authz.ErrSessionEnded
	}
	return principal, nil
}

// admitStream re-establishes a caller's authority and session for a response that is
// about to start streaming protected data, and fails when either has moved since the
// middleware resolved it.
//
// A registered response is cancelled by every later revocation, but a revocation that
// committed between the middleware's decision and the registration is observed by
// neither: the response would be admitted carrying authority that no longer exists, and
// the file's access check would be answered from the same stale principal. The two
// facts a write transaction checks before committing — the account's current roles and
// ownership state, and the session that made the request — are therefore checked here
// before a read begins to deliver bytes. The download path is the only caller: metadata
// responses finish inside the request that authorized them.
func (s *Server) admitStream(ctx context.Context, r *http.Request, user db.User) (authz.Principal, error) {
	principal, err := s.resolvePrincipal(ctx, s.store, user)
	if err != nil {
		return authz.Principal{}, err
	}
	if principal.Inactive {
		return authz.Principal{}, authz.ErrSessionEnded
	}
	if hash := currentSessionHash(r); hash != "" {
		var active int
		if err := s.store.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE token_hash=? AND user_id=? AND expires_at>?`, hash, user.ID, db.Now()).Scan(&active); err != nil {
			return authz.Principal{}, err
		}
		if active != 1 {
			return authz.Principal{}, authz.ErrSessionEnded
		}
	}
	return principal, nil
}

// requireCapabilityInTx re-establishes a route capability inside the committing
// transaction. A role that was revoked, a session that ended or an account that
// was deactivated between the guard and the commit is seen here, so a write
// cannot land on authority that no longer exists.
func (s *Server) requireCapabilityInTx(ctx context.Context, tx *sql.Tx, actor db.User, capability authz.Capability) (authz.Principal, error) {
	principal, err := s.authorizeInTx(ctx, tx, actor)
	if err != nil {
		return authz.Principal{}, err
	}
	if !principal.Allowed(capability) {
		return authz.Principal{}, authz.Denial{Reason: "Your role does not include " + string(capability) + "."}
	}
	return principal, nil
}

// problemError is a refusal or validation failure a handler wants answered with a
// specific status and message from inside its transaction.
type problemError struct {
	Status  int
	Message string
}

func (e problemError) Error() string { return e.Message }

func refuse(status int, message string) error { return problemError{Status: status, Message: message} }

// mutate runs a privileged change with its authority re-established inside the
// committing transaction.
//
// The route guard decides whether a request may reach a handler at all; it cannot
// decide whether the change may still commit, because a session, role, account or
// ownership change can land in between. Every privileged mutation goes through
// here or through an explicit requireCapabilityInTx on its own transaction, so
// "revoked a moment ago" and "revoked just before the commit" are the same
// answer.
func (s *Server) mutate(r *http.Request, actor db.User, capability authz.Capability, work func(tx *sql.Tx, principal authz.Principal) error) error {
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	principal, err := s.requireCapabilityInTx(r.Context(), tx, actor, capability)
	if err != nil {
		return err
	}
	if err := work(tx, principal); err != nil {
		return err
	}
	return tx.Commit()
}

// mutateReply runs a mutation and answers the request itself, so a handler reads
// as one decision instead of three error branches.
func (s *Server) mutateReply(w http.ResponseWriter, r *http.Request, actor db.User, capability authz.Capability, work func(tx *sql.Tx, principal authz.Principal) error) bool {
	err := s.mutate(r, actor, capability, work)
	if err == nil {
		return true
	}
	var refused problemError
	if errors.As(err, &refused) {
		problem(w, refused.Status, refused.Message)
		return false
	}
	s.authorityFailed(w, err)
	return false
}

// authorityFailed answers a failed in-transaction authorization with the right
// status: a session that ended is a signed-out caller, a denial is a refusal, and
// anything else is a server fault.
func (s *Server) authorityFailed(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, authz.ErrSessionEnded), errors.Is(err, authz.ErrUnavailable):
		problem(w, http.StatusUnauthorized, "This session ended while the request was in progress. Please sign in again.")
	case errors.Is(err, authz.ErrForbidden):
		problem(w, http.StatusForbidden, err.Error())
	default:
		s.failed(w, err)
	}
}

// requireChannelAccessInTx repeats the channel decision using the transaction's
// own view of the data.
func (s *Server) requireChannelAccessInTx(ctx context.Context, tx *sql.Tx, user db.User, channelID string) (authz.Principal, error) {
	principal, err := s.authorizeInTx(ctx, tx, user)
	if err != nil {
		return authz.Principal{}, err
	}
	channel, err := s.store.Channel(ctx, tx, channelID)
	if errors.Is(err, sql.ErrNoRows) {
		return principal, authz.ErrUnavailable
	}
	if err != nil {
		return principal, err
	}
	lists, err := s.store.ChannelAccessLists(ctx, tx, channel.ID)
	if err != nil {
		return principal, err
	}
	if !principal.MayReadChannel(channel, lists[channel.ID]) {
		return principal, authz.ErrUnavailable
	}
	if !principal.MayPostChannel(channel, lists[channel.ID]) {
		return principal, authz.ErrForbidden
	}
	return principal, nil
}

// channelAudience lists the active accounts that may read a channel. Delivery
// uses the same policy engine as a request, so an event cannot reach an account
// the server would refuse to answer.
//
// The query interface is a parameter, never implicit: the store holds a single
// connection, so a helper that reached for the store while a caller held a
// transaction would deadlock instead of failing.
//
// q must be one consistent read view. The audience is computed from the roles and
// ownership read through q, and the revision is read through q as well; if q is the
// bare store those are separate queries and a revocation can commit between them, so
// the stale audience is stamped with the new revision and looks consistent to the
// delivery check. Callers outside a transaction must use coherentAudience instead,
// which passes a transaction; callers already inside one pass their own transaction.
func (s *Server) channelAudience(ctx context.Context, q db.Queryer, channel db.Channel, access db.ChannelAccess) ([]string, int64, error) {
	authorities, err := s.store.AccountAuthorities(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	// A test can attempt an authorization change here, between the role reads and the
	// revision read. A coherent snapshot makes that attempt unable to commit. The hook
	// is loaded atomically and invoked without the fence: the change it attempts must
	// be able to reach its own commit path, not deadlock against this read.
	if hook := s.audienceInterleave.Load(); hook != nil {
		(*hook)()
	}
	owners, established, revision, err := s.store.AuthorizationSnapshot(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	audience := make([]string, 0, len(authorities))
	for _, authority := range authorities {
		if principalFor(authority, owners, established, revision).MayReadChannel(channel, access) {
			audience = append(audience, authority.User.ID)
		}
	}
	return audience, revision, nil
}

// coherentAudience resolves the accounts that may read one channel together with the
// authorization revision the answer was computed at, from a single read transaction.
//
// Resolving the audience and then reading the revision separately is the R4-01 defect: a
// revocation that commits between the two reads leaves an audience that still names the
// revoked account, and the following revision read stamps it with the revision the
// revocation committed. The delivery check then sees an audience and a revision that
// agree with each other, so it admits the event to an account the server has already
// refused. The store holds one connection, so the transaction is also the serialization
// point against every committing authority change: a revision bump cannot land between
// the channel, its grants, the effective roles, ownership, account state and the
// revision, because the writer needs the same connection.
func (s *Server) coherentAudience(ctx context.Context, channelID string) ([]string, int64, error) {
	tx, err := s.store.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	// Every path that does not commit the transaction releases the connection here, so a
	// resolution that fails part way through cannot hold it.
	defer tx.Rollback()
	audience, revision, err := s.channelAudienceIDs(ctx, tx, channelID)
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return audience, revision, nil
}

// admitted is the socket writer's write-admission decision. It is taken under the
// authority fence's read lock, so it is mutually exclusive with the commit of an
// authorization change and the publication of that change to the hub: admission either
// decides before the change is committed, or observes the change and refuses the event.
// The lock is released before any byte is written, so no network write is serialized
// behind it.
func (s *Server) admitted(client *ws.Client, queued ws.Queued) bool {
	s.authority.RLock()
	defer s.authority.RUnlock()
	return client.Admitted(queued)
}

// commitAuthorityChange commits a transaction that changed authorization and publishes the
// change's invalidation to the hub as one step under the authority fence.
//
// Admission and this function cannot interleave. A writer either takes the read lock
// before this critical section — deciding under the authority that was in force before the
// commit, which is a legitimate linearization before it — or after it, in which case the
// connections the change affects have already recorded the new revision and any event
// resolved earlier is refused. Every slow follow-up (stream revocation, voice enforcement)
// happens after the fence is released, so a delay there can no longer reopen the window
// the R5-01 defect lived in. Nothing here holds the fence across a network write; the
// commit uses the connection the caller's transaction already holds.
func (s *Server) commitAuthorityChange(ctx context.Context, tx *sql.Tx, reason string, userIDs ...string) error {
	return s.commitAuthorityChangeFor(ctx, tx, reason, ws.Audience{UserIDs: userIDs})
}

// commitAuthorityChangeFor is commitAuthorityChange with an explicit invalidation
// audience. A role, channel or ownership change reaches the accounts whose permissions
// moved; a credential or session change reaches the connections it ended, which is one
// session (sign-out, revoke one device) or every session of one account (password change,
// reset, factor removal, ban). Both share this one protocol, so there is no transition
// whose commit and whose delivery-eligibility update are separate steps.
//
// The audience is applied under the fence together with the commit, so the connections it
// names have recorded the new revision before the fence is released; a writer that decides
// later refuses events resolved before the change. Slow cleanup (stream revocation, voice
// enforcement, socket disconnection) runs after this function returns.
func (s *Server) commitAuthorityChangeFor(ctx context.Context, tx *sql.Tx, reason string, audience ws.Audience) error {
	s.authority.Lock()
	defer s.authority.Unlock()
	// The revision is read from the transaction that is about to commit, so it is the one
	// the change produces and no later revision can be mistaken for it.
	revision, err := s.store.AuthorizationRevision(ctx, tx)
	if err != nil {
		return err
	}
	// Account permission and session validity are separate dimensions. Invalidation
	// alone changes a generation and drops old events, but an event resolved after
	// this commit would be queued at the new generation even on an ended session.
	// Snapshot the affected sockets and check their sessions through this same
	// transaction. No new socket can register while the fence is held; no query
	// reaches for a second database connection, and no hub lock spans a query.
	var ended []string
	for hash, userID := range s.hub.SessionIdentities(audience) {
		var active bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.user_id=? AND s.expires_at>? AND u.is_banned=0 AND u.is_deactivated=0)`, hash, userID, db.Now()).Scan(&active); err != nil {
			return err
		}
		if !active {
			ended = append(ended, hash)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.hub.EndSessions(ended)
	s.hub.Invalidate(audience, revision, "authorization.changed", map[string]any{"reason": reason, "refetch": true})
	return nil
}

// registerConnection admits a new socket connection under the authority fence.
//
// The route guard resolved the caller before this point, but a guard decision and a
// connection registration are two moments: a ban, a session revocation or a credential
// reset can commit in between, and a connection inserted into the hub afterwards would
// carry authority the server has already withdrawn — the invalidation that accompanied the
// change could not reach it because it was not in the hub yet.
//
// The protocol is the one documented on Server.authority. The account and its session are
// re-established from the store inside a transaction: the store holds a single connection,
// so no authority or credential change can commit while the check reads, and the check
// itself holds the connection first. The fence is taken next, the read transaction is
// committed and the connection is inserted into the hub with the revision it was admitted
// under, all under the fence. A change that commits before the check is observed by it and
// refuses the connection; a change that commits after the insertion finds the connection
// and invalidates it. The check is a read transaction, so it releases the connection with
// no state change when the caller is refused.
func (s *Server) registerConnection(ctx context.Context, user db.User, client *ws.Client) (authz.Principal, error) {
	if hook := s.connectionAdmission.Load(); hook != nil {
		(*hook)()
	}
	tx, err := s.store.BeginTx(ctx, nil)
	if err != nil {
		return authz.Principal{}, err
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(ctx, tx, user)
	if err != nil {
		return authz.Principal{}, err
	}
	s.authority.Lock()
	defer s.authority.Unlock()
	if err := tx.Commit(); err != nil {
		return authz.Principal{}, err
	}
	client.ObserveRevision(principal.Revision)
	if !s.hub.Add(client) {
		return authz.Principal{}, errTooManyConnections
	}
	return principal, nil
}

// errTooManyConnections reports a connection limit rather than a refusal, so the caller
// can tell the client which of the two happened.
var errTooManyConnections = errors.New("too many connections")

// authorizationRevision reads the committed authorization revision. It is used where a
// delivery decision is made without resolving an audience, so the decision can be
// tagged with the authority it was made under.
func (s *Server) authorizationRevision(ctx context.Context) int64 {
	revision, err := s.store.AuthorizationRevision(ctx, s.store)
	if err != nil {
		s.log.Warn("authorization revision lookup failed", "error", err)
		return 0
	}
	return revision
}

// principalFor converts a stored account and its roles into a policy principal.
// It is the one conversion used by delivery, mention eligibility and dispatch
// checks, so an audience and a request can never disagree about the rules.
func principalFor(authority db.AccountAuthority, owners int, established bool, revision int64) authz.Principal {
	return authz.PrincipalFor(authority.User, authority.Roles, owners, established, revision)
}

// principalForAuthority is the same resolution for the revalidation passes, with the
// account's live suspension included. A bulk principal that omits it answers a
// different question than a request would, which is how a timed-out account kept a
// community call and kept streaming community content: nobody had asked (review 17,
// V17-04). The audience paths deliberately use the plain builder: they decide whether
// an account may receive an event at all, and a suspension is enforced where it
// matters — every read, join and stream of community content.
func principalForAuthority(authority db.AccountAuthority, owners int, established bool, revision int64) authz.Principal {
	principal := principalFor(authority, owners, established, revision)
	principal.Suspended = authority.Suspended
	return principal
}

// channelEligible lists the accounts that may currently read a channel. Mention
// targets are drawn from it, so a mention cannot notify someone who cannot open
// the conversation.
func (s *Server) channelEligible(ctx context.Context, q db.Queryer, channel db.Channel) ([]string, error) {
	lists, err := s.store.ChannelAccessLists(ctx, q, channel.ID)
	if err != nil {
		return nil, err
	}
	eligible, _, err := s.channelAudience(ctx, q, channel, lists[channel.ID])
	return eligible, err
}

// publishToChannel sends an event only to accounts that may read the channel. A
// resource an account cannot read must not produce metadata or activity, so
// restricted-channel events never reach an unrelated connection.
//
// A lookup that cannot be completed fails closed. Broadcasting a payload because
// its audience is unknown would turn every database hiccup and every
// delete-vs-read race into a disclosure, which is exactly how a private channel's
// name and topic used to reach unrelated sockets. Nothing is delivered instead;
// connected clients refetch state on their next request or reconnect.
func (s *Server) publishToChannel(ctx context.Context, channelID, kind string, data any) {
	audience, revision, err := s.coherentAudience(ctx, channelID)
	if err != nil {
		s.log.Warn("channel event audience lookup failed; event withheld", "error", err, "kind", kind)
		return
	}
	// Send admission is decided against committed authority, not only against the snapshot
	// the audience came from. A change can still commit after the transaction ends and
	// before this dispatch, and its invalidation has not necessarily reached the hub yet,
	// so the revision the audience was resolved at is compared with the revision committed
	// now. When they differ the event is withheld rather than delivered under a revision
	// that has already been replaced; the change's own invalidation is what tells clients
	// to refetch, and nothing here holds a database lock across a network write.
	if committed := s.authorizationRevision(ctx); committed != revision {
		s.log.Info("channel event withheld: authority changed while the audience was dispatched", "kind", kind, "resolved", revision, "committed", committed)
		return
	}
	// The revision the audience was resolved at travels with the event: a connection
	// whose authority has already moved past it does not receive the event, and the
	// writer re-checks the same revision immediately before it writes bytes.
	s.hub.BroadcastToRevision(audience, revision, kind, data)
}

// channelAudienceIDs resolves the accounts that may read one channel, together with
// the authorization revision the answer was computed at.
func (s *Server) channelAudienceIDs(ctx context.Context, q db.Queryer, channelID string) ([]string, int64, error) {
	channel, err := s.store.Channel(ctx, q, channelID)
	if err != nil {
		return nil, 0, err
	}
	lists, err := s.store.ChannelAccessLists(ctx, q, channelID)
	if err != nil {
		return nil, 0, err
	}
	return s.channelAudience(ctx, q, channel, lists[channelID])
}

// publishAuthorizationChange tells affected clients that their authority moved and
// they must refetch. It never carries the content it revoked, and it invalidates the
// affected connections rather than only announcing the change, so an event already
// queued under the previous authority is dropped instead of delivered.
//
// It publishes under the authority fence so the invalidation is ordered against write
// admission exactly as a revision-changing commit is; a change that does bump the
// revision commits through commitAuthorityChange instead.
func (s *Server) publishAuthorizationChange(reason string, userIDs ...string) {
	revision := s.authorityRevisionBeforeFence()
	s.authority.Lock()
	defer s.authority.Unlock()
	s.hub.Invalidate(ws.Audience{UserIDs: userIDs}, revision, "authorization.changed", map[string]any{"reason": reason, "refetch": true})
}

// authorityRevisionBeforeFence reads the committed authorization revision for an
// invalidation that is about to take the fence.
//
// The acquisition order documented on Server.authority puts the store's connection
// first and the fence second, so a revision lookup for a standalone invalidation happens
// *before* the fence is taken. Holding the fence across the lookup is the R6-01 lock-order
// cycle: a committing transaction holds the connection and waits for the fence while the
// invalidation holds the fence and waits for the connection, and only cancellation breaks
// the cycle. The revision read here is a floor for the invalidation, never a ceiling:
// ObserveRevision only moves a connection's revision forward, and a change that commits
// between this read and the fence records its own newer revision under the fence itself.
func (s *Server) authorityRevisionBeforeFence() int64 {
	if hook := s.standaloneInvalidation.Load(); hook != nil {
		(*hook)()
	}
	return s.authorizationRevision(context.Background())
}

// endStreams ends the file and preview responses of the listed accounts. An
// account-level authority change does not know which channels it affects, so every
// stream those accounts were admitted for stops, while unrelated accounts keep what
// they were reading.
func (s *Server) endStreams(userIDs []string) int {
	ended := 0
	for _, userID := range userIDs {
		ended += s.deliveries.revokeAccount(userID)
	}
	return ended
}

// endUnreadableStreams ends the responses of the listed accounts whose content they
// can no longer read under the authority that is committed now, and leaves the rest
// running.
//
// A change to a role's capabilities does not know in advance which conversations it
// affects: the same role can be granted on any number of channels, an export carries a
// whole set of conversations, and an account that keeps channel.read through another
// role keeps what it was already streaming. So the decision is made per registered
// response, scope by scope, with the same policy a conversation list or a download
// admission asks — rather than by ending everything the accounts had open. The list is
// the effective holders of the changed role — including the accounts that hold the
// default role without an assignment row — which is what makes an implicit holder's
// stream stop.
//
// The response metadata is read from the registry without its lock, because deciding
// here needs database reads; each decision is then applied by identifier, so a response
// that was replaced in between is left alone.
func (s *Server) endUnreadableStreams(ctx context.Context, userIDs []string) int {
	if len(userIDs) == 0 {
		return 0
	}
	affected := make(map[string]bool, len(userIDs))
	for _, userID := range userIDs {
		affected[userID] = true
	}
	records := s.deliveries.snapshot()
	if len(records) == 0 {
		return 0
	}
	authorities, err := s.store.AccountAuthorities(ctx, s.store)
	if err != nil {
		// Nothing is ended on a lookup failure: an account whose authority is unknown is
		// not silently cut off, and the next change or the stream's own end closes it.
		s.log.Warn("stream revocation lookup failed", "error", err)
		return 0
	}
	owners, established, revision, err := s.store.AuthorizationSnapshot(ctx, s.store)
	if err != nil {
		s.log.Warn("stream revocation lookup failed", "error", err)
		return 0
	}
	principals := make(map[string]authz.Principal, len(userIDs))
	for _, authority := range authorities {
		if affected[authority.User.ID] {
			principals[authority.User.ID] = principalForAuthority(authority, owners, established, revision)
		}
	}
	decided := make(map[string]bool)
	ending := make([]int, 0, len(records))
	for _, record := range records {
		if !affected[record.userID] {
			continue
		}
		principal, known := principals[record.userID]
		if !known {
			// The account no longer resolves: nothing of its may keep streaming.
			ending = append(ending, record.id)
			continue
		}
		// A response with no conversation scope is an unclaimed upload, which belongs to
		// its uploader alone; an account-level change ends it here as it always did.
		if len(record.scopes) == 0 {
			continue
		}
		// One unreadable scope ends the whole response. A file is one document: half of
		// an export is content the member can no longer read by any other route.
		blocked := false
		for _, scope := range record.scopes {
			key := record.userID + "\x00" + scope
			readable, seen := decided[key]
			if !seen {
				var err error
				readable, err = s.exportScopeStillReadable(ctx, s.store, principal, scope)
				if err != nil {
					// A scope that cannot be resolved is one this response may not keep
					// streaming: the alternative is serving under an unknown decision.
					s.log.Warn("stream revocation lookup failed", "error", err)
					readable = false
				}
				decided[key] = readable
			}
			if !readable {
				blocked = true
				break
			}
		}
		if blocked {
			ending = append(ending, record.id)
		}
	}
	return s.deliveries.endIDs(ending)
}

// invalidateWorkspace tells every connection that workspace structure or visibility
// moved. For the connections it reaches it is the same boundary as an authorization
// change: what was queued before it is not delivered under the new structure.
func (s *Server) invalidateWorkspace(reason string) {
	revision := s.authorityRevisionBeforeFence()
	s.authority.Lock()
	defer s.authority.Unlock()
	s.hub.Invalidate(ws.Audience{}, revision, "workspace.changed", map[string]string{"reason": reason})
}
