package attachments

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUploadFlow(t *testing.T) {
	t.Parallel()

	filePath := filepath.Join(t.TempDir(), "clip.mp4")
	fileBody := []byte("video fixture")
	if err := os.WriteFile(filePath, fileBody, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	file, err := Inspect(filePath)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/owner/repo":
			_, _ = io.WriteString(w, `<html><body>{"uploadToken":"upload-token"}</body></html>`)
		case r.Method == http.MethodPost && r.URL.Path == "/upload/policies/assets":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("ParseMultipartForm() error = %v", err)
			}
			if got := r.FormValue("name"); got != file.Name {
				t.Fatalf("policy name = %q, want %q", got, file.Name)
			}
			if got := r.FormValue("size"); got != "13" {
				t.Fatalf("policy size = %q, want 13", got)
			}
			if got := r.FormValue("content_type"); got != file.ContentType {
				t.Fatalf("policy content_type = %q, want %q", got, file.ContentType)
			}
			if got := r.FormValue("authenticity_token"); got != "upload-token" {
				t.Fatalf("policy authenticity_token = %q, want upload-token", got)
			}
			if got := r.FormValue("repository_id"); got != "42" {
				t.Fatalf("policy repository_id = %q, want 42", got)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"upload_url": server.URL + "/s3",
				"asset": map[string]any{
					"id":           7,
					"name":         file.Name,
					"size":         file.Size,
					"content_type": file.ContentType,
					"href":         "https://github.com/user-attachments/assets/example",
				},
				"form": map[string]string{
					"key":                          "key",
					"acl":                          "private",
					"policy":                       "policy",
					"X-Amz-Algorithm":              "algo",
					"X-Amz-Credential":             "credential",
					"X-Amz-Date":                   "date",
					"X-Amz-Signature":              "signature",
					"Content-Type":                 file.ContentType,
					"Cache-Control":                "max-age=2592000",
					"x-amz-meta-Surrogate-Control": "max-age=31557600",
				},
				"asset_upload_authenticity_token": "finalize-token",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/s3":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if !strings.Contains(string(body), `name="file"; filename="clip.mp4"`) {
				t.Fatalf("s3 body missing file payload")
			}
			if strings.Index(string(body), `name="file"; filename="clip.mp4"`) < strings.Index(string(body), `name="Content-Type"`) {
				t.Fatalf("file field was not written after policy fields")
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && r.URL.Path == "/upload/assets/7":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("ParseMultipartForm() error = %v", err)
			}
			if got := r.FormValue("authenticity_token"); got != "finalize-token" {
				t.Fatalf("finalize authenticity_token = %q, want finalize-token", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": file.Name,
				"href": "https://github.com/user-attachments/assets/example",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &Client{
		httpClient: server.Client(),
		s3Client:   server.Client(),
		baseURL:    server.URL,
		userAgent:  userAgent,
	}

	result, err := client.Upload("owner", "repo", 42, file)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if result.URL != "https://github.com/user-attachments/assets/example" {
		t.Fatalf("Upload() url = %q", result.URL)
	}
	if result.Category != CategoryVideo {
		t.Fatalf("Upload() category = %q, want %q", result.Category, CategoryVideo)
	}
}
