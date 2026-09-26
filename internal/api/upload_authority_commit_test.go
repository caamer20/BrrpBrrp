package api

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUploadCapabilityIsRecheckedWhenMetadataCommits(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		t.Run(fmt.Sprintf("private_scope=%v", scoped), func(t *testing.T) {
			s, handler, admin, _, member, _, _, conversation := privatePipeline(t)
			role := defaultRole(t, handler, admin)
			setPermissions := func(permissions []string) error {
				w := request(t, handler, "PATCH", "/api/roles/"+role.ID, map[string]any{"name": role.Name, "position": role.Position, "permissions": permissions}, admin)
				if w.Code != http.StatusOK {
					return fmt.Errorf("role change: %d %s", w.Code, w.Body.String())
				}
				return nil
			}
			var raw bytes.Buffer
			writer := multipart.NewWriter(&raw)
			part, err := writer.CreateFormFile("file", "not-authorized.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write([]byte("upload admitted before capability removal")); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			// Reading begins after the route guard and initial destination check.
			// Permission moves before any attachment metadata can commit.
			body := &revokeDuringRequest{body: raw.Bytes(), revoke: func() error { return setPermissions(withoutCapability(role.Permissions, "message.upload")) }}
			path := "/api/uploads"
			if scoped {
				path += "?conversation_id=" + conversation
			}
			r := httptest.NewRequest(http.MethodPost, path, body)
			r.Header.Set("Content-Type", writer.FormDataContentType())
			r.Header.Set("Origin", "https://localhost:8080")
			r.AddCookie(member)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Errorf("revoked upload committed: %d %s", w.Code, w.Body.String())
			}
			if rows := attachmentRows(t, s); rows != 0 {
				t.Errorf("revoked upload created%d metadata rows", rows)
			}
			if files := uploadEntries(t, s); len(files) != 0 {
				t.Errorf("revoked upload retained files: %v", files)
			}
			if err := setPermissions(role.Permissions); err != nil {
				t.Fatal(err)
			}
			if !scoped {
				conversation = ""
			}
			if allowed := directUpload(t, handler, member, conversation); allowed.Code != http.StatusCreated {
				t.Fatalf("regranted upload refused: %d %s", allowed.Code, allowed.Body.String())
			}
		})
	}
}
