package emby_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/require"
)

// resp 是一次 HTTP 调用的快照。
type resp struct {
	Status int
	Header http.Header
	Body   []byte
}

// JSON 把响应体解析成 map，便于断言字段结构。
func (r resp) JSON(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(r.Body, &out), "响应不是合法 JSON: %s", string(r.Body))
	return out
}

// Array 取出响应里的数组字段（如 Items）。
func (r resp) Array(t *testing.T, key string) []any {
	t.Helper()
	raw, ok := r.JSON(t)[key]
	require.True(t, ok, "响应缺少字段 %q: %s", key, string(r.Body))
	arr, ok := raw.([]any)
	require.True(t, ok, "字段 %q 不是数组: %s", key, string(r.Body))
	return arr
}

// api 是对测试服务的轻量封装：默认不跟随 302（播放链路要断言重定向目标）。
type api struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
}

func newAPI(t *testing.T) (api, testutil.Env) {
	t.Helper()

	env := testutil.Setup(t)
	return api{
		t:      t,
		server: testutil.NewTestServer(t, env.Cfg),
		client: &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, env
}

func (a api) do(method, path string, headers map[string]string, body []byte) resp {
	a.t.Helper()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, a.server.URL+path, reader)
	require.NoError(a.t, err)

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := a.client.Do(req)
	require.NoError(a.t, err)
	defer func() { _ = res.Body.Close() }()

	data, err := io.ReadAll(res.Body)
	require.NoError(a.t, err)

	return resp{Status: res.StatusCode, Header: res.Header, Body: data}
}

// get 带 X-Emby-Token 的 GET。token 为空表示匿名。
func (a api) get(path, token string) resp {
	return a.do(http.MethodGet, path, a.auth(token), nil)
}

func (a api) post(path, token string, body []byte) resp {
	return a.do(http.MethodPost, path, a.auth(token), body)
}

func (a api) delete(path, token string) resp {
	return a.do(http.MethodDelete, path, a.auth(token), nil)
}

func (a api) auth(token string) map[string]string {
	if token == "" {
		return nil
	}
	return map[string]string{"X-Emby-Token": token}
}
