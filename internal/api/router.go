package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
	"brrpbrrp/internal/rtc"
	"brrpbrrp/internal/scraper"
	"brrpbrrp/internal/ws"
	"brrpbrrp/web"
	"github.com/pion/webrtc/v4"
)

type Server struct {
	connectionsMu    sync.Mutex
	connections      sync.WaitGroup
	closing          bool
	settingsUpdateMu sync.Mutex
	filesMu          sync.Mutex
	backupMu         sync.Mutex
	voice            *rtc.SFU
	voicePublished   voicePublisher
	// alertState is the raised-alert history and its delivery record. It is created
	// lazily by alerts(), so a server that never raises an alert never allocates it.
	alertMu    sync.Mutex
	alertState *alertState
	// voiceGate serializes voice admission against access enforcement.
	voiceGate sync.Mutex
	// authority is the delivery fence. Its write lock is held across the moment an
	// authorization, credential or session change commits *and* the hub records that
	// change on the connections it affects; its read lock is held while a socket writer
	// decides whether to admit an event. That makes admission mutually exclusive with
	// the commit of the authority the admission depends on, so an event resolved before
	// a revocation cannot be admitted after it, however long the change's slower work
	// (stream revocation, voice enforcement, socket disconnection) takes afterwards. It
	// is never held across a network write or a database transaction that is not the
	// change itself.
	//
	// Acquisition order (ADR 0009). Any path that needs both the store and the fence
	// takes the store first and the fence second:
	//
	//	1. a write transaction is begun, which takes the store's single connection;
	//	2. the change is prepared and its revision is read through that transaction;
	//
	//	3. the fence is taken, the transaction commits and the hub invalidates the
	//	   change's audience, all under the fence;
	//	4. the fence is released, and only then do the slow follow-ups run (ending
	//	   streams, enforcing voice access, disconnecting sockets).
	//
	// A standalone invalidation has no transaction, so it reads the committed revision
	// before it takes the fence (authorityRevisionBeforeFence). Nothing waits for the
	// store's connection while holding the fence, which is what removes the R6-01
	// cycle: a committing transaction holds the connection and waits for the fence
	// while the invalidation holds the fence and waits for the connection. Connection
	// registration follows the same order: it re-establishes the caller's authority and
	// session inside a read transaction, then takes the fence, commits that transaction
	// and inserts the connection into the hub (registerConnection).
	authority    sync.RWMutex
	deliveries   deliveries
	turn         *rtc.TURN
	vapidPrivate string
	vapidPublic  string
	pushJobs     chan pushJob
	// pushHTTP is the client the push worker uses. Tests replace it with a stub
	// transport so a queued notification's dispatch decision can be observed
	// without reaching a push service.
	pushHTTP    *http.Client
	workers     sync.WaitGroup
	scraper     *scraper.Scraper
	previewJobs chan previewJob
	ctx         context.Context
	cancel      context.CancelFunc
	hub         *ws.Hub
	config      app.Config
	store       *db.Store
	authz       *authz.Service
	log         *slog.Logger
	started     time.Time
	setupMu     sync.Mutex
	// streamAdmission, when set, runs after a streamed response is registered and
	// before its authority is re-established. A test uses it to interleave a
	// revocation between registration and admission, which is the ordering the
	// admission decision exists to close; production leaves it nil. It is stored
	// atomically because the hook may run on a handler goroutine while the test that
	// installed it is still running, so an ordinary field would be a data race.
	streamAdmission atomic.Pointer[func()]
	// audienceInterleave, when set, runs while an audience snapshot is open, between
	// the channel and role reads and the revision read. A test uses it to attempt an
	// authorization change at exactly that point; because the snapshot holds the
	// store's only connection, the attempt cannot commit inside it, which is the
	// property the coherent snapshot exists to provide. Production leaves it nil. It
	// is stored atomically for the same reason as streamAdmission: the hook is read
	// from whichever goroutine is resolving an audience.
	audienceInterleave atomic.Pointer[func()]
	// writerAdmission, when set, runs in the socket writer immediately before the
	// admission decision. A test pauses there to commit a revocation while the write is
	// pending, which is the R5-01 ordering; production leaves it nil.
	writerAdmission atomic.Pointer[func(*ws.Client, ws.Queued)]
	// standaloneInvalidation, when set, runs at the start of a standalone invalidation,
	// before the committed revision is read and before the authority fence is taken. A
	// test uses it to prove the documented acquisition order: at that point the
	// invalidation is not yet holding the fence, so a concurrent committing transaction
	// completes instead of waiting on a lock cycle (R6-01). Production leaves it nil.
	standaloneInvalidation atomic.Pointer[func()]
	// connectionAdmission, when set, runs before a new socket connection re-establishes
	// its authority and session in registerConnection. A test uses it to commit a
	// credential change between the route guard and the registration check, which is the
	// ordering registration exists to close (R6-02). Production leaves it nil.
	connectionAdmission atomic.Pointer[func()]
	settingsMu          sync.RWMutex
	settings            db.Settings
	passwordSlots       chan struct{}
	thumbnailSlots      chan struct{}
	limiter             *rateLimiter
}

