// Package ws carries the shape of the websocket conversation between a client and this
// server: the envelopes, the events, and the protocol number that says whether the two
// can understand each other.
package ws

// Protocol identifies the shape of the conversation, not the release. It is raised when a
// frame's *meaning* changes in a way an older client would misread — a field that changes
// type, an event whose payload an old client would act on wrongly, an order that becomes
// load-bearing. Adding a field is not a protocol change: an old client ignores what it
// does not know, which is why the number moves rarely and deliberately.
//
// What it protects is drafts. A tab that has been open across an update speaks the older
// protocol, and the failure this exists to prevent is that tab sending text the server
// would store differently, or holding text in a composer while the server refuses it and
// nothing says why. `hello` carries this number and the lowest one the server still
// accepts; a client outside the range is told to update and keeps its text.
const (
	// Protocol is what this build speaks.
	Protocol = 1
	// ProtocolMin is the oldest conversation this server can answer without guessing.
	ProtocolMin = 1
)
