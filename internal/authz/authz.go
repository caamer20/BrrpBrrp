// Package authz is the single place that answers "may this account do this to
// this resource". Handlers must ask it rather than inspecting users.is_admin,
// otherwise a private-channel decision can drift between delivery surfaces.
//
// The v1 model is deliberately bounded: additive roles plus explicit channel
// access gates. There is no inherited allow/deny override algebra, and no
// administrator silently gains access to another account's private
// conversations.
package authz

import (
	"errors"
	"fmt"
	"sort"

	"brrpbrrp/internal/db"
)

// Capability is a named action grant.
type Capability string

const (
	// Resource capabilities describe what an account may do with community
	// content at all. Access to one restricted channel is a separate grant.
	ChannelRead      Capability = "channel.read"
	MessageSend      Capability = "message.send"
	MessageReact     Capability = "message.react"
	MessageUpload    Capability = "message.upload"
	MessagePin       Capability = "message.pin"
	MessageDeleteAny Capability = "message.delete_any"
	MentionEveryone  Capability = "mention.everyone"

	VoiceConnect     Capability = "voice.connect"
	VoiceSpeak       Capability = "voice.speak"
	VoiceShareScreen Capability = "voice.share_screen"
	VoiceModerate    Capability = "voice.moderate"

	MemberManage   Capability = "member.manage"
	ChannelManage  Capability = "channel.manage"
	CategoryManage Capability = "category.manage"
	RoleManage     Capability = "role.manage"
	InviteManage   Capability = "invite.manage"
	SettingsManage Capability = "settings.manage"
	AuditRead      Capability = "audit.read"

	// ReportReviewPrivate authorizes reading and reviewing the private message
	// evidence an account has voluntarily submitted about one accepted direct
	// message. It is separate from MessageDeleteAny on purpose: a community
	// moderator's grant is scoped to channels they can currently read, which is
	// never true of a direct conversation. Holding this capability grants the
	// submitted snapshot and nothing else — no membership, no history, no search,
	// no files — so it is delegated explicitly rather than inherited by every
	// reviewer. Owners hold it through the ordinary owner rule (ADR 0013).
	ReportReviewPrivate Capability = "report.review_private"

	// The operator tier is not delegable through ordinary custom roles. These
	// powers can defeat a private-conversation boundary (whole-server export,
	// password takeover), so they require owner authority and a recent
	// authentication, and they are audited when exercised.
	OperatorBackup             Capability = "operator.backup"
	OperatorCredentialRecovery Capability = "operator.credential_recovery"
	OperatorOwnershipTransfer  Capability = "operator.ownership_transfer"
)

// Catalog is every capability this build understands. Tests assert that no
// route, event or stored role references a capability outside it.
var Catalog = []Capability{
	ChannelRead, MessageSend, MessageReact, MessageUpload, MessagePin, MessageDeleteAny, MentionEveryone,
	VoiceConnect, VoiceSpeak, VoiceShareScreen, VoiceModerate,
	MemberManage, ChannelManage, CategoryManage, RoleManage, InviteManage, SettingsManage, AuditRead,
	ReportReviewPrivate,
	OperatorBackup, OperatorCredentialRecovery, OperatorOwnershipTransfer,
}

var catalogSet = func() map[Capability]bool {
	set := make(map[Capability]bool, len(Catalog))
	for _, capability := range Catalog {
		set[capability] = true
	}
	return set
}()

var operatorTier = map[Capability]bool{
	OperatorBackup:             true,
	OperatorCredentialRecovery: true,
	OperatorOwnershipTransfer:  true,
}

// Valid reports whether a capability belongs to this build's catalog.
func Valid(capability Capability) bool { return catalogSet[capability] }

// Capabilities converts stored capability names into the typed catalog.
func Capabilities(names []string) []Capability {
	capabilities := make([]Capability, 0, len(names))
	for _, name := range names {
		capabilities = append(capabilities, Capability(name))
	}
	return capabilities
}

// IsOperator reports whether a capability is part of the non-delegable operator
// tier.
func IsOperator(capability Capability) bool { return operatorTier[capability] }

// OwnerPosition is the authority level of the installation owner. Owners hold
// every capability, including the operator tier.
const OwnerPosition = 100

// AdministrativePosition is the position of the built-in administrator role.
const AdministrativePosition = 80

// Role is a resolved grant. Permissions are a set for cheap union evaluation.
type Role struct {
	ID          string
	Key         string
	Name        string
	Position    int
	IsDefault   bool
	IsSystem    bool
	Permissions map[Capability]bool
}