func New(config app.Config, store *db.Store, log *slog.Logger) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())
	// The push client is built once, before the worker starts, so a test can
	// replace it and observe what the worker decided to send. A push endpoint that
	// answers with a redirect is not followed: the destination comes from a device
	// subscription, not from this server's configuration.
	pushClient := scraper.HTTPClient(5 * time.Second)
	pushClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s := &Server{
		pushJobs:       make(chan pushJob, 128),
		pushHTTP:       pushClient,
		scraper:        scraper.New(),
		previewJobs:    make(chan previewJob, 64),
		ctx:            ctx,
		cancel:         cancel,
		hub:            ws.New(),
		config:         config,
		store:          store,
		authz:          authz.NewService(store),
		log:            log,
		started:        time.Now(),
		settings:       db.DefaultSettings(),
		passwordSlots:  make(chan struct{}, 2),
		thumbnailSlots: make(chan struct{}, 1),
		limiter:        newRateLimiter(),
	}
	var saved string
	if err := store.QueryRow(`SELECT value FROM settings WHERE key='public'`).Scan(&saved); err == nil {
		if err := json.Unmarshal([]byte(saved), &s.settings); err != nil {
			cancel()
			return nil, err
		}
	}
	// The audit log is mandatory, including for an installation that stored the
	// old setting before it became one.
	s.settings.Audit = true
	if err := s.prepareSetup(); err != nil {
		cancel()
		return nil, err
	}
	if err := s.preparePush(); err != nil {
		cancel()
		return nil, err
	}
	// Voice presence is published per channel so a restricted channel's
	// participants never reach an account that may not read it.
	voice, err := rtc.New(rtc.Options{ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}, ExternalIP: config.VoiceExternalIP, PortMin: config.VoicePortMin, PortMax: config.VoicePortMax}, s.publishVoiceStates)
	if err != nil {
		cancel()
		return nil, err
	}
	s.voice = voice
	s.turn, err = rtc.NewTURN()
	if err != nil {
		cancel()
		voice.Close()
		return nil, err
	}
	if err = s.configureTURN(s.settings); err != nil {
		cancel()
		voice.Close()
		return nil, err
	}
	s.workers.Add(1)
	go s.pushWorker()
	s.workers.Add(1)
	go s.maintenanceWorker()
	for range 2 {
		s.workers.Add(1)
		go s.previewWorker()
	}
	return s, nil
}

func (s *Server) Settings() db.Settings {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.settings
}

// route is one endpoint with the authority it requires. Routes are declared in a
// table rather than registered inline so a new endpoint cannot be added without
// stating its policy, and so the inventory test can check every entry.
type route struct {
	Method  string
	Path    string
	Policy  routePolicy
	Handler authenticatedHandler
}

