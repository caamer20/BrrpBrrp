package api

import (
	"context"
	"crypto/elliptic"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/scraper"
	webpush "github.com/SherClockHolmes/webpush-go"
)

type pushJob struct {
	userID string
	// channelID is the conversation the notice refers to. Dispatch re-checks it,
	// so a queued job cannot disclose activity the recipient lost access to
	// while it waited in the backlog.
	channelID string
	payload   map[string]string
}

func (s *Server) pushDevices(w http.ResponseWriter, r *http.Request, user db.User) {
	rows, err := s.store.QueryContext(r.Context(), `SELECT endpoint FROM push_subscriptions WHERE user_id=?`, user.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer rows.Close()
	hashes := []string{}
	for rows.Next() {
		var endpoint string
		if err := rows.Scan(&endpoint); err != nil {
			s.failed(w, err)
			return
		}
		hashes = append(hashes, hashToken(endpoint))
	}
	if err := rows.Err(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, map[string]any{"endpoint_hashes": hashes})
}

func (s *Server) preparePush() error {
	var value string
	err := s.store.QueryRow(`SELECT value FROM settings WHERE key='vapid'`).Scan(&value)
	if err == nil {
		var keys struct{ Private, Public string }
		if err := json.Unmarshal([]byte(value), &keys); err != nil {
			return err
		}
		s.vapidPrivate, s.vapidPublic = keys.Private, keys.Public
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return err
	}
	encoded, _ := json.Marshal(struct{ Private, Public string }{private, public})
	if _, err := s.store.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('vapid',?,?)`, string(encoded), db.Now()); err != nil {
		return err
	}
	s.vapidPrivate, s.vapidPublic = private, public
	return nil
}

func (s *Server) subscribePush(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.Settings().Push {
		problem(w, 409, "Push notifications are disabled.")
		return
	}
	var subscription webpush.Subscription
	if !decode(w, r, &subscription) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	key, keyErr := base64.RawURLEncoding.DecodeString(subscription.Keys.P256dh)
	auth, authErr := base64.RawURLEncoding.DecodeString(subscription.Keys.Auth)
	validKey := false
	if keyErr == nil && len(key) == 65 {
		x, _ := elliptic.Unmarshal(elliptic.P256(), key)
		validKey = x != nil
	}
	if len(subscription.Endpoint) > 2048 || !validKey || authErr != nil || len(auth) != 16 || !scraper.PublicURL(ctx, subscription.Endpoint) {
		problem(w, 400, "Invalid push subscription.")
		return
	}
	// Endpoint validation can wait for DNS after the route guard has admitted
	// the request. Re-establish the session in the transaction that records the
	// device, so a revoked session cannot recreate a subscription after cleanup.
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := s.authorizeInTx(r.Context(), tx, user); err != nil {
		s.authorityFailed(w, err)
		return
	}
	var count int
	if err := tx.QueryRowContext(r.Context(), `SELECT count(*) FROM push_subscriptions WHERE user_id=? AND endpoint<>?`, user.ID, subscription.Endpoint).Scan(&count); err != nil {
		s.failed(w, err)
		return
	}
	if count >= 5 {
		problem(w, 409, "You can register up to five notification devices.")
		return
	}
	result, err := tx.ExecContext(r.Context(), `INSERT INTO push_subscriptions(id,user_id,endpoint,p256dh,auth,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(endpoint) DO UPDATE SET p256dh=excluded.p256dh,auth=excluded.auth WHERE user_id=excluded.user_id`, db.ID(), user.ID, subscription.Endpoint, subscription.Keys.P256dh, subscription.Keys.Auth, db.Now())
	if err != nil {
		s.failed(w, err)
		return
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		problem(w, 409, "That device belongs to another account. Unsubscribe it first.")
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 201, map[string]bool{"ok": true})
}

func (s *Server) unsubscribePush(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Endpoint string `json:"endpoint"`
	}
	if !decode(w, r, &input) {
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := s.authorizeInTx(r.Context(), tx, user); err != nil {
		s.authorityFailed(w, err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM push_subscriptions WHERE user_id=? AND endpoint=?`, user.ID, input.Endpoint); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) pushWorker() {
	defer s.workers.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case job := <-s.pushJobs:
			if !s.Settings().Push {
				continue
			}
			// A queued notice cannot disclose activity the recipient lost access to
			// while it waited in the backlog: the decision is made here, at dispatch.
			if job.channelID != "" && !s.mayNotifyChannel(s.ctx, job.userID, job.channelID) {
				continue
			}
			client := s.pushHTTP
			subscriptions, err := s.pushSubscriptions(job.userID)
			if err != nil {
				continue
			}
			payload, _ := json.Marshal(job.payload)
			for _, subscription := range subscriptions {
				// A slow device must not let a later device reuse the access or mute
				// decision made before that request. Already admitted network writes
				// cannot be recalled; every new device dispatch checks current state.
				if job.channelID != "" && !s.mayNotifyChannel(s.ctx, job.userID, job.channelID) {
					break
				}
				response, err := webpush.SendNotificationWithContext(s.ctx, payload, &subscription, &webpush.Options{HTTPClient: client, Subscriber: s.config.BaseURL, VAPIDPublicKey: s.vapidPublic, VAPIDPrivateKey: s.vapidPrivate, TTL: 300, Urgency: webpush.UrgencyNormal})
				if err != nil {
					continue
				}
				_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
				response.Body.Close()
				if response.StatusCode == 404 || response.StatusCode == 410 {
					_, _ = s.store.Exec(`DELETE FROM push_subscriptions WHERE user_id=? AND endpoint=?`, job.userID, subscription.Endpoint)
				}
			}
		}
	}
}

func (s *Server) pushSubscriptions(userID string) ([]webpush.Subscription, error) {
	rows, err := s.store.Query(`SELECT endpoint,p256dh,auth FROM push_subscriptions WHERE user_id=? AND EXISTS(SELECT 1 FROM users WHERE id=? AND is_banned=0 AND is_deactivated=0 AND is_deleted=0)`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	subscriptions := make([]webpush.Subscription, 0)
	for rows.Next() {
		var subscription webpush.Subscription
		if err := rows.Scan(&subscription.Endpoint, &subscription.Keys.P256dh, &subscription.Keys.Auth); err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}
	return subscriptions, rows.Err()
}
