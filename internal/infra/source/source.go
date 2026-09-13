// Package source resolves explicitly configured media sources; it never fetches metadata.
package source

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Result struct {
	URL                 string
	RequiredHttpHeaders map[string]string
}
type SourceResolver interface {
	Resolve(context.Context, string) (Result, error)
}
type Direct struct{ Headers map[string]string }

func ValidURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || strings.ContainsAny(raw, "\r\n") {
		return errors.New("source must be an absolute HTTP(S) URL without credentials")
	}
	return nil
}
func (d Direct) Resolve(ctx context.Context, raw string) (Result, error) {
	if err := ValidURL(raw); err != nil {
		return Result{}, err
	}
	headers := map[string]string{}
	for k, v := range d.Headers {
		if strings.ContainsAny(k+v, "\r\n") {
			return Result{}, errors.New("invalid source header")
		}
		headers[k] = v
	}
	return Result{raw, headers}, nil
}

// OpenList also supports Alist's documented fs/get protocol with user-owned API authorization.
type OpenList struct {
	BaseURL, Token string
	Client         *http.Client
}

func (a OpenList) Resolve(ctx context.Context, path string) (Result, error) {
	if err := ValidURL(a.BaseURL); err != nil {
		return Result{}, err
	}
	body, _ := json.Marshal(map[string]any{"path": path, "password": "", "refresh": false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(a.BaseURL, "/")+"/api/fs/get", strings.NewReader(string(body)))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", a.Token)
	req.Header.Set("Content-Type", "application/json")
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, errors.New("source resolver request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return Result{}, fmt.Errorf("source resolver status %d", resp.StatusCode)
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			RawURL string            `json:"raw_url"`
			Header map[string]string `json:"header"`
		} `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return Result{}, err
	}
	if payload.Code != 200 {
		return Result{}, errors.New("source resolver rejected request")
	}
	return (Direct{Headers: payload.Data.Header}).Resolve(ctx, payload.Data.RawURL)
}

// STRM accepts only regular files inside an explicitly allowed directory.
// Symlink resolution is checked for both the root and target before reading.
type STRM struct{ Root string }

func (s STRM) Resolve(ctx context.Context, path string) (Result, error) {
	if s.Root == "" {
		return Result{}, errors.New("STRM is disabled: configure playback.strm_root")
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return Result{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Result{}, err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return Result{}, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Result{}, errors.New("STRM path escapes configured root")
	}
	f, err := os.Open(path)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 65536 {
		return Result{}, errors.New("STRM must be a regular file no larger than 64 KiB")
	}
	scanner := bufio.NewScanner(io.LimitReader(f, 65537))
	raw := ""
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if raw != "" {
			return Result{}, errors.New("STRM must contain exactly one media URL")
		}
		raw = line
	}
	if scanner.Err() != nil {
		return Result{}, scanner.Err()
	}
	return (Direct{}).Resolve(ctx, raw)
}

type Entry struct {
	ID      string              `json:"Id"`
	Name    string              `json:"name"`
	Type    string              `json:"type"`
	Sources []map[string]string `json:"sources"`
}

func (s STRM) Scan(ctx context.Context) ([]Entry, error) {
	root, err := filepath.Abs(s.Root)
	if s.Root == "" || err != nil {
		return nil, errors.New("STRM root required")
	}
	entries := []Entry{}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".strm") {
			return nil
		}
		result, err := s.Resolve(ctx, path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		hash := sha256.Sum256([]byte(filepath.ToSlash(rel)))
		entries = append(entries, Entry{ID: hex.EncodeToString(hash[:16]), Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), Type: "Movie", Sources: []map[string]string{{"name": "STRM", "url": result.URL}}})
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, err
}