// RoleFrom converts a stored role, ignoring capability names this build does not
// know so an older binary cannot be tricked into honouring a future grant.
func RoleFrom(role db.Role) Role {
	resolved := Role{ID: role.ID, Key: role.Key, Name: role.Name, Position: role.Position, IsDefault: role.IsDefault, IsSystem: role.IsSystem, Permissions: map[Capability]bool{}}
	for _, name := range role.Permissions {
		capability := Capability(name)
		if Valid(capability) {
			resolved.Permissions[capability] = true
		}
	}
	return resolved
}

// Principal is an authenticated account with its authority resolved at a known
// authorization revision.
type Principal struct {
	UserID  string
	IsOwner bool
	Roles   []Role
	// OwnerDesignated reports whether an owner exists right now. When it is
	// false the server reports the missing ownership ceremony as a setup task.
	OwnerDesignated bool
	// OwnershipEstablished is the durable record that the ownership ceremony
	// completed at least once. The bootstrap bridge below applies only while it
	// is false, so removing an owner record can never recreate installation
	// privileges.
	OwnershipEstablished bool
	// AdminBridge mirrors the legacy users.is_admin column. It only ever grants
	// the administrator role's capabilities, never the operator tier.
	AdminBridge bool
	// Inactive is set when the account is banned or deactivated in the state the
	// policy was resolved from. An inactive account holds nothing, so a request
	// that was authenticated before the account changed cannot commit.
	//
	// This is *only* a ban or a deactivation. A moderation timeout is not one of
	// them, and folding it in here is what made a community timeout behave as an
	// installation ban: the account could not sign in, could not read its own
	// private history, and could not send a direct message. A timeout is a
	// community decision and its scope is the community.
	Inactive bool
	// Suspended is set while a moderation timeout is live. The session is valid and
	// the account is still signed in; it may not participate in the community, and
	// everything outside the community is untouched.
	//
	// It is a separate flag rather than a facet of Inactive because the two are
	// answered differently on purpose: a ban ends the session and gets 401, a
	// timeout keeps it and gets 403. One flag cannot express both, and every
	// consumer that cared had to ask the timeout question again for itself.
	Suspended bool
	Revision  int64
}

// Position is the highest authority level the principal holds.
func (p Principal) Position() int {
	if p.IsOwner {
		return OwnerPosition
	}
	highest := 0
	for _, role := range p.Roles {
		if role.Position > highest {
			highest = role.Position
		}
	}
	return highest
}

// Allowed reports whether the principal holds a capability.
func (p Principal) Allowed(capability Capability) bool {
	if !Valid(capability) || p.Inactive {
		return false
	}
	if p.IsOwner {
		return true
	}
	if IsOperator(capability) {
		// An installation that has never completed the ownership ceremony keeps
		// its working administrators exactly as they were before roles existed.
		// This is a compatibility bridge, not a delegation path: it is on only
		// while OwnershipEstablished is false, so it cannot be re-entered by
		// removing an owner.
		//
		// Completing the ceremony is the one exception, and it grants nothing by
		// itself: an installation with no owner needs a way back, and the ceremony
		// is explicit, audited, needs a recent strong sign-in, and creates an owner
		// instead of handing out an operator power to whoever happens to be signed
		// in.
		if !p.OwnerDesignated && p.AdminBridge && capability == OperatorOwnershipTransfer {
			return true
		}
		return !p.OwnershipEstablished && p.AdminBridge
	}
	for _, role := range p.Roles {
		if role.Permissions[capability] {
			return true
		}
	}
	return false
}

// Capabilities lists the principal's effective grants for the client.
func (p Principal) Capabilities() []Capability {
	granted := make([]Capability, 0, len(Catalog))
	for _, capability := range Catalog {
		if p.Allowed(capability) {
			granted = append(granted, capability)
		}
	}
	sort.Slice(granted, func(i, j int) bool { return granted[i] < granted[j] })
	return granted
}

// RoleIDs returns the principal's assigned role identifiers.
func (p Principal) RoleIDs() []string {
	ids := make([]string, 0, len(p.Roles))
	for _, role := range p.Roles {
		ids = append(ids, role.ID)
	}
	return ids
}

// MayReadChannel decides whether community channel content, metadata or counts
// may be returned to the principal.
//
// Rules, in order:
//  1. The account must hold channel.read at all.
//  2. A public channel is readable by any such account.
//  3. A community administrator may manage and view community channels.
//  4. Otherwise an explicit member grant or a grant through an assigned role is
//     required.
func (p Principal) MayReadChannel(channel db.Channel, access db.ChannelAccess) bool {
	if !p.Allowed(ChannelRead) {
		return false
	}
	if channel.Visibility != "private" {
		return true
	}
	if p.Allowed(ChannelManage) {
		return true
	}
	for _, userID := range access.Users {
		if userID == p.UserID {
			return true
		}
	}
	for _, roleID := range access.Roles {
		for _, role := range p.Roles {
			if role.ID == roleID {
				return true
			}
		}
	}
	return false
}

