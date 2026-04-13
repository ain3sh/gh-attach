package main

import (
	"strings"
	"testing"

	"gh-attach/internal/attachments"
)

func TestParseArgsDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := parseArgs([]string{"screenshot.png"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}

	if cfg.format != attachments.OutputFormatLink {
		t.Fatalf("format = %q, want %q", cfg.format, attachments.OutputFormatLink)
	}
	if len(cfg.paths) != 1 || cfg.paths[0] != "screenshot.png" {
		t.Fatalf("paths = %v, want [screenshot.png]", cfg.paths)
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
			if cfg.format != tc.want {
				t.Fatalf("format = %q, want %q", cfg.format, tc.want)
			}
		})
	}
}

func TestParseArgsRejectsMultipleFormatFlags(t *testing.T) {
	t.Parallel()

	_, err := parseArgs([]string{"--json", "--url", "report.pdf"})
	if err == nil || err.Error() != "output format specified more than once" {
		t.Fatalf("parseArgs() error = %v, want duplicate format error", err)
	}
}

func TestHelpTextCoversSimpleAndAdvancedUsage(t *testing.T) {
	t.Parallel()

	help := helpText()

	required := []string{
		"Usage: gh-attach",
		"Primary command:",
		"If installed as a GitHub CLI extension",
		"Simple examples:",
		"Advanced examples:",
		"--auto",
		"--json",
		"gh-attach -- --file-named-like-a-flag.png",
		"GH_ATTACH_USER_SESSION",
	}

	for _, snippet := range required {
		if !strings.Contains(help, snippet) {
			t.Fatalf("help text missing %q\n%s", snippet, help)
		}
	}
}
