package api

import (
	"strings"
	"testing"

	"brrpbrrp/internal/authz"
)

// TestEveryRouteDeclaresItsPolicy is the inventory check for WP-06: an endpoint
// cannot be registered without stating who may reach it. A route with no
// capability must explain, in words a reviewer can check, how its handler scopes
// what it returns.
func TestEveryRouteDeclaresItsPolicy(t *testing.T) {
	s, _ := testServer(t)
	seen := map[string]bool{}
	for _, registered := range s.routes() {
		key := registered.Method + " " + registered.Path
		if seen[key] {
			t.Fatalf("duplicate route registration: %s", key)
		}
		seen[key] = true
		if !strings.HasPrefix(registered.Path, "/") {
			t.Fatalf("route %s has an invalid path", key)
		}
		if registered.Handler == nil {
			t.Fatalf("route %s has no handler", key)
		}
		if registered.Policy.Capability != "" && !authz.Valid(registered.Policy.Capability) {
			t.Fatalf("route %s requires unknown capability %q", key, registered.Policy.Capability)
		}
		if registered.Policy.Capability == "" && registered.Policy.Note == "" {
			t.Fatalf("route %s declares neither a capability nor an explanation", key)
		}
		if _, public := publicRoutes[key]; public {
			t.Fatalf("route %s is declared both authenticated and public", key)
		}
	}
	for key, reason := range publicRoutes {
		if strings.TrimSpace(reason) == "" {
			t.Fatalf("public route %s has no documented reason", key)
		}
		if !strings.HasPrefix(key, "GET /api/setup") && !strings.HasPrefix(key, "POST /api/") {
			t.Fatalf("public route %s is not an expected bootstrap endpoint", key)
		}
	}
	// Administrative capabilities must be real capabilities, so the stricter
	// mutation limit cannot be attached to a name nothing enforces.
	for capability := range administrativeCapabilities {
		if !authz.Valid(capability) {
			t.Fatalf("administrative capability %q is not in the catalog", capability)
		}
	}
}

// TestSensitiveRoutesRequireRecentStrongAuthentication pins the exact set
// of irreversible identity and operator actions, so the list cannot grow or
// shrink without a deliberate change here. Every one of them also demands an
// enrolled second factor of an operator account (WP-05); a fresh password alone
// is not enough to open a full archive or take over another account.
func TestSensitiveRoutesRequireRecentStrongAuthentication(t *testing.T) {
	s, _ := testServer(t)
	fresh := make([]string, 0)
	for _, registered := range s.routes() {
		if registered.Policy.FreshAuth {
			fresh = append(fresh, registered.Method+" "+registered.Path)
		}
	}
	want := []string{
		"POST /api/owner",
		"POST /api/admin/users/{id}/anonymize",
		"POST /api/admin/users/{id}/reset-password",
		"DELETE /api/admin/users/{id}/mfa",
		"POST /api/admin/backup",
		"GET /api/admin/backups/{id}",
	}
	if strings.Join(fresh, ",") != strings.Join(want, ",") {
		t.Fatalf("recent-auth routes = %v, want %v", fresh, want)
	}
	// The route that unlocks them must stay reachable with an ordinary session,
	// or a stale session could never be refreshed before it expires.
	if _, ok := publicRoutes["POST /api/me/reauth"]; ok {
		t.Fatal("reauthentication must not be public")
	}
}

// TestOperatorTierIsNotReachableByOrdinaryRoles proves that the capabilities which
// can defeat a private boundary are not seeded onto any delegation role.
func TestOperatorTierIsNotReachableByOrdinaryRoles(t *testing.T) {
	s, _ := testServer(t)
	roles, err := s.store.Roles(t.Context(), s.store)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range roles {
		for _, permission := range role.Permissions {
			if authz.IsOperator(authz.Capability(permission)) {
				t.Fatalf("seeded role %s holds operator capability %s", role.Key, permission)
			}
		}
	}
	// Those routes are reachable only through operator capabilities.
	operatorRoutes := 0
	for _, registered := range s.routes() {
		if authz.IsOperator(registered.Policy.Capability) {
			operatorRoutes++
		}
	}
	if operatorRoutes != 5 {
		t.Fatalf("operator-gated routes = %d, want 5 (ownership, chosen-password reset, lost-factor removal, backup create and download)", operatorRoutes)
	}
}
