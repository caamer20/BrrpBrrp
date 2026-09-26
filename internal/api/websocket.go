package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
	"brrpbrrp/internal/ws"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func (s *Server) websocket(w http.ResponseWriter, r *http.Request, user db.User) {
	s.connectionsMu.Lock()
	if s.closing {
		s.connectionsMu.Unlock()
		problem(w, 503, "Server is restarting.")
		return
	}
	s.connections.Add(1)
	s.connectionsMu.Unlock()
	defer s.connections.Done()
	if r.Header.Get("Origin") != s.config.BaseURL {
		problem(w, 403, "Request origin is not allowed.")
		return
	}
	origin, _ := url.Parse(s.config.BaseURL)
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{origin.Host}, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 << 10)
	cookie, _ := r.Cookie(cookieName)
	client := &ws.Client{ID: db.ID(), UserID: user.ID, SessionHash: hashToken(cookie.Value), Conn: conn, Out: make(chan ws.Queued, 64)}
	// Registration is an admission, not a formality: the caller's authority and session
	// are re-established from the store and the connection is inserted into the hub under
	// the authority fence, so a credential, session or authorization change that commits
	// concurrently is either observed by this check or invalidates the connection after it
	// is registered. The authority the connection is admitted under is the starting point
	// for its delivery decisions, so a restricted event resolved before it is not delivered
	// to it as if nothing had changed.
	if _, err := s.registerConnection(r.Context(), user, client); err != nil {
		if errors.Is(err, errTooManyConnections) {
			_ = conn.Close(websocket.StatusPolicyViolation, "Too many connections.")
			return
		}
		// The session, role or account state changed while the connection was being
		// established. The upgrade already happened, so the refusal is the close.
		_ = conn.Close(websocket.StatusPolicyViolation, "Your session is no longer valid.")
		return
	}
	defer func() {
		if _, err := s.authenticate(r.Clone(context.Background())); err != nil || s.ctx.Err() != nil {
			s.leaveVoice(user.ID, client.ID)
		} else {
			s.voice.Detach(user.ID, client.ID)
		}
		s.hub.Remove(client)
		if !s.hub.Online(user.ID) {
			// Account removal clears activity metadata before disconnecting its
			// sockets. Transport cleanup must not restore that removed identity.
			_, _ = s.store.Exec(`UPDATE users SET last_seen_at=? WHERE id=? AND is_deleted=0`, db.Now(), user.ID)
		}
		s.publishPresence()
	}()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-s.ctx.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	lastTouched := time.Now()
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer cancel()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case queued := <-client.Out:
				// Write admission, taken after the queue was drained and immediately
				// before the socket write. It is decided under the authority fence, so it
				// cannot interleave with an authorization change committing: an event
				// queued under authority the connection has since lost, or scoped to an
				// audience resolved before a change that commits while the write is
				// pending, is dropped here rather than sent. The invalidation queued with
				// the change is what tells the client to refetch. The fence is released
				// before the network write, so no byte is serialized behind it.
				if hook := s.writerAdmission.Load(); hook != nil {
					(*hook)(client, queued)
				}
				if !s.admitted(client, queued) {
					continue
				}
				writeCtx, done := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Write(writeCtx, websocket.MessageText, queued.Event)
				done()
				if err != nil {
					return
				}
			case <-ticker.C:
				if _, err := s.authenticate(r); err != nil {
					return
				}
				// Session activity is recorded at a coarse interval: a useful "last
				// seen" without a write per event.
				if time.Since(lastTouched) > 5*time.Minute {
					s.store.TouchSession(ctx, s.store, client.SessionHash)
					lastTouched = time.Now()
				}
				pingCtx, done := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Ping(pingCtx)
				done()
				if err != nil {
					return
				}
				client.Send("presence.update", s.visiblePresence())
			}
		}
	}()
	defer func() { cancel(); <-writerDone }()
	client.Send("hello", map[string]any{"user_id": user.ID, "refetch": true, "presence": s.visiblePresence()})
	s.publishPresence()
	for {
		var event ws.Event
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			return
		}
		if !s.limiter.allow("ws:"+user.ID, 600, time.Minute) {
			client.Send("error", map[string]string{"error": "Too many events. Slow down."})
			continue
		}
		current, err := s.authenticate(r)
		if err != nil {
			return
		}
		s.handleEvent(ctx, r, client, current, event)
	}
}

func (s *Server) visiblePresence() map[string]string {
	if !s.Settings().Presence {
		return map[string]string{}
	}
	return s.hub.Presence()
}

func (s *Server) publishPresence() {
	if s.Settings().Presence {
		s.hub.Broadcast("presence.update", s.hub.Presence())
	}
}

