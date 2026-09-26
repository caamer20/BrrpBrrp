package api

import (
	"context"
	"io"
	"sync"
)

// deliveries tracks the responses that are still streaming protected data, so a
// revocation can end them.
//
// Resolving access when a request arrives is not enough on its own: a slow client
// downloading a file keeps receiving bytes after the grant that admitted it has been
// withdrawn, and the media path has no request to re-check. Metadata responses are not
// registered here — they finish inside the request that authorized them — while file
// and preview bodies, which can run for seconds, are. The gap the plan names is
// T-AUTH-02: no response may outlive the access it was admitted under.
//
// A registered response carries the account and the session that admitted it, so a
// role change, an account-wide change or a single revoked session ends exactly the
// streams it should and leaves unrelated sessions working. Bytes already written
// cannot be recalled: the boundary is the stream, and it stops at the next read.
type delivery struct {
	id          int
	channelID   string
	userID      string
	sessionHash string
	cancel      context.CancelFunc
}

type deliveries struct {
	mu     sync.Mutex
	next   int
	active map[int]delivery
}

// begin registers one streamed response and returns the context that ends it. The
// returned function unregisters it; call it when the handler ends.
//
// Registration happens before the request resolves access, so the window between the
// authorization decision and the first byte is inside the registry: whatever is
// revoked after this point is cancelled here rather than admitted, and a check that
// runs after registration sees a revocation that already committed.
func (d *deliveries) begin(channelID, userID, sessionHash string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	d.mu.Lock()
	if d.active == nil {
		d.active = make(map[int]delivery)
	}
	d.next++
	record := delivery{id: d.next, channelID: channelID, userID: userID, sessionHash: sessionHash, cancel: cancel}
	d.active[record.id] = record
	d.mu.Unlock()
	return ctx, func() {
		d.mu.Lock()
		delete(d.active, record.id)
		d.mu.Unlock()
		cancel()
	}
}

// snapshot copies the responses that are registered now. It exists for the revocation
// paths whose decision needs a database read: deciding inside end would hold the
// registry lock across queries and block every new registration for that time.
func (d *deliveries) snapshot() []delivery {
	d.mu.Lock()
	defer d.mu.Unlock()
	records := make([]delivery, 0, len(d.active))
	for _, record := range d.active {
		records = append(records, record)
	}
	return records
}

// endIDs ends the listed responses and reports how many it ended. A response that was
// already ended or unregistered in between is skipped, so a decision made from a
// snapshot can never cancel something that has been replaced.
func (d *deliveries) endIDs(ids []int) int {
	d.mu.Lock()
	ended := make([]delivery, 0, len(ids))
	for _, id := range ids {
		if record, ok := d.active[id]; ok {
			delete(d.active, id)
			ended = append(ended, record)
		}
	}
	d.mu.Unlock()
	for _, record := range ended {
		record.cancel()
	}
	return len(ended)
}

// revoke ends every response still streaming one channel's data and reports how many
// it ended. It is called on the same request that changes a channel's access, so the
// boundary is the commit rather than the client's next request.
func (d *deliveries) revoke(channelID string) int {
	return d.end(func(record delivery) bool { return record.channelID == channelID })
}

// revokeAccount ends every response admitted for one account. A role, a ban or a
// removed factor can affect any number of channels, and the affected channels are not
// known at that point, so all of the account's streams stop.
func (d *deliveries) revokeAccount(userID string) int {
	return d.end(func(record delivery) bool { return record.userID == userID })
}

// revokeSession ends every response admitted for one session, which is what signing a
// single device out, changing a password or removing a factor requires. Other
// sessions of the same account keep working.
func (d *deliveries) revokeSession(sessionHash string) int {
	if sessionHash == "" {
		return 0
	}
	return d.end(func(record delivery) bool { return record.sessionHash == sessionHash })
}

// revokeAccountExceptSession ends an account's streams apart from the ones its
// surviving session admitted, which is what signing the other devices out needs.
func (d *deliveries) revokeAccountExceptSession(userID, sessionHash string) int {
	return d.end(func(record delivery) bool {
		return record.userID == userID && (sessionHash == "" || record.sessionHash != sessionHash)
	})
}

// revokeAll ends every streamed response.
func (d *deliveries) revokeAll() int {
	return d.end(func(delivery) bool { return true })
}

// end cancels every registered response that matches and reports how many it ended.
func (d *deliveries) end(match func(delivery) bool) int {
	d.mu.Lock()
	ended := make([]delivery, 0, len(d.active))
	for id, record := range d.active {
		if match(record) {
			delete(d.active, id)
			ended = append(ended, record)
		}
	}
	d.mu.Unlock()
	for _, record := range ended {
		record.cancel()
	}
	return len(ended)
}

// revocableReader stops a response the moment the access that admitted it changes.
// http.ServeContent copies through this reader, so the copy ends while the client is
// still reading slowly instead of after it has received the whole resource.
type revocableReader struct {
	ctx    context.Context
	reader io.ReadSeeker
}

// Seek is delegated unchanged so range requests still resolve to the right offset;
// every Read then re-checks whether the response is still allowed.
func (r revocableReader) Seek(offset int64, whence int) (int64, error) {
	return r.reader.Seek(offset, whence)
}

func (r revocableReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		// Ending the copy short is deliberate: the server closes the connection and
		// the client sees an incomplete download rather than the file it lost the
		// right to read.
		return 0, err
	}
	return r.reader.Read(p)
}
