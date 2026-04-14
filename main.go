package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"gh-attach/internal/attachments"
	"gh-attach/internal/cookies"
	"gh-attach/internal/repo"
)

const usage = `Usage:
  gh-attach [--repo owner/repo] [--session-file PATH] [--auto|--link|--md|--url|--json|--format auto|link|url|json] FILE...
  gh-attach auth doctor [--session-file PATH]
  gh-attach auth export --session-file PATH`

type commandKind string

const (
	commandUpload     commandKind = "upload"
	commandAuthDoctor commandKind = "auth-doctor"
	commandAuthExport commandKind = "auth-export"
)

func main() {
	args := os.Args[1:]

	cfg, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n%s", err, helpTextForArgs(args))
		os.Exit(1)
	}

	if cfg.showHelp {
		fmt.Print(helpTextForConfig(cfg))
		return
	}

	switch cfg.kind {
	case commandAuthDoctor:
		os.Exit(runAuthDoctor(cfg.auth))
	case commandAuthExport:
		os.Exit(runAuthExport(cfg.auth))
	default:
		os.Exit(runUpload(cfg.upload))
	}
}

func runUpload(cfg uploadConfig) int {
	repoInfo, err := repo.Resolve(cfg.owner, cfg.name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving repository: %v\n", err)
		return 1
	}

	auth, err := cookies.GetGitHubAuth(cookies.ResolveOptions{SessionFile: cfg.sessionFile})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	client := attachments.NewClient(auth.Cookies)
	hasError := false

	for _, path := range cfg.paths {
		file, err := attachments.Inspect(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error preparing %s: %v\n", path, err)
			hasError = true
			continue
		}

		warnings, err := attachments.Validate(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error preparing %s: %v\n", path, err)
			hasError = true
			continue
		}

		for _, warning := range warnings {
			fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
		}

		result, err := client.Upload(repoInfo.Owner, repoInfo.Name, repoInfo.ID, file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error uploading %s: %v\n", path, err)
			hasError = true
			continue
		}

		rendered, err := result.Render(cfg.format)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error rendering %s: %v\n", path, err)
			hasError = true
			continue
		}

		fmt.Println(rendered)
	}

	if hasError {
		return 1
	}

	return 0
}

func runAuthDoctor(cfg authConfig) int {
	report, err := cookies.Doctor(context.Background(), cookies.ResolveOptions{SessionFile: cfg.sessionFile})
	fmt.Print(formatDoctorReport(report))
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
		return 1
	}

	return 0
}

func runAuthExport(cfg authConfig) int {
	source, err := cookies.ExportGitHubSession(context.Background(), cfg.sessionFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	fmt.Printf("Exported GitHub user_session to %s\n", cfg.sessionFile)
	fmt.Printf("Source: %s\n", source.Describe())
	return 0
}

type config struct {
	kind     commandKind
	upload   uploadConfig
	auth     authConfig
	showHelp bool
}

type uploadConfig struct {
	owner       string
	name        string
	format      attachments.OutputFormat
	paths       []string
	sessionFile string
}

type authConfig struct {
	sessionFile string
}

func parseArgs(args []string) (*config, error) {
	if len(args) > 0 && args[0] == "auth" {
		return parseAuthArgs(args[1:])
	}

	return parseUploadArgs(args)
}

func parseUploadArgs(args []string) (*config, error) {
	cfg := &config{
		kind:   commandUpload,
		upload: uploadConfig{format: attachments.OutputFormatLink},
	}

	flagsDone := false
	repoFlagSeen := false
	formatFlagSeen := false
	sessionFileSeen := false

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if flagsDone {
			cfg.upload.paths = append(cfg.upload.paths, arg)
			continue
		}

		switch {
		case arg == "--":
			flagsDone = true
		case arg == "--help" || arg == "-h":
			cfg.showHelp = true
		case arg == "--repo":
			if repoFlagSeen {
				return nil, fmt.Errorf("--repo specified more than once")
			}
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--repo requires a value in owner/repo format")
			}
			repoFlagSeen = true
			i++
			if err := cfg.upload.setRepo(args[i]); err != nil {
				return nil, err
			}
		case strings.HasPrefix(arg, "--repo="):
			if repoFlagSeen {
				return nil, fmt.Errorf("--repo specified more than once")
			}
			repoFlagSeen = true
			if err := cfg.upload.setRepo(strings.SplitN(arg, "=", 2)[1]); err != nil {
				return nil, err
			}
		case arg == "--session-file":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--session-file requires a value")
			}
			i++
			if err := setSessionFile(args[i], &cfg.upload.sessionFile, &sessionFileSeen); err != nil {
				return nil, err
			}
		case strings.HasPrefix(arg, "--session-file="):
			if err := setSessionFile(strings.SplitN(arg, "=", 2)[1], &cfg.upload.sessionFile, &sessionFileSeen); err != nil {
				return nil, err
			}
		case arg == "--format":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--format requires a value")
			}
			i++
			if err := cfg.upload.setFormat(args[i], &formatFlagSeen); err != nil {
				return nil, err
			}
		case strings.HasPrefix(arg, "--format="):
			if err := cfg.upload.setFormat(strings.SplitN(arg, "=", 2)[1], &formatFlagSeen); err != nil {
				return nil, err
			}
		case arg == "--auto":
			if err := cfg.upload.setFormat(string(attachments.OutputFormatAuto), &formatFlagSeen); err != nil {
				return nil, err
			}
		case arg == "--link" || arg == "--md":
			if err := cfg.upload.setFormat(string(attachments.OutputFormatLink), &formatFlagSeen); err != nil {
				return nil, err
			}
		case arg == "--url":
			if err := cfg.upload.setFormat(string(attachments.OutputFormatURL), &formatFlagSeen); err != nil {
				return nil, err
			}
		case arg == "--json":
			if err := cfg.upload.setFormat(string(attachments.OutputFormatJSON), &formatFlagSeen); err != nil {
				return nil, err
			}
		case strings.HasPrefix(arg, "-") && arg != "-":
			return nil, fmt.Errorf("unknown flag %s", arg)
		default:
			cfg.upload.paths = append(cfg.upload.paths, arg)
		}
	}

	if cfg.showHelp {
		return cfg, nil
	}
	if len(cfg.upload.paths) == 0 {
		return nil, fmt.Errorf("at least one file path is required")
	}

	return cfg, nil
}