// MayPostChannel decides whether the principal may send an ordinary message to a
// channel. An announcement channel restricts ordinary sends to accounts that may
// manage the channel.
func (p Principal) MayPostChannel(channel db.Channel, access db.ChannelAccess) bool {
	if !p.MayReadChannel(channel, access) || !p.Allowed(MessageSend) {
		return false
	}
	if channel.Posting == "announcement" {
		return p.Allowed(ChannelManage)
	}
	return true
}

// CanActOnAccount reports whether the principal may change another account's
// authority, status or existence. targetPosition is the highest authority level
// the target holds, and targetOwner reports whether the target is the designated
// installation owner.
//
// Ownership is never changed by account management: it moves only through the
// ownership ceremony, which is why a designated owner cannot be edited, banned,
// deactivated or deleted from here at all. Everything else follows the same
// "authority moves downward only" rule as role assignment, so a delegated member
// manager can act on ordinary members and moderators but never on a peer, a
// superior or itself.
func (p Principal) CanActOnAccount(targetOwner bool, targetPosition int) bool {
	if p.Inactive || !p.Allowed(MemberManage) {
		return false
	}
	if targetOwner {
		return false
	}
	if p.IsOwner || p.IsAdministrator() {
		return true
	}
	return p.Position() > targetPosition
}

// IsAdministrator reports whether the principal holds the built-in administrator
// role, which is mirrored by the legacy users.is_admin column. Administrators
// manage each other within the last-administrator rule; an account whose only
// privileged grant is a delegated capability such as member.manage does not, and
// can therefore never reach an account that holds administrator authority.
func (p Principal) IsAdministrator() bool {
	if p.AdminBridge {
		return true
	}
	for _, role := range p.Roles {
		if role.IsSystem && role.Key == "admin" {
			return true
		}
	}
	return false
}

// CanActOnRole reports whether the principal may edit, grant or revoke a role.
// An actor never outranks itself or a peer, cannot touch the undeletable default
// role, and cannot reach the implicit owner level without being the owner.
func (p Principal) CanActOnRole(role Role) bool {
	if !p.Allowed(RoleManage) {
		return false
	}
	if p.IsOwner {
		return true
	}
	return p.Position() > role.Position
}

// CanDeleteRole reports whether a role may be removed. The default role is not
// deletable, built-in roles are protected, and a role holding members cannot be
// deleted out from under them.
func (p Principal) CanDeleteRole(role Role, members int) bool {
	if role.IsDefault || role.IsSystem {
		return false
	}
	if members > 0 {
		return false
	}
	return p.CanActOnRole(role)
}

// CanGrantCapabilities reports whether the principal may hand out a permission
// set. Authority can only be delegated downward: an actor may grant what it
// holds, never the operator tier unless it is the owner.
func (p Principal) CanGrantCapabilities(capabilities []Capability) error {
	for _, capability := range capabilities {
		if !Valid(capability) {
			return fmt.Errorf("%q is not a capability this server knows", capability)
		}
		if IsOperator(capability) {
			if !p.IsOwner {
				return fmt.Errorf("%s is part of the non-delegable operator tier", capability)
			}
			continue
		}
		if !p.Allowed(capability) {
			return fmt.Errorf("you cannot grant %s because you do not hold it", capability)
		}
	}
	return nil
}

// Denial classifies a failed check so the caller returns a non-disclosing result
// for resources the principal may not know about, and a clear refusal for an
// action on a resource it can already see.
type Denial struct {
	// Hidden marks a resource whose existence must not be confirmed.
	Hidden bool
	Reason string
}

// ErrUnavailable is returned for hidden resources; callers map it to the same
// response a missing resource produces.
var ErrUnavailable = errors.New("resource unavailable")

// ErrForbidden is returned for a denied action on a visible resource.
var ErrForbidden = errors.New("not permitted")

// ErrSuspended is returned while an account is inside a moderation timeout. It is
// distinct from ErrSessionEnded: the session is still valid and the account is still
// signed in, it simply may not act until the timeout expires. Answering 401 would tell
// somebody they have been signed out and send them through a sign-in they do not need.
var ErrSuspended = errors.New("account suspended")

// ErrSessionEnded is returned when the session or the account that made a
// request stopped being valid while the request was being authorized. It maps to
// a 401, not a 403: the caller is not merely refused, it is no longer signed in.
var ErrSessionEnded = errors.New("session ended")

func (d Denial) Error() string { return d.Reason }

// Unwrap lets errors.Is classify a denial.
func (d Denial) Unwrap() error {
	if d.Hidden {
		return ErrUnavailable
	}
	return ErrForbidden
}
