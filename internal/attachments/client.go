package attachments

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://github.com"
	userAgent      = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"
)

type Client struct {
	httpClient *http.Client
	s3Client   *http.Client
	baseURL    string
	userAgent  string
}

type policyResponse struct {
	UploadURL      string `json:"upload_url"`
	AssetUploadURL string `json:"asset_upload_url"`
	Asset          struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Size        int64  `json:"size"`
		ContentType string `json:"content_type"`
		Href        string `json:"href"`
	} `json:"asset"`
	Form                         map[string]string `json:"form"`
	AssetUploadAuthenticityToken string            `json:"asset_upload_authenticity_token"`
}

func NewClient(cookies []*http.Cookie) *Client {
	jar, _ := cookiejar.New(nil)
	ghURL, _ := url.Parse(defaultBaseURL)
	jar.SetCookies(ghURL, cookies)

	return &Client{
		httpClient: &http.Client{Jar: jar, Timeout: 30 * time.Second},
		s3Client:   &http.Client{Timeout: 120 * time.Second},
		baseURL:    defaultBaseURL,
		userAgent:  userAgent,
	}
}

func (c *Client) Upload(owner, repo string, repoID int, file File) (*Result, error) {
	uploadToken, err := c.getUploadToken(owner, repo)
	if err != nil {
		return nil, fmt.Errorf("step 0 (get upload token): %w", err)
	}

	policy, err := c.requestPolicy(owner, repo, uploadToken, repoID, file)
	if err != nil {
		return nil, fmt.Errorf("step 1 (request policy): %w", err)
	}

	if err := c.uploadToS3(policy, file); err != nil {
		return nil, fmt.Errorf("step 2 (S3 upload): %w", err)
	}

	result, err := c.finalizeUpload(owner, repo, policy, file)
	if err != nil {
		return nil, fmt.Errorf("step 3 (finalize): %w", err)
	}

	return result, nil
}

func (c *Client) getUploadToken(owner, repo string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, c.repoURL(owner, repo), nil)
	if err != nil {
		return "", fmt.Errorf("creating repo request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching repo page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("repo page returned %d — do you have access to %s/%s?", resp.StatusCode, owner, repo)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading repo page: %w", err)
	}

	const marker = `"uploadToken":"`
	start := strings.Index(string(body), marker)
	if start == -1 {
		return "", fmt.Errorf("uploadToken not found on repo page — do you have write access to %s/%s?", owner, repo)
	}
	start += len(marker)
	end := strings.Index(string(body[start:]), `"`)
	if end == -1 {
		return "", fmt.Errorf("uploadToken was present but malformed on repo page")
	}

	return string(body[start : start+end]), nil
}

func (c *Client) requestPolicy(owner, repo, uploadToken string, repoID int, file File) (*policyResponse, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	fields := []struct {
		key   string
		value string
	}{
		{key: "name", value: file.Name},
		{key: "size", value: strconv.FormatInt(file.Size, 10)},
		{key: "content_type", value: file.ContentType},
		{key: "authenticity_token", value: uploadToken},
		{key: "repository_id", value: strconv.Itoa(repoID)},
	}

	for _, field := range fields {
		if err := writer.WriteField(field.key, field.value); err != nil {
			return nil, fmt.Errorf("writing form field %s: %w", field.key, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/upload/policies/assets", body)
	if err != nil {
		return nil, fmt.Errorf("creating policy request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("Referer", c.repoURL(owner, repo))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting upload policy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("expected 201, got %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var policy policyResponse
	if err := json.NewDecoder(resp.Body).Decode(&policy); err != nil {
		return nil, fmt.Errorf("decoding policy response: %w", err)
	}

	if policy.UploadURL == "" {
		return nil, fmt.Errorf("policy response missing upload_url")
	}
	if policy.AssetUploadAuthenticityToken == "" {
		return nil, fmt.Errorf("policy response missing asset_upload_authenticity_token")
	}
	if len(policy.Form) == 0 {
		return nil, fmt.Errorf("policy response missing form fields")
	}
	if policy.Asset.ID == 0 {
		return nil, fmt.Errorf("policy response missing asset ID")
	}

	return &policy, nil
}

func (c *Client) uploadToS3(policy *policyResponse, file File) error {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	fieldOrder := []string{
		"key",
		"acl",
		"policy",
		"X-Amz-Algorithm",
		"X-Amz-Credential",
		"X-Amz-Date",
		"X-Amz-Signature",
		"Content-Type",
		"Cache-Control",
		"x-amz-meta-Surrogate-Control",
	}

	written := make(map[string]bool, len(fieldOrder))
	for _, key := range fieldOrder {
		value, ok := policy.Form[key]
		if !ok {
			continue
		}
		if err := writer.WriteField(key, value); err != nil {
			return fmt.Errorf("writing form field %s: %w", key, err)
		}
		written[key] = true
	}

	for key, value := range policy.Form {
		if written[key] {
			continue
		}
		if err := writer.WriteField(key, value); err != nil {
			return fmt.Errorf("writing form field %s: %w", key, err)
		}
	}

	part, err := writer.CreateFormFile("file", file.Name)
	if err != nil {
		return fmt.Errorf("creating file field: %w", err)
	}

	input, err := os.Open(file.Path)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer input.Close()

	if _, err := io.Copy(part, input); err != nil {
		return fmt.Errorf("writing file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, policy.UploadURL, body)
	if err != nil {
		return fmt.Errorf("creating S3 upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.s3Client.Do(req)
	if err != nil {
		return fmt.Errorf("S3 upload request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("S3 returned %d: %s", resp.StatusCode, truncate(string(respBody), 300))
	}

	return nil
}

func (c *Client) finalizeUpload(owner, repo string, policy *policyResponse, file File) (*Result, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("authenticity_token", policy.AssetUploadAuthenticityToken); err != nil {
		return nil, fmt.Errorf("writing form field authenticity_token: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, c.assetUploadURL(policy), body)
	if err != nil {
		return nil, fmt.Errorf("creating finalize request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("Referer", c.repoURL(owner, repo))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("finalizing upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("expected 200, got %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var payload struct {
		Href string `json:"href"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decoding finalize response: %w", err)
	}

	name := payload.Name
	if name == "" {
		name = file.Name
	}

	return &Result{
		URL:         payload.Href,
		Name:        name,
		ContentType: file.ContentType,
		Size:        file.Size,
		Category:    file.Category,
	}, nil
}

func (c *Client) assetUploadURL(policy *policyResponse) string {
	if strings.TrimSpace(policy.AssetUploadURL) == "" {
		return fmt.Sprintf("%s/upload/assets/%d", c.baseURL, policy.Asset.ID)
	}

	base, err := url.Parse(c.baseURL)
	if err != nil {
		return policy.AssetUploadURL
	}
	ref, err := url.Parse(policy.AssetUploadURL)
	if err != nil {
		return policy.AssetUploadURL
	}

	return base.ResolveReference(ref).String()
}

func (c *Client) repoURL(owner, repo string) string {
	return fmt.Sprintf("%s/%s/%s", strings.TrimRight(c.baseURL, "/"), owner, repo)
}

func truncate(value string, maxLen int) string {
	runes := []rune(value)
	if len(runes) <= maxLen {
		return value
	}
	return string(runes[:maxLen]) + "..."
}