func parseAuthArgs(args []string) (*config, error) {
	cfg := &config{}
	if len(args) == 0 {
		cfg.showHelp = true
		return cfg, nil
	}

	if args[0] == "--help" || args[0] == "-h" {
		cfg.showHelp = true
		return cfg, nil
	}

	switch args[0] {
	case "doctor":
		cfg.kind = commandAuthDoctor
	case "export":
		cfg.kind = commandAuthExport
	default:
		return nil, fmt.Errorf("unknown auth command %q", args[0])
	}

	sessionFileSeen := false
	for i := 1; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "--help" || arg == "-h":
			cfg.showHelp = true
		case arg == "--session-file":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--session-file requires a value")
			}
			i++
			if err := setSessionFile(args[i], &cfg.auth.sessionFile, &sessionFileSeen); err != nil {
				return nil, err
			}
		case strings.HasPrefix(arg, "--session-file="):
			if err := setSessionFile(strings.SplitN(arg, "=", 2)[1], &cfg.auth.sessionFile, &sessionFileSeen); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unexpected argument %q", arg)
		}
	}

	if cfg.showHelp {
		return cfg, nil
	}
	if cfg.kind == commandAuthExport && strings.TrimSpace(cfg.auth.sessionFile) == "" {
		return nil, fmt.Errorf("auth export requires --session-file")
	}

	return cfg, nil
}

func (c *uploadConfig) setRepo(raw string) error {
	parts := strings.SplitN(strings.TrimSpace(raw), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("--repo must be in owner/repo format, got %q", raw)
	}

	c.owner = parts[0]
	c.name = parts[1]
	return nil
}

func (c *uploadConfig) setFormat(raw string, seen *bool) error {
	if *seen {
		return fmt.Errorf("output format specified more than once")
	}

	format, err := attachments.ParseOutputFormat(raw)
	if err != nil {
		return err
	}

	c.format = format
	*seen = true
	return nil
}

func setSessionFile(raw string, target *string, seen *bool) error {
	if *seen {
		return fmt.Errorf("--session-file specified more than once")
	}

	sessionFile := strings.TrimSpace(raw)
	if sessionFile == "" {
		return fmt.Errorf("--session-file cannot be empty")
	}

	*target = sessionFile
	*seen = true
	return nil
}

func helpText() string {
	var b strings.Builder

	lines := []string{
		usage,
		"",
		"Upload GitHub web-style attachments from the command line.",
		"",
		"Primary upload command:",
		"  gh-attach FILE...",
		"",
		"If installed as a GitHub CLI extension, the same binary is invoked as:",
		"  gh attach FILE...",
		"",
		"Authentication helpers:",
		"  gh-attach auth doctor",
		"  gh-attach auth export --session-file \"$HOME/.config/gh-attach/session\"",
		"",
		"Simple examples:",
		"  gh-attach screenshot.png",
		"  gh-attach report.pdf",
		"  gh-attach image.png video.mp4 notes.txt",
		"",
		"Advanced examples:",
		"  gh-attach screenshot.png --repo owner/repo",
		"  gh-attach report.pdf --session-file \"$HOME/.config/gh-attach/session\"",
		"  gh-attach clip.mp4 --auto",
		"  gh-attach report.pdf --url",
		"  gh-attach build.log --json",
		"  gh-attach --repo owner/repo --json image.png video.mp4",
		"  gh-attach -- --file-named-like-a-flag.png",
		"",
		"Upload flags:",
		"  --repo owner/repo           Target repository; otherwise inferred from git remote",
		"  --session-file PATH         Read exported GitHub auth cookies from a file instead of env/browser discovery",
		"  --auto                      Image => ![name](url); everything else => raw URL",
		"  --link, --md                Always output [name](url) (default)",
		"  --url                       Always output the raw uploaded URL",
		"  --json                      Output structured JSON",
		"  --format auto|link|url|json Long-form equivalent for selecting output mode",
		"  --help, -h                  Show this help text",
		"",
		"How repo selection works:",
		"  1. Use --repo if provided",
		"  2. Otherwise infer owner/repo from git remote origin",
		"  3. Resolve repository ID via `gh api repos/{owner}/{repo} --jq .id`",
		"",
		"Authentication:",
		"  1. Read --session-file if provided",
		fmt.Sprintf("  2. Read %s if set", cookies.SessionCookieEnvVar),
		"  3. Discover GitHub auth cookies in Chrome, Brave, Chromium, Edge, Firefox, or Zen",
		"",
		"Notes:",
		"  - Upload one or many files in a single command",
		"  - Use -- to separate flags from filenames that start with '-'",
		"  - Use `gh-attach auth doctor` to inspect available auth sources",
		"  - Use `gh-attach auth export --session-file PATH` to create a reusable auth-cookie file",
		"  - Images, videos, and common document/code/archive file types are supported",
	}

	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	return b.String()
}

