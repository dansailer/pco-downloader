package pco

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const defaultBaseURL = "https://api.planningcenteronline.com"

// APIError is a non-2xx response from the Planning Center API.
type APIError struct {
	StatusCode int
	Method     string
	URL        string
	Detail     string
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("Planning Center %s: %s", e.Method, e.Detail)
	}
	return fmt.Sprintf("Planning Center %s %s: HTTP %d", e.Method, stripURL(e.URL), e.StatusCode)
}

// Client calls the Services API with a personal access token (HTTP Basic).
type Client struct {
	BaseURL string
	AppID   string
	Secret  string
	HTTP    *http.Client

	// transcode writes an MP3 to dest from a local temp file.
	// Nil uses ffmpeg, which is not given a network URL.
	transcode func(ctx context.Context, src, dest string) error

	// execCmd runs ffmpeg or yt-dlp. Nil uses the program on PATH.
	// YouTube is the only caller. The transcode path does not use it.
	execCmd func(ctx context.Context, name string, args []string) ([]byte, error)
}

// New returns a client for the public Planning Center API.
func New(appID, secret string) *Client {
	c := &Client{
		BaseURL: defaultBaseURL,
		AppID:   appID,
		Secret:  secret,
	}
	c.HTTP = c.newHTTPClient()
	return c
}

type resource struct {
	ID            string          `json:"id"`
	Attributes    json.RawMessage `json:"attributes"`
	Relationships map[string]struct {
		Data json.RawMessage `json:"data"`
	} `json:"relationships"`
}

// toOne reads a to-one relationship id. A null or empty relationship is not an id.
func (r resource) toOne(name string) (string, bool) {
	env, ok := r.Relationships[name]
	if !ok {
		return "", false
	}
	raw := bytes.TrimSpace(env.Data)
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var ref struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &ref); err != nil || ref.ID == "" {
		return "", false
	}
	return ref.ID, true
}

type listEnvelope struct {
	Data  []resource `json:"data"`
	Links struct {
		Next string `json:"next"`
	} `json:"links"`
	Errors []apiErrorBody `json:"errors"`
}

type apiErrorBody struct {
	Status string `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return defaultBaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP == nil {
		c.HTTP = c.newHTTPClient()
	}
	return c.HTTP
}

// list follows JSON:API pagination and returns every resource on the path.
func (c *Client) list(ctx context.Context, path string) ([]resource, error) {
	next, err := c.resolve(withPerPage(path))
	if err != nil {
		return nil, err
	}
	var all []resource
	for next != "" {
		body, err := c.do(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		var env listEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		all = append(all, env.Data...)
		if env.Links.Next == "" {
			break
		}
		next, err = c.resolve(env.Links.Next)
		if err != nil {
			return nil, err
		}
	}
	return all, nil
}

func (c *Client) getResource(ctx context.Context, path string) (resource, error) {
	raw, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return resource{}, err
	}
	var env struct {
		Data resource `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return resource{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if env.Data.ID == "" {
		return resource{}, fmt.Errorf("empty Planning Center response for GET %s", path)
	}
	return env.Data, nil
}

func (c *Client) do(ctx context.Context, method, path string, payload []byte) ([]byte, error) {
	endpoint, err := c.resolve(path)
	if err != nil {
		return nil, err
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.AppID, c.Secret)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Method:     method,
			URL:        endpoint,
			Detail:     errorDetail(resp.StatusCode, data),
		}
	}
	return data, nil
}

// resolve keeps API calls on the configured origin, including pagination links.
// The scheme and port must match. Cleartext is allowed only for a local API.
func (c *Client) resolve(ref string) (string, error) {
	base, err := url.Parse(c.base())
	if err != nil || base.Hostname() == "" {
		return "", errors.New("api base url is invalid")
	}
	if base.User != nil {
		return "", errors.New("refusing an API URL that contains credentials")
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("api url: %w", err)
	}
	if u.User != nil {
		return "", errors.New("refusing an API URL that contains credentials")
	}
	resolved := base.ResolveReference(u)
	if resolved.User != nil {
		return "", errors.New("refusing an API URL that contains credentials")
	}
	if !sameEndpoint(base, resolved) {
		return "", fmt.Errorf("refusing API link to %s", resolved.Host)
	}
	if resolved.Scheme != "https" && !c.localAPIHost(resolved.Hostname()) {
		return "", errors.New("refusing a non-HTTPS API URL")
	}
	return resolved.String(), nil
}

func withPerPage(path string) string {
	u, err := url.Parse(path)
	if err != nil {
		return path
	}
	q := u.Query()
	if q.Get("per_page") == "" {
		q.Set("per_page", "100")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func errorDetail(status int, body []byte) string {
	var env struct {
		Errors []apiErrorBody `json:"errors"`
	}
	if json.Unmarshal(body, &env) == nil && len(env.Errors) > 0 {
		var parts []string
		for _, e := range env.Errors {
			msg := strings.TrimSpace(e.Detail)
			if msg == "" {
				msg = strings.TrimSpace(e.Title)
			}
			if msg != "" {
				parts = append(parts, clip(msg, 180))
			}
		}
		if len(parts) > 0 {
			return fmt.Sprintf("HTTP %d: %s", status, strings.Join(parts, "; "))
		}
	}
	// The raw body is not included. It can echo a signed URL or a token.
	return fmt.Sprintf("HTTP %d", status)
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func decodeAttrs[T any](raw json.RawMessage) (T, error) {
	var out T
	if len(raw) == 0 {
		return out, nil
	}
	err := json.Unmarshal(raw, &out)
	return out, err
}

// openAttachment asks the API for a file URL. The returned URL is fetched
// without the API credential. Callers must not send the token to that host.
func (c *Client) openAttachment(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", errors.New("attachment id is empty")
	}
	// Action posts take a JSON:API document. This one has no attributes.
	const openBody = `{"data":{"type":"AttachmentActivity"}}`
	body, err := c.do(ctx, http.MethodPost, "/services/v2/attachments/"+url.PathEscape(id)+"/open", []byte(openBody))
	if err != nil {
		return "", err
	}
	return attachmentURL(body)
}

func attachmentURL(body []byte) (string, error) {
	var probe struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return "", fmt.Errorf("decode open response: %w", err)
	}
	raw := bytes.TrimSpace(probe.Data)
	if len(raw) == 0 || string(raw) == "null" {
		return "", errors.New("open response has no attachment URL")
	}
	type item struct {
		Attributes struct {
			AttachmentURL string `json:"attachment_url"`
		} `json:"attributes"`
	}
	if raw[0] == '[' {
		var list []item
		if err := json.Unmarshal(raw, &list); err != nil {
			return "", fmt.Errorf("decode open response: %w", err)
		}
		for _, it := range list {
			if it.Attributes.AttachmentURL != "" {
				return it.Attributes.AttachmentURL, nil
			}
		}
		return "", errors.New("open response has no attachment URL")
	}
	var one item
	if err := json.Unmarshal(raw, &one); err != nil {
		return "", fmt.Errorf("decode open response: %w", err)
	}
	if one.Attributes.AttachmentURL == "" {
		return "", errors.New("open response has no attachment URL")
	}
	return one.Attributes.AttachmentURL, nil
}
