package attachments

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Category string

const (
	CategoryImage Category = "image"
	CategoryVideo Category = "video"
	CategoryFile  Category = "file"
)

type OutputFormat string

const (
	OutputFormatAuto OutputFormat = "auto"
	OutputFormatLink OutputFormat = "link"
	OutputFormatURL  OutputFormat = "url"
	OutputFormatJSON OutputFormat = "json"
)

type File struct {
	Path        string
	Name        string
	Extension   string
	ContentType string
	Size        int64
	Category    Category
}

type Result struct {
	URL         string   `json:"url"`
	Name        string   `json:"name"`
	ContentType string   `json:"content_type"`
	Size        int64    `json:"size"`
	Category    Category `json:"category"`
}

var supportedExtensions = map[string]Category{
	"png":       CategoryImage,
	"gif":       CategoryImage,
	"jpg":       CategoryImage,
	"jpeg":      CategoryImage,
	"svg":       CategoryImage,
	"bmp":       CategoryImage,
	"tif":       CategoryImage,
	"tiff":      CategoryImage,
	"mp4":       CategoryVideo,
	"mov":       CategoryVideo,
	"webm":      CategoryVideo,
	"pdf":       CategoryFile,
	"docx":      CategoryFile,
	"pptx":      CategoryFile,
	"xlsx":      CategoryFile,
	"xls":       CategoryFile,
	"xlsm":      CategoryFile,
	"odt":       CategoryFile,
	"fodt":      CategoryFile,
	"ods":       CategoryFile,
	"fods":      CategoryFile,
	"odp":       CategoryFile,
	"fodp":      CategoryFile,
	"odg":       CategoryFile,
	"fodg":      CategoryFile,
	"odf":       CategoryFile,
	"rtf":       CategoryFile,
	"doc":       CategoryFile,
	"txt":       CategoryFile,
	"md":        CategoryFile,
	"copilotmd": CategoryFile,
	"csv":       CategoryFile,
	"tsv":       CategoryFile,
	"log":       CategoryFile,
	"json":      CategoryFile,
	"jsonc":     CategoryFile,
	"c":         CategoryFile,
	"cs":        CategoryFile,
	"cpp":       CategoryFile,
	"css":       CategoryFile,
	"drawio":    CategoryFile,
	"dmp":       CategoryFile,
	"html":      CategoryFile,
	"htm":       CategoryFile,
	"java":      CategoryFile,
	"js":        CategoryFile,
	"ipynb":     CategoryFile,
	"patch":     CategoryFile,
	"php":       CategoryFile,
	"py":        CategoryFile,
	"sh":        CategoryFile,
	"sql":       CategoryFile,
	"ts":        CategoryFile,
	"tsx":       CategoryFile,
	"xml":       CategoryFile,
	"yaml":      CategoryFile,
	"yml":       CategoryFile,
	"zip":       CategoryFile,
	"gz":        CategoryFile,
	"tgz":       CategoryFile,
	"debug":     CategoryFile,
	"msg":       CategoryFile,
	"eml":       CategoryFile,
	"mp3":       CategoryFile,
	"wav":       CategoryFile,
}

var contentTypeOverrides = map[string]string{
	"svg":       "image/svg+xml",
	"md":        "text/markdown",
	"copilotmd": "text/markdown",
	"jsonc":     "application/json",
	"drawio":    "application/xml",
	"ipynb":     "application/json",
	"patch":     "text/x-diff",
	"ts":        "text/typescript",
	"tsx":       "text/tsx",
	"yaml":      "application/yaml",
	"yml":       "application/yaml",
}

func ParseOutputFormat(raw string) (OutputFormat, error) {
	format := OutputFormat(strings.ToLower(strings.TrimSpace(raw)))
	switch format {
	case OutputFormatAuto, OutputFormatLink, OutputFormatURL, OutputFormatJSON:
		return format, nil
	default:
		return "", fmt.Errorf("unsupported format %q (expected auto, link, url, or json)", raw)
	}
}

func Inspect(path string) (File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return File{}, fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return File{}, fmt.Errorf("path %q is a directory", path)
	}

	name := filepath.Base(path)
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	category, ok := supportedExtensions[ext]
	if !ok {
		return File{}, fmt.Errorf("unsupported attachment type %q for %s", filepath.Ext(name), name)
	}

	contentType, err := detectContentType(path, ext)
	if err != nil {
		return File{}, err
	}

	return File{
		Path:        path,
		Name:        name,
		Extension:   ext,
		ContentType: contentType,
		Size:        info.Size(),
		Category:    category,
	}, nil
}

func Validate(file File) ([]string, error) {
	const mib = 1024 * 1024

	switch file.Category {
	case CategoryImage:
		if file.Size > 10*mib {
			return nil, fmt.Errorf("%s exceeds GitHub's 10MB image limit", file.Name)
		}
	case CategoryVideo:
		if file.Size > 100*mib {
			return nil, fmt.Errorf("%s exceeds GitHub's 100MB video limit", file.Name)
		}
		if file.Size > 10*mib {
			return []string{"video exceeds GitHub's 10MB free-plan limit and may require a paid plan plus collaborator access"}, nil
		}
	case CategoryFile:
		if file.Size > 25*mib {
			return nil, fmt.Errorf("%s exceeds GitHub's 25MB file limit", file.Name)
		}
	default:
		return nil, fmt.Errorf("unsupported attachment category %q", file.Category)
	}

	return nil, nil
}

func (r Result) Render(format OutputFormat) (string, error) {
	switch format {
	case OutputFormatAuto:
		if r.Category == CategoryImage {
			return fmt.Sprintf("![%s](%s)", escapeMarkdownLabel(r.Name), r.URL), nil
		}
		return r.URL, nil
	case OutputFormatLink:
		return fmt.Sprintf("[%s](%s)", escapeMarkdownLabel(r.Name), r.URL), nil
	case OutputFormatURL:
		return r.URL, nil
	case OutputFormatJSON:
		payload, err := json.Marshal(r)
		if err != nil {
			return "", fmt.Errorf("marshal result: %w", err)
		}
		return string(payload), nil
	default:
		return "", fmt.Errorf("unsupported format %q", format)
	}
}

func detectContentType(path, ext string) (string, error) {
	if contentType, ok := contentTypeOverrides[ext]; ok {
		return contentType, nil
	}

	if contentType := mime.TypeByExtension("." + ext); contentType != "" {
		return strings.TrimSpace(strings.Split(contentType, ";")[0]), nil
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for content type detection: %w", err)
	}
	defer file.Close()

	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read file for content type detection: %w", err)
	}

	contentType := http.DetectContentType(buffer[:n])
	return strings.TrimSpace(strings.Split(contentType, ";")[0]), nil
}

func escapeMarkdownLabel(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`)
	return replacer.Replace(value)
}
