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
	"group.", // private group lifecycle: who invited, who joined, who left
}

// The restricted audit feed is readable by the installation's operator and by an
// account holding `report.review_private` — **not** by ordinary `audit.read`.
//
// It used to require only `audit.read`, on the argument that a separate URL is a
// separate policy. It is not: the feed carries the *reporter's* username, the
// reviewer's username, evidence identifiers and timestamps. An account that is
// refused the evidence itself with 403 must not learn that a specific person
// reported a specific private message, who reviewed it, or when — that is the fact
// the evidence policy exists to withhold, and the audit row states it plainly
// (completion review 14, V14-01).
//
// So the two are separate policies on one feed, not two feeds:
//
//   - `report.review_private` sees private *evidence* audit. It already reads the
//     evidence, so the row naming it adds nothing that was not deliberately
//     submitted to them.
//   - `audit.read` alone sees neither. It remains a community-feed capability.
//
// Group lifecycle is treated the same way and *not* widened: a group is not
// submitted evidence, so holding `report.review_private` does not grant private group
// membership. A reviewer of reported messages learns nothing about who is in whose
// group. The operator, who administers the installation, still sees all of it.
//
// A broader policy is a legitimate thing for an owner to choose. It is not a thing
// to encode as a bug fix, and the evidence for that is a review that had to be
// countermanded by the person who owns the data.

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
