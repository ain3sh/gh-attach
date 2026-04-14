package main

import (
	"strings"
	"testing"

	"gh-attach/internal/attachments"
	"gh-attach/internal/cookies"
)

func TestParseArgsDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := parseArgs([]string{"screenshot.png"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}

	if cfg.kind != commandUpload {
		t.Fatalf("kind = %q, want %q", cfg.kind, commandUpload)
	}
	if cfg.upload.format != attachments.OutputFormatLink {
		t.Fatalf("format = %q, want %q", cfg.upload.format, attachments.OutputFormatLink)
	}
	if len(cfg.upload.paths) != 1 || cfg.upload.paths[0] != "screenshot.png" {
		t.Fatalf("paths = %v, want [screenshot.png]", cfg.upload.paths)
	}
}

func TestParseArgsShortFormatFlags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want attachments.OutputFormat
	}{
		{name: "auto", args: []string{"--auto", "clip.mp4"}, want: attachments.OutputFormatAuto},
		{name: "link", args: []string{"--link", "report.pdf"}, want: attachments.OutputFormatLink},
		{name: "md", args: []string{"--md", "report.pdf"}, want: attachments.OutputFormatLink},
		{name: "url", args: []string{"--url", "report.pdf"}, want: attachments.OutputFormatURL},
		{name: "json", args: []string{"--json", "report.pdf"}, want: attachments.OutputFormatJSON},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := parseArgs(tc.args)
			if err != nil {
				t.Fatalf("parseArgs() error = %v", err)
			}
			if cfg.upload.format != tc.want {
				t.Fatalf("format = %q, want %q", cfg.upload.format, tc.want)
			}
		})
	}
}

func TestParseArgsSessionFile(t *testing.T) {
	t.Parallel()

	cfg, err := parseArgs([]string{"--session-file", "session.txt", "report.pdf"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}

	if cfg.upload.sessionFile != "session.txt" {
		t.Fatalf("sessionFile = %q, want session.txt", cfg.upload.sessionFile)
	}
}

func TestParseArgsRejectsMultipleFormatFlags(t *testing.T) {
	t.Parallel()

	_, err := parseArgs([]string{"--json", "--url", "report.pdf"})
	if err == nil || err.Error() != "output format specified more than once" {
		t.Fatalf("parseArgs() error = %v, want duplicate format error", err)
	}
}

func TestParseArgsAuthCommands(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		args        []string
		wantKind    commandKind
		wantSession string
	}{
		{
			name:     "doctor",
			args:     []string{"auth", "doctor"},
			wantKind: commandAuthDoctor,
		},
		{
			name:        "export",
			args:        []string{"auth", "export", "--session-file", "session.txt"},
			wantKind:    commandAuthExport,
			wantSession: "session.txt",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := parseArgs(tc.args)
			if err != nil {
				t.Fatalf("parseArgs() error = %v", err)
			}
			if cfg.kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", cfg.kind, tc.wantKind)
			}
			if cfg.auth.sessionFile != tc.wantSession {
				t.Fatalf("sessionFile = %q, want %q", cfg.auth.sessionFile, tc.wantSession)
			}
		})
	}
}

func TestParseArgsAuthExportRequiresSessionFile(t *testing.T) {
	t.Parallel()

	_, err := parseArgs([]string{"auth", "export"})
	if err == nil || err.Error() != "auth export requires --session-file" {
		t.Fatalf("parseArgs() error = %v, want missing session-file error", err)
	}
}

func TestHelpTextCoversSimpleAndAdvancedUsage(t *testing.T) {
	t.Parallel()

	help := helpText()

	required := []string{
		"Usage:\n  gh-attach",
		"Primary upload command:",
		"Authentication helpers:",
		"If installed as a GitHub CLI extension",
		"Simple examples:",
		"Advanced examples:",
		"--session-file PATH",
		"--auto",
		"--json",
		"gh-attach -- --file-named-like-a-flag.png",
		cookies.SessionCookieEnvVar,
	}

	for _, snippet := range required {
		if !strings.Contains(help, snippet) {
			t.Fatalf("help text missing %q\n%s", snippet, help)
		}
	}
}

func TestAuthHelpTextCoversDoctorAndExport(t *testing.T) {
	t.Parallel()

	help := authHelpText()
	required := []string{
		"gh-attach auth doctor",
		"gh-attach auth export --session-file PATH",
		"Inspect auth sources",
		"Export the resolved GitHub auth cookies",
		cookies.SessionCookieEnvVar,
	}

	for _, snippet := range required {
		if !strings.Contains(help, snippet) {
			t.Fatalf("auth help text missing %q\n%s", snippet, help)
		}
	}
}