func authHelpText() string {
	var b strings.Builder

	lines := []string{
		"Usage:",
		"  gh-attach auth doctor [--session-file PATH]",
		"  gh-attach auth export --session-file PATH",
		"",
		"Commands:",
		"  doctor    Inspect auth sources and show which one gh-attach would use",
		"  export    Export the resolved GitHub auth cookies into a secure file",
		"",
		"Examples:",
		"  gh-attach auth doctor",
		"  gh-attach auth doctor --session-file \"$HOME/.config/gh-attach/session\"",
		"  gh-attach auth export --session-file \"$HOME/.config/gh-attach/session\"",
		"",
		"Resolution order:",
		"  1. --session-file",
		fmt.Sprintf("  2. %s", cookies.SessionCookieEnvVar),
		"  3. Browser cookie stores (Chrome, Brave, Chromium, Edge, Firefox, Zen)",
		"",
		"Notes:",
		"  - `auth export` writes GitHub auth cookies as JSON",
		"  - Exported session files are written with 0600 permissions",
		"  - `auth doctor` never prints the cookie value",
	}

	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	return b.String()
}

func helpTextForArgs(args []string) string {
	if len(args) > 0 && args[0] == "auth" {
		return authHelpText()
	}
	return helpText()
}

func helpTextForConfig(cfg *config) string {
	if cfg != nil && (cfg.kind == commandAuthDoctor || cfg.kind == commandAuthExport || (cfg.showHelp && cfg.kind == "")) {
		return authHelpText()
	}
	return helpText()
}

func formatDoctorReport(report *cookies.DoctorReport) string {
	if report == nil {
		return "gh-attach auth doctor\n\nNo report available.\n"
	}

	var b strings.Builder
	lines := []string{
		"gh-attach auth doctor",
		"",
		"Resolution order:",
		"  1. --session-file",
		fmt.Sprintf("  2. %s", cookies.SessionCookieEnvVar),
		"  3. Browser cookie stores (Chrome, Brave, Chromium, Edge, Firefox, Zen)",
		"",
		"Session file:",
		fmt.Sprintf("  path: %s", fallbackText(report.SessionFile, "not provided")),
		fmt.Sprintf("  status: %s", report.SessionFileStatus),
		"",
		"Environment:",
		fmt.Sprintf("  %s: %s", cookies.SessionCookieEnvVar, report.EnvironmentStatus),
		"",
		"Discovered browser stores:",
	}

	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	if len(report.Stores) == 0 {
		b.WriteString("  none\n")
	} else {
		for _, store := range report.Stores {
			status := "no github.com user_session"
			if store.HasGitHubSession {
				status = "github.com user_session found"
			}

			label := fmt.Sprintf("  - %s", store.Browser)
			if store.Profile != "" {
				label += " / " + store.Profile
			}
			if store.DefaultProfile {
				label += " [default]"
			}

			b.WriteString(label)
			b.WriteString(": ")
			b.WriteString(status)
			b.WriteByte('\n')

			if store.FilePath != "" {
				b.WriteString("    path: ")
				b.WriteString(store.FilePath)
				b.WriteByte('\n')
			}
			if store.Error != "" {
				b.WriteString("    error: ")
				b.WriteString(store.Error)
				b.WriteByte('\n')
			}
		}
	}

	if len(report.DiscoveryErrors) > 0 {
		b.WriteByte('\n')
		b.WriteString("Discovery errors:\n")
		for _, discoveryErr := range report.DiscoveryErrors {
			b.WriteString("  - ")
			b.WriteString(discoveryErr)
			b.WriteByte('\n')
		}
	}

	b.WriteByte('\n')
	b.WriteString("Selected source:\n")
	b.WriteString("  ")
	if report.Selected != nil {
		b.WriteString(report.Selected.Describe())
	} else {
		b.WriteString("none")
	}
	b.WriteByte('\n')

	return b.String()
}

func fallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