// routes is the complete authenticated endpoint inventory.
func (s *Server) routes() []route {
	return []route{
		{http.MethodGet, "/ws", routePolicy{Note: "realtime session; events are scoped per delivery"}, s.websocket},
		{http.MethodPost, "/api/logout", routePolicy{Note: "acts only on the caller's own session"}, s.logout},
		{http.MethodGet, "/api/me", routePolicy{Note: "returns the caller's own account"}, s.me},
		{http.MethodGet, "/api/me/mfa", routePolicy{Note: "returns the caller's own enrollment state"}, s.mfaStatus},
		{http.MethodPost, "/api/me/mfa/setup", routePolicy{Note: "any member may enroll a second factor; enrollment is the prerequisite for privileged actions"}, s.setupMFA},
		{http.MethodPost, "/api/me/mfa/enable", routePolicy{Note: "any member may enroll a second factor; confirms a password and a code"}, s.enableMFA},
		{http.MethodPost, "/api/me/mfa/disable", routePolicy{Note: "acts only on the caller's own enrollment and requires a current factor"}, s.disableMFA},
		{http.MethodPost, "/api/me/password", routePolicy{Note: "acts only on the caller's own credentials"}, s.changePassword},
		{http.MethodGet, "/api/me/sessions", routePolicy{Note: "lists the caller's own sessions without token material"}, s.listSessions},
		{http.MethodDelete, "/api/me/sessions", routePolicy{Note: "signs out the caller's other devices and keeps this one"}, s.revokeOtherSessions},
		{http.MethodDelete, "/api/me/sessions/{id}", routePolicy{Note: "revokes one of the caller's own sessions and closes its sockets"}, s.revokeSession},
		{http.MethodPost, "/api/me/reauth", routePolicy{Note: "refreshes the caller's own recent authentication after a password and factor check"}, s.reauth},
		{http.MethodGet, "/api/state", routePolicy{Note: "the snapshot is filtered to readable channels and own cursors"}, s.state},
		{http.MethodGet, "/api/unread", routePolicy{Note: "counts readable channels only, before aggregation"}, s.unreadState},
		{http.MethodGet, "/api/channels", routePolicy{Note: "the handler returns only readable channels"}, s.listChannels},
		{http.MethodPost, "/api/channels", routePolicy{Capability: authz.ChannelManage}, s.createChannel},
		{http.MethodPost, "/api/channels/{id}/move", routePolicy{Capability: authz.ChannelManage}, s.moveChannel},
		{http.MethodPatch, "/api/channels/{id}", routePolicy{Capability: authz.ChannelManage}, s.updateChannel},
		{http.MethodDelete, "/api/channels/{id}", routePolicy{Capability: authz.ChannelManage}, s.deleteChannel},
		{http.MethodGet, "/api/channels/{id}/access", routePolicy{Capability: authz.ChannelManage}, s.channelAccessList},
		{http.MethodPut, "/api/channels/{id}/access", routePolicy{Capability: authz.ChannelManage}, s.setChannelAccessList},
		{http.MethodPost, "/api/channels/{id}/read", routePolicy{Note: "read cursor; the handler applies channel access"}, s.markRead},
		{http.MethodGet, "/api/channels/{id}/messages", routePolicy{Note: "the handler resolves the channel's current access before reading"}, s.listMessages},
		{http.MethodGet, "/api/channels/{id}/first-unread", routePolicy{Note: "resolves channel access before returning a position"}, s.firstUnreadMessage},
		{http.MethodPost, "/api/channels/{id}/messages", routePolicy{Capability: authz.MessageSend}, s.createMessage},
		{http.MethodGet, "/api/channels/{id}/threads", routePolicy{Note: "threads of a channel the caller can read; no separate access rule (ADR 0017)"}, s.listThreads},
		{http.MethodPost, "/api/channels/{id}/threads", routePolicy{Capability: authz.MessageSend, Note: "starting a thread writes to the channel, so it takes the channel's post rule"}, s.createThread},
		{http.MethodGet, "/api/threads/{id}", routePolicy{Note: "resolves the thread's channel access before returning anything"}, s.getThread},
		{http.MethodGet, "/api/threads/{id}/messages", routePolicy{Note: "bounded cursor page of one thread; the channel's read rule applies"}, s.listThreadMessages},
		{http.MethodPost, "/api/threads/{id}/messages", routePolicy{Capability: authz.MessageSend, Note: "re-establishes channel post authority and the thread's own state inside the transaction"}, s.createThreadMessage},
		{http.MethodPost, "/api/threads/{id}/follow", routePolicy{Note: "personal subscription, bounded on the write"}, s.followThreadRoute},
		{http.MethodDelete, "/api/threads/{id}/follow", routePolicy{Note: "unfollowing never touches messages or read state"}, s.unfollowThreadRoute},
		{http.MethodPut, "/api/threads/{id}/state", routePolicy{Capability: authz.ChannelManage, Note: "archiving, reopening and locking a thread change what the whole channel can write"}, s.setThreadState},
		{http.MethodPost, "/api/admin/reports/{id}/assign", routePolicy{Capability: authz.MessageDeleteAny, Note: "a claim about who reviews, not a grant; the reviewer still needs their own current capability"}, s.assignReport},
		{http.MethodPost, "/api/admin/accounts/{id}/timeout", routePolicy{Capability: authz.MemberManage, Note: "the downward-only hierarchy check runs inside the transaction against current authority"}, s.applyTimeout},
		{http.MethodDelete, "/api/admin/accounts/{id}/timeout", routePolicy{Capability: authz.MemberManage, Note: "lifting early is a recorded decision, distinct from expiry"}, s.liftTimeout},
		// A member's own data. Gated on recent strong authentication because the file
		// leaves the building, which is the only read path in the application that does
		// (ADR 0019b).
		{http.MethodPost, "/api/me/export", routePolicy{FreshAuth: true, Note: "one account's own data, gated on recent strong authentication because the file leaves the building"}, s.requestExport},
		{http.MethodGet, "/api/me/export", routePolicy{Note: "this account's own export history, including what each file could not include"}, s.listExports},
		{http.MethodGet, "/api/me/export/{id}", routePolicy{Note: "authority is re-read at download, not only at build: a file must not outlive the permission that made it"}, s.downloadExport},
		{http.MethodDelete, "/api/me/export/{id}", routePolicy{Note: "the member's own deletion of a produced file"}, s.deleteExport},
		{http.MethodGet, "/api/admin/alerts", routePolicy{Capability: authz.OperatorBackup, Note: "answered by what was delivered, not by what is configured; a destination that has never delivered is worse than none"}, s.alertStatusHandler},
		{http.MethodGet, "/api/directs", routePolicy{Note: "own consented conversations and contact requests only; gated"}, s.listDirectConversations},
		{http.MethodPost, "/api/directs", routePolicy{Note: "current consent/contact policy; new requests recheck message.send"}, s.createDirectConversation},
		{http.MethodPost, "/api/directs/{id}/accept", routePolicy{Note: "only intended recipient may accept a direct request"}, s.acceptDirectConversation},
		{http.MethodPost, "/api/directs/{id}/reject", routePolicy{Note: "recipient rejects or requester withdraws a pending direct request"}, s.rejectDirectConversation},
		{http.MethodPut, "/api/directs/{id}/visibility", routePolicy{Note: "hides or reopens only caller conversation list entry"}, s.setDirectVisibility},
		{http.MethodGet, "/api/me/contact-preferences", routePolicy{Note: "own contact preferences"}, s.contactPreferences},
		{http.MethodPut, "/api/me/contact-preferences", routePolicy{Note: "own contact preferences"}, s.setContactPreferences},
		{http.MethodGet, "/api/me/blocks", routePolicy{Note: "own blocked contacts only"}, s.listBlocks},
		{http.MethodPut, "/api/me/blocks/{id}", routePolicy{Note: "block contact without changing shared community permissions"}, s.blockContact},
		{http.MethodDelete, "/api/me/blocks/{id}", routePolicy{Note: "remove own block without replaying suppressed work"}, s.unblockContact},

		{http.MethodGet, "/api/conversations/{id}/messages", routePolicy{Note: "accepted conversation members or authorized community readers only"}, s.listMessages},
		{http.MethodPost, "/api/conversations/{id}/messages", routePolicy{Capability: authz.MessageSend}, s.createMessage},
		{http.MethodGet, "/api/conversations/{id}/first-unread", routePolicy{Note: "current conversation access before returning positions"}, s.firstUnreadMessage},
		{http.MethodPost, "/api/conversations/{id}/read", routePolicy{Note: "own cursor after current conversation/session authorization"}, s.markRead},
		{http.MethodGet, "/api/conversations/{id}/pins", routePolicy{Note: "current conversation access before returning shared pins"}, s.pins},

		{http.MethodGet, "/api/messages/{id}", routePolicy{Note: "message access is resolved through its conversation"}, s.getMessage},
		{http.MethodGet, "/api/messages/{id}/context", routePolicy{Note: "context uses the same access rule as ordinary history"}, s.messageContext},
		{http.MethodGet, "/api/messages/{id}/replies", routePolicy{Note: "replies use the same access rule as ordinary history"}, s.messageReplies},
		{http.MethodPut, "/api/messages/{id}/pin", routePolicy{Capability: authz.MessagePin}, s.pinMessage},
		{http.MethodGet, "/api/channels/{id}/pins", routePolicy{Note: "pins are channel content and follow channel access"}, s.pins},
		{http.MethodPatch, "/api/messages/{id}", routePolicy{Note: "a member may edit only its own message in a readable channel"}, s.editMessage},
		{http.MethodDelete, "/api/messages/{id}", routePolicy{Note: "own content, or message.delete_any for moderators"}, s.deleteMessage},
		{http.MethodPost, "/api/messages/{id}/reactions", routePolicy{Capability: authz.MessageReact}, s.toggleReaction},
		{http.MethodGet, "/api/search", routePolicy{Note: "the query is filtered to readable conversations before limits"}, s.search},
		{http.MethodPost, "/api/uploads", routePolicy{Capability: authz.MessageUpload}, s.upload},
		{http.MethodGet, "/api/uploads/{id}", routePolicy{Note: "resolves the attachment's current conversation access"}, s.download},
		{http.MethodPost, "/api/push/subscribe", routePolicy{Note: "registers the caller's own device"}, s.subscribePush},
		{http.MethodGet, "/api/push/devices", routePolicy{Note: "returns endpoint hashes for the caller's own devices"}, s.pushDevices},
		{http.MethodDelete, "/api/push/subscribe", routePolicy{Note: "removes the caller's own device"}, s.unsubscribePush},
		{http.MethodGet, "/api/categories", routePolicy{Note: "category names are shared workspace structure, not private content"}, s.listCategories},
		{http.MethodPost, "/api/categories", routePolicy{Capability: authz.CategoryManage}, s.createCategory},
		{http.MethodPatch, "/api/categories/{id}", routePolicy{Capability: authz.CategoryManage}, s.updateCategory},
		{http.MethodDelete, "/api/categories/{id}", routePolicy{Capability: authz.CategoryManage}, s.deleteCategory},
		{http.MethodGet, "/api/roles", routePolicy{Capability: authz.RoleManage}, s.listRoles},
		{http.MethodPost, "/api/roles", routePolicy{Capability: authz.RoleManage}, s.createRole},
		{http.MethodPatch, "/api/roles/{id}", routePolicy{Capability: authz.RoleManage}, s.updateRole},
		{http.MethodDelete, "/api/roles/{id}", routePolicy{Capability: authz.RoleManage}, s.deleteRole},
		{http.MethodGet, "/api/admin/accounts/summary", routePolicy{Capability: authz.RoleManage, Note: "the account list a role manager may see: identity and role assignments only, because assigning a role needs no credential, session or moderation state"}, s.accountSummaries},
		{http.MethodGet, "/api/admin/users/{id}/roles", routePolicy{Capability: authz.RoleManage}, s.userRoles},
		{http.MethodPut, "/api/admin/users/{id}/roles", routePolicy{Capability: authz.RoleManage}, s.setUserRoles},
		{http.MethodPost, "/api/owner", routePolicy{Capability: authz.OperatorOwnershipTransfer, FreshAuth: true, Note: "completes or transfers the single ownership; needs a recent strong sign-in"}, s.designateOwner},
		{http.MethodGet, "/api/admin/invitations", routePolicy{Capability: authz.InviteManage}, s.invitations},
		{http.MethodPost, "/api/admin/invitations", routePolicy{Capability: authz.InviteManage}, s.createInvitation},
		{http.MethodDelete, "/api/admin/invitations/{id}", routePolicy{Capability: authz.InviteManage}, s.revokeInvitation},
		{http.MethodGet, "/api/admin/users", routePolicy{Capability: authz.MemberManage}, s.adminUsers},
		{http.MethodPost, "/api/admin/users", routePolicy{Capability: authz.MemberManage}, s.createUser},
		{http.MethodPatch, "/api/admin/users/{id}", routePolicy{Capability: authz.MemberManage}, s.updateUser},
		{http.MethodDelete, "/api/admin/users/{id}", routePolicy{Capability: authz.MemberManage}, s.deleteUser},
		{http.MethodPost, "/api/admin/users/{id}/anonymize", routePolicy{Capability: authz.MemberManage, FreshAuth: true, Note: "irreversible account removal preserves history and requires a recent sign-in"}, s.anonymizeUser},
		{http.MethodPost, "/api/messages/{id}/report", routePolicy{Note: "members report one currently readable community message; no conversation-wide export"}, s.reportMessage},
		{http.MethodGet, "/api/admin/reports", routePolicy{Capability: authz.MessageDeleteAny}, s.listReports},
		{http.MethodPatch, "/api/admin/reports/{id}", routePolicy{Capability: authz.MessageDeleteAny}, s.reviewReport},
		// Private message evidence (ADR 0013). Reporting requires current accepted
		// read access, not write access, so a participant who blocked the other
		// person can still report retained history; reviewing requires the separate
		// report.review_private grant and confers no access to the conversation.
		{http.MethodGet, "/api/messages/{id}/private-report-preview", routePolicy{Note: "returns the exact server-side snapshot a private report would save, for the caller's own readable accepted direct message"}, s.previewPrivateEvidence},
		{http.MethodPost, "/api/messages/{id}/private-report", routePolicy{Note: "stores one confirmed private evidence snapshot; the submitted text is the server's, never the client's"}, s.submitPrivateEvidence},
		{http.MethodGet, "/api/admin/private-reports", routePolicy{Capability: authz.ReportReviewPrivate, Note: "reads only voluntarily submitted evidence snapshots; grants no private membership, history, search or files"}, s.listPrivateEvidence},
		{http.MethodPatch, "/api/admin/private-reports/{id}", routePolicy{Capability: authz.ReportReviewPrivate, Note: "records nonpunitive triage of one evidence record"}, s.reviewPrivateEvidence},
		// Recipient-only reporting of a pending request, and the durable command
		// receipt that makes it retryable (ADR 0014). Neither route consults
		// accepted-history access: a pending recipient has none, which is the point.
		{http.MethodGet, "/api/directs/{id}/report-preview", routePolicy{Note: "the exact introduction a report would save, for the recipient of the current attempt only"}, s.previewRequestEvidence},
		{http.MethodPost, "/api/directs/{id}/report-and-block", routePolicy{Note: "stores introduction evidence, rejects the request, clears it and blocks the sender in one transaction; never accepts contact"}, s.reportRequestAndBlock},
		{http.MethodGet, "/api/me/report-receipt", routePolicy{Note: "the caller's own minimal outcome for their own operation key; never reviewer or evidence data"}, s.ownReportReceipt},
		// Durable account activity is the caller's own record of what it was told
		// about, and is readable only about the signed-in account. It is not a
		// privileged view of anyone else's inbox, and a row is filtered against
		// current access before it is returned (ADR 0015).
		{http.MethodGet, "/api/me/activity", routePolicy{Capability: capabilityForActivity(), Note: "the caller's own activity, filtered by current access; never another account's"}, s.listActivity},
		{http.MethodPost, "/api/me/activity/acknowledge", routePolicy{Capability: capabilityForActivity(), Note: "monotonic acknowledgment of the caller's own activity; separate from reading so a failed read cannot consume it"}, s.acknowledgeActivity},
		{http.MethodGet, "/api/me/notification-preferences", routePolicy{Note: "the caller's own private notification preferences, defaulting to the documented quiet defaults"}, s.privateNotificationPreference},
		// Groups are the shared conversation model with kind='group' and membership,
		// not a second message service. Listing is the caller's own groups; inviting
		// and accepting are separate acts, and only the recipient may accept
		// (ADR 0016).
		{http.MethodGet, "/api/groups", routePolicy{Capability: capabilityForActivity(), Note: "groups the caller has accepted; a group they were only invited to is not listed"}, s.listGroups},
		{http.MethodPost, "/api/groups", routePolicy{Capability: capabilityForActivity(), Note: "open a group; the creator is its owner and first member"}, s.createGroup},
		{http.MethodGet, "/api/groups/{id}/members", routePolicy{Capability: capabilityForActivity(), Note: "the membership list of a group the caller has joined, for the three actions ownership governs"}, s.listGroupMembers},
		{http.MethodPost, "/api/groups/{id}/invite", routePolicy{Capability: capabilityForActivity(), Note: "invite one person to a group; owner only, and the invitation names the history rule"}, s.inviteToGroup},
		{http.MethodPost, "/api/groups/{id}/leave", routePolicy{Capability: capabilityForActivity(), Note: "leave a group; an owner must transfer or name a successor in the same act"}, s.leaveGroup},
		{http.MethodPost, "/api/groups/{id}/members/{user}/remove", routePolicy{Capability: capabilityForActivity(), Note: "remove a member; owner only, and what they read before stays theirs"}, s.removeGroupMember},
		{http.MethodPost, "/api/groups/{id}/transfer", routePolicy{Capability: capabilityForActivity(), Note: "move group ownership atomically to an accepted member"}, s.transferGroupOwnership},
		{http.MethodGet, "/api/group-invitations", routePolicy{Capability: capabilityForActivity(), Note: "invitations addressed to the caller, plus the ones their own groups have sent"}, s.listGroupInvitations},
		{http.MethodPost, "/api/group-invitations/{id}/accept", routePolicy{Capability: capabilityForActivity(), Note: "the recipient's act alone; the inviter cannot accept for them"}, s.acceptGroupInvitation},
		{http.MethodPost, "/api/group-invitations/{id}/reject", routePolicy{Capability: capabilityForActivity(), Note: "the recipient declining; recorded rather than deleted"}, s.rejectGroupInvitation},
		{http.MethodPut, "/api/me/notification-preferences", routePolicy{Note: "interruption preferences only; grants no access and changes no history"}, s.setPrivateNotificationPreference},
		{http.MethodGet, "/api/admin/request-reports", routePolicy{Capability: authz.ReportReviewPrivate, Note: "submitted request-introduction evidence only; the conversation is omitted and cannot be opened"}, s.listRequestEvidence},
		{http.MethodPatch, "/api/admin/request-reports/{id}", routePolicy{Capability: authz.ReportReviewPrivate, Note: "nonpunitive triage of one request evidence record"}, s.reviewRequestEvidence},
		{http.MethodGet, "/api/admin/private-reports/audit", routePolicy{Capability: authz.AuditRead, Note: "the restricted audit feed: private evidence review and private group membership, kept out of the community feed and read under the audit capability rather than the evidence one"}, s.privateEvidenceAuditLogs},
		{http.MethodGet, "/api/me/channel-notifications", routePolicy{Note: "own notification preferences filtered by current channel access"}, s.listChannelNotifications},
		{http.MethodPut, "/api/channels/{id}/notifications", routePolicy{Note: "active members change only their own notification choice for a readable channel"}, s.setChannelNotifications},
		{http.MethodPost, "/api/admin/users/{id}/reset-password", routePolicy{Capability: authz.OperatorCredentialRecovery, FreshAuth: true, Note: "chosen-password takeover is operator authority, not ordinary moderation, and needs a recent strong sign-in"}, s.resetPassword},
		{http.MethodDelete, "/api/admin/users/{id}/mfa", routePolicy{Capability: authz.OperatorCredentialRecovery, FreshAuth: true, Note: "removing another account's lost second factor is credential recovery, not moderation, and needs a recent strong sign-in"}, s.removeLostFactor},
		{http.MethodPost, "/api/admin/users/{id}/kick", routePolicy{Capability: authz.MemberManage}, s.kickUser},
		{http.MethodPost, "/api/admin/channels/{id}/purge", routePolicy{Capability: authz.ChannelManage}, s.purgeChannel},
		{http.MethodGet, "/api/admin/settings", routePolicy{Capability: authz.SettingsManage}, s.adminSettings},
		{http.MethodPut, "/api/admin/settings", routePolicy{Capability: authz.SettingsManage, Note: "routine settings only; the privileged second-factor policy needs owner authority and a recent strong sign-in, and the audit log cannot be disabled"}, s.updateSettings},
		{http.MethodGet, "/api/admin/audit-logs", routePolicy{Capability: authz.AuditRead}, s.auditLogs},
		{http.MethodGet, "/api/admin/health", routePolicy{Capability: authz.SettingsManage}, s.health},
		{http.MethodGet, "/api/admin/diagnostics", routePolicy{Capability: authz.SettingsManage}, s.diagnostics},
		{http.MethodPost, "/api/admin/backup", routePolicy{Capability: authz.OperatorBackup, FreshAuth: true, Note: "a full archive can defeat every private boundary, against this server's own administrators too"}, s.createBackup},
		{http.MethodGet, "/api/admin/backups/{id}", routePolicy{Capability: authz.OperatorBackup, FreshAuth: true, Note: "a full archive can defeat every private boundary, against this server's own administrators too"}, s.downloadBackup},
	}
}

