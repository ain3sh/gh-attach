package main

import (
	"fmt"
	"os"
	"strings"

	"gh-attach/internal/attachments"
	"gh-attach/internal/cookies"
	"gh-attach/internal/repo"
)

const usage = "Usage: gh-attach [--repo owner/repo] [--auto|--link|--md|--url|--json|--format auto|link|url|json] FILE..."

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, usage)
		os.Exit(1)
	}

	if cfg.showHelp {
		printHelp()
		return
	}

	repoInfo, err := repo.Resolve(cfg.owner, cfg.name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving repository: %v\n", err)
		os.Exit(1)
	}

	sessionCookie, err := cookies.GetGitHubSession()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	client := attachments.NewClient(sessionCookie)
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
		os.Exit(1)
	}
}

type config struct {
	owner    string
	name     string
	format   attachments.OutputFormat
	paths    []string
	showHelp bool
}

func parseArgs(args []string) (*config, error) {
	cfg := &config{format: attachments.OutputFormatLink}
	flagsDone := false
	repoFlagSeen := false
	formatFlagSeen := false

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if flagsDone {
			cfg.paths = append(cfg.paths, arg)
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
			if err := cfg.setRepo(args[i]); err != nil {
				return nil, err
			}
		case strings.HasPrefix(arg, "--repo="):
			if repoFlagSeen {
				return nil, fmt.Errorf("--repo specified more than once")
			}
			repoFlagSeen = true
			if err := cfg.setRepo(strings.SplitN(arg, "=", 2)[1]); err != nil {
				return nil, err
			}
		case arg == "--format":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--format requires a value")
			}
			i++
			if err := cfg.setFormat(args[i], &formatFlagSeen); err != nil {
				return nil, err
			}
		case strings.HasPrefix(arg, "--format="):
			if err := cfg.setFormat(strings.SplitN(arg, "=", 2)[1], &formatFlagSeen); err != nil {
				return nil, err
			}
		case arg == "--auto":
			if err := cfg.setFormat(string(attachments.OutputFormatAuto), &formatFlagSeen); err != nil {
				return nil, err
			}
		case arg == "--link" || arg == "--md":
			if err := cfg.setFormat(string(attachments.OutputFormatLink), &formatFlagSeen); err != nil {
				return nil, err
			}
		case arg == "--url":
			if err := cfg.setFormat(string(attachments.OutputFormatURL), &formatFlagSeen); err != nil {
				return nil, err
			}
		case arg == "--json":
			if err := cfg.setFormat(string(attachments.OutputFormatJSON), &formatFlagSeen); err != nil {
				return nil, err
			}
		case strings.HasPrefix(arg, "-") && arg != "-":
			return nil, fmt.Errorf("unknown flag %s", arg)
		default:
			cfg.paths = append(cfg.paths, arg)
		}
	}

	if cfg.showHelp {
		return cfg, nil
	}
	if len(cfg.paths) == 0 {
		return nil, fmt.Errorf("at least one file path is required")
	}

	return cfg, nil
}

func (c *config) setRepo(raw string) error {
	parts := strings.SplitN(strings.TrimSpace(raw), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("--repo must be in owner/repo format, got %q", raw)
	}

	c.owner = parts[0]
	c.name = parts[1]
	return nil
}

func (c *config) setFormat(raw string, seen *bool) error {
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

func printHelp() {
	fmt.Print(helpText())
}

func helpText() string {
	var b strings.Builder

	lines := []string{
		usage,
		"",
		"Upload GitHub web-style attachments from the command line.",
		"",
		"Primary command:",
		"  gh-attach FILE...",
		"",
		"If installed as a GitHub CLI extension, the same binary is invoked as:",
		"  gh attach FILE...",
		"",
		"Simple examples:",
		"  gh-attach screenshot.png",
		"  gh-attach report.pdf",
		"  gh-attach image.png video.mp4 notes.txt",
		"",
		"Advanced examples:",
		"  gh-attach screenshot.png --repo owner/repo",
		"  gh-attach clip.mp4 --auto",
		"  gh-attach report.pdf --url",
		"  gh-attach build.log --json",
		"  gh-attach --repo owner/repo --json image.png video.mp4",
		"  gh-attach -- --file-named-like-a-flag.png",
		"",
		"Flags:",
		"  --repo owner/repo           Target repository; otherwise inferred from git remote",
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
		"  - Default: read github.com user_session from a supported browser",
		"  - Override: set GH_ATTACH_USER_SESSION for headless or scripted use",
		"",
		"Notes:",
		"  - Upload one or many files in a single command",
		"  - Use -- to separate flags from filenames that start with '-'",
		"  - Images, videos, and common document/code/archive file types are supported",
	}

	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	return b.String()
}
