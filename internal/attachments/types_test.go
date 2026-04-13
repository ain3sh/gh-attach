package attachments

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectClassifiesAttachments(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		filename string
		want     Category
	}{
		{name: "image", filename: "photo.PNG", want: CategoryImage},
		{name: "video", filename: "demo.mp4", want: CategoryVideo},
		{name: "file", filename: "report.pdf", want: CategoryFile},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), tc.filename)
			if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			file, err := Inspect(path)
			if err != nil {
				t.Fatalf("Inspect() error = %v", err)
			}

			if file.Category != tc.want {
				t.Fatalf("Inspect() category = %q, want %q", file.Category, tc.want)
			}
			if file.Name != tc.filename {
				t.Fatalf("Inspect() name = %q, want %q", file.Name, tc.filename)
			}
			if file.Size == 0 {
				t.Fatalf("Inspect() size = 0, want > 0")
			}
		})
	}
}

func TestInspectRejectsUnsupportedExtensions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "archive.tar")
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := Inspect(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported attachment type") {
		t.Fatalf("Inspect() error = %v, want unsupported attachment type", err)
	}
}

func TestValidateLimits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		file        File
		wantWarning bool
		wantErr     string
	}{
		{
			name:    "image too large",
			file:    File{Name: "image.png", Category: CategoryImage, Size: 11 * 1024 * 1024},
			wantErr: "10MB image limit",
		},
		{
			name:        "paid plan sized video warning",
			file:        File{Name: "clip.mp4", Category: CategoryVideo, Size: 11 * 1024 * 1024},
			wantWarning: true,
		},
		{
			name:    "file too large",
			file:    File{Name: "report.pdf", Category: CategoryFile, Size: 26 * 1024 * 1024},
			wantErr: "25MB file limit",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			warnings, err := Validate(tc.file)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Validate() error = %v, want substring %q", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if tc.wantWarning && len(warnings) == 0 {
				t.Fatalf("Validate() warnings = %v, want warning", warnings)
			}
		})
	}
}

func TestResultRender(t *testing.T) {
	t.Parallel()

	result := Result{
		URL:         "https://github.com/user-attachments/assets/example",
		Name:        "diagram [v2].png",
		ContentType: "image/png",
		Size:        128,
		Category:    CategoryImage,
	}

	auto, err := result.Render(OutputFormatAuto)
	if err != nil {
		t.Fatalf("Render(auto) error = %v", err)
	}
	if auto != "![diagram \\[v2\\].png](https://github.com/user-attachments/assets/example)" {
		t.Fatalf("Render(auto) = %q", auto)
	}

	link, err := result.Render(OutputFormatLink)
	if err != nil {
		t.Fatalf("Render(link) error = %v", err)
	}
	if link != "[diagram \\[v2\\].png](https://github.com/user-attachments/assets/example)" {
		t.Fatalf("Render(link) = %q", link)
	}

	url, err := result.Render(OutputFormatURL)
	if err != nil {
		t.Fatalf("Render(url) error = %v", err)
	}
	if url != result.URL {
		t.Fatalf("Render(url) = %q, want %q", url, result.URL)
	}

	jsonOutput, err := result.Render(OutputFormatJSON)
	if err != nil {
		t.Fatalf("Render(json) error = %v", err)
	}

	var payload Result
	if err := json.Unmarshal([]byte(jsonOutput), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.URL != result.URL || payload.Category != result.Category {
		t.Fatalf("json output = %+v, want %+v", payload, result)
	}
}