// publicRoutes are the only endpoints reachable without a session. Each one has a
// documented reason for being public, and the inventory test enforces the list.
var publicRoutes = map[string]string{
	"GET /api/setup/status":        "an unauthenticated browser must learn whether first-time setup is still required",
	"POST /api/setup":              "creates the first account and requires the local setup token",
	"POST /api/login":              "establishes a session",
	"POST /api/invitations/accept": "accepts a single-use invitation before any session exists",
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, registered := range s.routes() {
		mux.HandleFunc(registered.Method+" "+registered.Path, s.protect(registered.Policy, registered.Handler))
	}
	mux.HandleFunc("GET /api/setup/status", s.setupStatus)
	mux.HandleFunc("POST /api/setup", s.setup)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/invitations/accept", s.acceptInvitation)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { problem(w, http.StatusNotFound, "Endpoint not found.") })
	mux.HandleFunc("/", serveFrontend)
	return s.middleware(mux)
}

func serveFrontend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		problem(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	assets := web.Assets()
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if _, err := fs.Stat(assets, path); err != nil {
		if strings.Contains(path, ".") {
			http.NotFound(w, r)
			return
		}
		path = "index.html"
	}
	if strings.HasPrefix(path, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if path == "service-worker.js" {
		w.Header().Set("Service-Worker-Allowed", "/")
	}
	if path == "index.html" {
		r.URL.Path = "/"
	} else {
		r.URL.Path = "/" + path
	}
	web.ServeAsset(w, r, path)
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}

func problem(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}

func (s *Server) failed(w http.ResponseWriter, err error) {
	s.log.Error("request failed", "error", err)
	problem(w, http.StatusInternalServerError, "The server couldn't complete that request.")
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		problem(w, http.StatusUnsupportedMediaType, "Send JSON with Content-Type: application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		problem(w, http.StatusBadRequest, "Invalid request body.")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		problem(w, http.StatusBadRequest, "Send one JSON object.")
		return false
	}
	return true
}

// configureTURN brings the relay up with the configured transports. A TCP fallback
// that is off means the relay is offered over UDP alone, which is what this shipped
// before the fallback existed and stays the default rather than becoming an error.
//
// The TLS transport is wired but not enabled: a `turns:` URL without a certificate
// would give a client something it cannot connect to, and ICE reports that as a network
// fault rather than a misconfiguration. It is left off until an operator supplies a
// certificate through the settings surface (ADR 0019, §7).
func (s *Server) configureTURN(settings db.Settings) error {
	tcpPort := 0
	if settings.TURNTCPFallback {
		tcpPort = settings.TURNTLSPort
		if tcpPort == 0 {
			// A distinct default rather than the UDP port: two protocols cannot share
			// one listener, and an operator who asked for a fallback without naming a
			// port meant a port that is not the one already in use.
			tcpPort = settings.TURNPort + 1
		}
	}
	return s.turn.ConfigureWithFallback(settings.TURN, settings.TURNExternalIP, settings.TURNPort, tcpPort, 0, settings.TURNAdvertisedHost, nil)
}