func (s *Server) handleEvent(ctx context.Context, r *http.Request, client *ws.Client, user db.User, event ws.Event) {
	switch event.Type {
	case "voice.join", "voice.move", "voice.resume", "voice.leave", "voice.state", "voice.speaking", "rtc.signal", "admin.voice_kick", "admin.voice_mute", "admin.voice_deafen":
		s.voiceEvent(ctx, client, user, event)
	case "hello":
		client.Send("reconnect.required", map[string]bool{"refetch": true})
	case "activity":
		s.hub.Activity(client)
	case "typing.start", "typing.stop":
		if !s.Settings().Typing {
			return
		}
		var input struct {
			ChannelID string `json:"channel_id"`
		}
		if json.Unmarshal(event.Data, &input) != nil {
			return
		}
		// Authority is re-resolved for the event, not inherited from page load.
		principal, err := s.resolvePrincipal(ctx, s.store, user)
		if err != nil {
			return
		}
		channel, _, readable, err := s.channelAccess(ctx, principal, input.ChannelID)
		if err != nil || !readable || channel.Type != "text" {
			return
		}
		if event.Type == "typing.start" && !s.limiter.allow("typing:"+user.ID+":"+channel.ID, 1, 3*time.Second) {
			return
		}
		// Typing state is channel metadata, so it is delivered with the revision the
		// eligibility answer was computed at. The audience and that revision come from
		// one snapshot, so a revocation cannot be straddled by the two reads.
		eligible, revision, err := s.coherentAudience(ctx, channel.ID)
		if err != nil {
			return
		}
		if committed := s.authorizationRevision(ctx); committed != revision {
			return
		}
		s.hub.BroadcastToRevision(eligible, revision, "typing.update", map[string]any{"channel_id": channel.ID, "user_id": user.ID, "typing": event.Type == "typing.start", "expires_at": time.Now().Add(5 * time.Second).UnixMilli()})
	default:
		s.dispatchSocketMutation(ctx, r, client, user, event)
	}
}

func (s *Server) dispatchSocketMutation(ctx context.Context, r *http.Request, client *ws.Client, user db.User, event ws.Event) {
	var input map[string]json.RawMessage
	if json.Unmarshal(event.Data, &input) != nil {
		client.Send("error", map[string]string{"error": "Invalid event data."})
		return
	}
	var handler authenticatedHandler
	var idKey string
	var capability authz.Capability
	switch event.Type {
	case "message.send":
		handler, idKey, capability = s.createMessage, "channel_id", authz.MessageSend
	case "message.edit":
		handler, idKey = s.editMessage, "message_id"
	case "message.delete":
		handler, idKey = s.deleteMessage, "message_id"
	case "reaction.toggle":
		handler, idKey, capability = s.toggleReaction, "message_id", authz.MessageReact
	case "channel.read":
		handler, idKey = s.markRead, "channel_id"
	default:
		client.Send("error", map[string]string{"error": "Unknown event."})
		return
	}
	// A socket mutation is subject to the same route policy as its HTTP
	// equivalent. Reusing the handler without the policy would be an open door.
	principal, err := s.resolvePrincipal(ctx, s.store, user)
	if err != nil {
		client.Send("error", map[string]string{"error": "Your session could not be verified."})
		return
	}
	if capability != "" && !principal.Allowed(capability) {
		client.Send("ack", map[string]any{"event": event.Type, "status": http.StatusForbidden, "result": map[string]string{"error": "Your role does not include " + string(capability) + "."}})
		return
	}
	r = withPrincipal(r, principal)
	var id string
	if json.Unmarshal(input[idKey], &id) != nil {
		return
	}
	delete(input, idKey)
	data, _ := json.Marshal(input)
	request := r.Clone(ctx)
	request.Method = http.MethodPost
	request.Body = io.NopCloser(bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", id)
	response := &socketResponse{header: make(http.Header), status: http.StatusOK}
	handler(response, request, user)
	client.Send("ack", map[string]any{"event": event.Type, "status": response.status, "result": json.RawMessage(response.body.Bytes())})
}

// publishMessage announces a message only to accounts that may read its channel.
// The payload carries identifiers, never content, and never reaches an account
// without current access.
func (s *Server) publishMessage(kind string, message db.Message) {
	s.publishToChannel(context.Background(), message.ChannelID, kind, map[string]string{"id": message.ID, "channel_id": message.ChannelID, "reply_to_id": message.ReplyToID})
}

func (s *Server) Close() {
	s.connectionsMu.Lock()
	s.closing = true
	s.connectionsMu.Unlock()
	s.cancel()
	s.voice.Close()
	s.turn.Close()
	s.hub.Disconnect("", "")
	s.connections.Wait()
	s.workers.Wait()
}

// Socket commands reuse the HTTP mutation handlers and capture their JSON result.
type socketResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *socketResponse) Header() http.Header            { return r.header }
func (r *socketResponse) WriteHeader(status int)         { r.status = status }
func (r *socketResponse) Write(data []byte) (int, error) { return r.body.Write(data) }
