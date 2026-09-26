package db

import "context"

// Receipt is the durable outcome of one send operation: what the account asked to do, where
// it was committed and whether the content still exists. It is keyed by the account and the
// operation key — the identity the message table already uses — and the destination travels
// in the intent digest, so reusing a key for another conversation or other content is a
// conflict rather than a silent replay of unrelated work (ADR 0010, D-11).
//
// The receipt is deliberately independent of the message row: it outlives deletion and
// purge, so a lost-acknowledgement retry can never recreate content the server has erased.
// It carries identifiers, a digest and a state, never a message body.
type Receipt struct {
	AccountID       string `json:"-"`
	Key             string `json:"key"`
	ConversationID  string `json:"conversation_id"`
	IntentDigest    string `json:"intent_digest"`
	State           string `json:"state"`
	ResultMessageID string `json:"result_message_id,omitempty"`
	CreatedAt       string `json:"created_at"`
	ExpiresAt       string `json:"expires_at"`
}

// Deleted reports that the receipt's content no longer exists. It is the outcome a retry must
// never turn back into a message.
func (r Receipt) Deleted() bool { return r.ResultMessageID == "" }

// Receipt reads one operation's outcome. sql.ErrNoRows means no receipt exists, which is the
// only state in which the operation may be performed as new work. Compaction never releases
// a used key: arbitrary legacy keys cannot prove that a retry is inside its original window.
func (s *Store) Receipt(ctx context.Context, q Queryer, accountID, key string) (Receipt, error) {
	var receipt Receipt
	err := q.QueryRowContext(ctx, `SELECT account_id,idempotency_key,conversation_id,intent_digest,state,COALESCE(result_message_id,''),created_at,expires_at FROM message_operations WHERE account_id=? AND idempotency_key=?`, accountID, key).
		Scan(&receipt.AccountID, &receipt.Key, &receipt.ConversationID, &receipt.IntentDigest, &receipt.State, &receipt.ResultMessageID, &receipt.CreatedAt, &receipt.ExpiresAt)
	return receipt, err
}

// BeginOperation admits one operation and records its identity in the caller's transaction. It
// is written before the message it will acknowledge, so the operation key is claimed exactly
// once even when two requests race: the loser's insert violates the primary key and answers
// the replay instead of creating a second message.
func (s *Store) BeginOperation(ctx context.Context, q Queryer, receipt Receipt) error {
	_, err := q.ExecContext(ctx, `INSERT INTO message_operations(account_id,idempotency_key,conversation_id,intent_digest,state,result_message_id,created_at,expires_at) VALUES(?,?,?,?,'committed',NULL,?,?)`,
		receipt.AccountID, receipt.Key, receipt.ConversationID, receipt.IntentDigest, receipt.CreatedAt, receipt.ExpiresAt)
	return err
}

// CompleteOperation attaches the committed message to the operation it belongs to. The write
// is in the same transaction as the message insert, so a committed message always has its
// receipt and a rolled-back send has neither.
func (s *Store) CompleteOperation(ctx context.Context, q Queryer, accountID, key, messageID string) error {
	_, err := q.ExecContext(ctx, `UPDATE message_operations SET result_message_id=? WHERE account_id=? AND idempotency_key=?`, messageID, accountID, key)
	return err
}

// PruneSendReceipts compacts old deleted receipts in bounded batches. Keep the account,
// operation key and expired outcome until account deletion; remove destination, intent and
// timestamps. An offline or modified client can retry beyond its own 24-hour timer, so
// removing the final guard would recreate content after deletion. Live receipts are intact.
func (s *Store) PruneSendReceipts(ctx context.Context, q Queryer, before string) (int64, error) {
	result, err := q.ExecContext(ctx, `UPDATE message_operations
		SET conversation_id='',intent_digest='',state='deleted',created_at='',expires_at=''
		WHERE rowid IN (SELECT rowid FROM message_operations
			WHERE result_message_id IS NULL AND created_at<>'' AND created_at<?
			ORDER BY created_at LIMIT 1000)`, before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
