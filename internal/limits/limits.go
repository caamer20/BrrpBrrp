// Package limits holds this installation's own hard bounds, in one place.
//
// Each of these was a literal `100` written beside the behaviour it bounded — the account
// ceiling in two packages, the channel ceiling in two branches of the same handler — which
// is how a product comes to advertise one number and enforce another, or to refuse with a
// message that names a bound it does not actually apply. The check and the message now
// read the same constant, and a test that walks the boundary reads it too.
//
// They are product policy rather than configuration: an operator raises them by shipping a
// different build, because the capacity evidence behind each number belongs to the release
// that states it (see docs/v1-implementation-plan.md, section on capacity).
package limits

const (
	// Accounts is the number of live and recoverable accounts one installation admits.
	// Permanently erased identities are tombstones and do not count.
	Accounts = 100
	// Channels is the number of channels one workspace may hold.
	Channels = 100
	// OutstandingInvitations is how many invitations may be waiting at once.
	OutstandingInvitations = 100
	// PinsPerChannel bounds a channel's pinned messages.
	PinsPerChannel = 100
)
