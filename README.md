# gh-attach

`gh-attach` uploads the same kinds of attachments GitHub's web UI supports for issues, pull requests, and discussions: images, videos, and common document/code/archive files.

## Quick start

The happy path is one command:

```bash
gh-attach screenshot.png
```

And the power path stays small:

```bash
gh-attach clip.mp4 report.pdf --repo owner/repo --json
```

## Install

One-line install:

```bash
curl -fsSL https://github.com/ain3sh/gh-attach/releases/latest/download/install.sh | sh
```

Install to a custom directory:

```bash
curl -fsSL https://github.com/ain3sh/gh-attach/releases/latest/download/install.sh | sh -s -- --bin-dir "$HOME/bin"
```

Pin a specific version:

```bash
curl -fsSL https://github.com/ain3sh/gh-attach/releases/latest/download/install.sh | sh -s -- --ref v0.1.0
```

The bootstrap script downloads the latest published release source by default and builds locally, so `go` must already be installed.

From a local checkout:

```bash
./install.sh
```

From a local checkout, install to a custom directory:

```bash
./install.sh --bin-dir "$HOME/bin"
```

Build manually:

```bash
go build -o gh-attach .
```

If you publish it as a GitHub CLI extension, GitHub CLI will expose the same binary as:

```bash
gh attach
```

Run directly from the checkout:

```bash
go run . screenshot.png --repo owner/repo
```

## Usage

```bash
gh-attach [--repo owner/repo] [--auto|--link|--md|--url|--json|--format auto|link|url|json] FILE...
```

If installed as a GitHub CLI extension, the same binary is typically invoked as:

```bash
gh attach FILE...
```

### One-glance CLI prototype

```text
gh-attach FILE...                   # default: markdown links
gh-attach FILE... --repo OWNER/REPO # explicit target repo
gh-attach FILE... --auto            # image => ![name](url), else raw URL
gh-attach FILE... --url             # raw URLs
gh-attach FILE... --json            # JSON objects
gh-attach -- FILE...                # filenames that begin with '-'
```

### Simple examples

Upload an image from the current repo:

```bash
gh-attach screenshot.png
```

Upload a regular file and get a Markdown link:

```bash
gh-attach report.pdf
```

Upload several attachments at once:

```bash
gh-attach image.png clip.mp4 report.pdf
```

### More advanced examples

Upload several attachments to an explicit repo:

```bash
gh-attach clip.mp4 report.pdf logs.zip --repo owner/repo
```

Match GitHub's web-style output:

```bash
gh-attach --auto screenshot.png clip.mp4
```

Emit plain URLs:

```bash
gh-attach --url report.pdf
```

Emit structured JSON:

```bash
gh-attach --json screenshot.png
```

Pass a filename that starts with `-`:

```bash
gh-attach -- --trace.log
```

### Output modes

| Flag | Behavior |
| --- | --- |
| _default_ / `--link` / `--md` | Always prints a Markdown link: `[name](url)` |
| `--auto` | Prints `![name](url)` for images, plain URLs for everything else |
| `--url` | Prints the raw uploaded URL |
| `--json` | Prints a JSON object with URL, name, MIME type, size, and category |

`--format auto|link|url|json` is the long-form equivalent when you want to generate flags programmatically.

## Authentication

By default, `gh-attach` reads your GitHub auth cookies from a supported local browser profile.

Supported browser sources:

- Chrome
- Brave
- Chromium
- Edge
- Firefox
- Zen

For Firefox-derived browsers such as Firefox and Zen, `gh-attach` also reads live session cookies from `sessionstore-backups/*.jsonlz4` so authenticated uploads keep working even when `cookies.sqlite` alone is not enough.

Inspect what auth source `gh-attach` will use:

```bash
gh-attach auth doctor
```

Export a reusable auth-cookie file:

```bash
gh-attach auth export --session-file "$HOME/.config/gh-attach/session.json"
```

For headless or scripted environments, set:

```bash
export GH_ATTACH_USER_SESSION=...
```

Or reuse an exported session file directly:

```bash
gh-attach report.pdf --session-file "$HOME/.config/gh-attach/session.json"
```

## Repository targeting

If `--repo` is omitted, `gh-attach` tries to infer the target repo from:

```bash
git remote get-url origin
```

It then resolves the numeric repository ID via:

```bash
gh api repos/{owner}/{repo} --jq .id
```

So the `gh` CLI must be installed and authenticated.

## Supported attachments

The CLI is built around the file classes GitHub documents for comment attachments, including:

- Images: `png`, `gif`, `jpg`, `jpeg`, `svg`, `bmp`, `tif`, `tiff`
- Videos: `mp4`, `mov`, `webm`
- Documents: `pdf`, Office docs, OpenDocument files, `rtf`, `doc`
- Text/data/code: `txt`, `md`, `json`, `log`, `csv`, `py`, `ts`, `tsx`, `html`, `xml`, `yaml`, and more
- Archives/audio/log-style files: `zip`, `gz`, `tgz`, `mp3`, `wav`, `msg`, `eml`, `debug`

## Size limits

The CLI validates against GitHub's documented attachment limits before upload:

- Images and GIFs: 10 MB
- Videos: 10 MB on free plans, up to 100 MB on paid plans
- Other files: 25 MB

For videos above 10 MB, the CLI warns instead of failing because the final outcome depends on the repository plan and your access level.

## How it works

`gh-attach` follows the same internal attachment flow GitHub's web UI uses:

1. Resolve GitHub auth cookies from `--session-file`, `GH_ATTACH_USER_SESSION`, or a supported browser profile
2. Fetch the repository page and extract the upload token
3. Request an upload policy from GitHub
4. Upload the file to GitHub's temporary S3 target
5. Finalize the upload using GitHub's returned asset endpoint and print the resulting attachment reference

This keeps attachments scoped the way GitHub's web uploads are, including private-repo behavior.

## Inspiration

This project was inspired by [`drogers0/gh-image`](https://github.com/drogers0/gh-image), which proved out the core GitHub asset-upload flow for images. `gh-attach` extends that idea to cover the broader set of attachment types supported by GitHub's web UI.
