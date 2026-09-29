package api

import "strings"

// Restricted audit classification.
//
// Every private-evidence review writes an audit record naming the reviewer, the
// evidence and the time. An account that can read the community audit log but not
// `report.review_private` must not learn any of that: the existence of the record
// would disclose that a specific person reported a specific private message or
// request, which is the very thing the evidence policy withholds (ADR 0013/0014).
//
// Three independent consumers have to agree on which events are restricted — the
// writers that emit them, the two readers that filter them, and the maintenance
// pass that reclaims them on the restricted clock. They drifted apart twice. A
// request review was written as `private_request.*` while every other consumer
// recognised only `private_report.*`, so the event reached the community feed and
// was reclaimed on the 90-day clock. The OR chains below were then emitted
// unparenthesized, so `created_at<? AND a LIKE x OR a LIKE y` deleted every
// restricted row regardless of age. The classification therefore lives here, once,
// and every consumer derives from it — including the parentheses.
//
// Prefixes are matched by SQL LIKE, so the trailing dot is load-bearing: it keeps
// `private_report.` from also matching a future `private_reporting.*`.
var restrictedAuditPrefixes = []string{
	"private_report.",  // private message evidence: submission and review
	"private_request.", // pending-request evidence: submission and review
	"request_evidence.", // retained for rows written before the `private_` prefixes
	// were adopted; see the compatibility note in migration 14's documentation
}

// restrictedAuditLike selects restricted rows.
//
// The parentheses are load-bearing, not decoration. `AND` binds more tightly than
// `OR` in SQL, so an unparenthesized chain spliced into
// `WHERE created_at<? AND <this>` parses as
// `created_at<? AND first OR second OR third`, which matches every restricted row
// regardless of the age condition and so reclaims a 400-day record on the first
// maintenance pass after it is written. Callers may not strip them.
func restrictedAuditLike(column string) string {
	return "(" + restrictedAuditClauses(column, " OR ", false) + ")"
}

// restrictedAuditNotLike is its complement, for the community feed. Filtering
// before the limit matters: a restricted event must not occupy a page slot or move
// the cursor a community reader is holding. It is parenthesized for the same
// reason as its counterpart, so the two cannot be swapped by accident.
func restrictedAuditNotLike(column string) string {
	return "(" + restrictedAuditClauses(column, " AND ", true) + ")"
}

func restrictedAuditClauses(column, join string, negate bool) string {
	operator := " LIKE "
	if negate {
		operator = " NOT LIKE "
	}
	clause := ""
	for i, prefix := range restrictedAuditPrefixes {
		if i > 0 {
			clause += join
		}
		clause += column + operator + "'" + prefix + "%'"
	}
	return clause
}

// isRestrictedAuditAction is the Go-side equivalent, for code that classifies one
// action rather than building a query. Two spellings of one rule is exactly the
// drift this file exists to remove, so any new caller uses this instead of
// writing its own prefix test.
func isRestrictedAuditAction(action string) bool {
	for _, prefix := range restrictedAuditPrefixes {
		if strings.HasPrefix(action, prefix) {
			return true
		}
	}
	return false
}
