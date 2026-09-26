package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFrontendRoutesReturnCompiledPage(t *testing.T) {
	for _, path := range []string{"/", "/setup", "/settings"} {
		request := httptest.NewRequest("GET", path, nil)
		response := httptest.NewRecorder()
		serveFrontend(response, request)
		if response.Code != 200 || !strings.Contains(response.Body.String(), "/assets/") {
			t.Fatalf("%s returned %d: %s", path, response.Code, response.Body.String())
		}
	}
}
