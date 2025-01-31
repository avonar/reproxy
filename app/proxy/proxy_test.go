package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/reproxy/app/discovery"
	"github.com/umputun/reproxy/app/discovery/provider"
	"github.com/umputun/reproxy/app/mgmt"
)

func TestHttp_Do(t *testing.T) {
	port := rand.Intn(10000) + 40000
	h := Http{Timeouts: Timeouts{ResponseHeader: 200 * time.Millisecond}, Address: fmt.Sprintf("127.0.0.1:%d", port),
		AccessLog: io.Discard, Signature: true, ProxyHeaders: []string{"hh1:vv1", "hh2:vv2"}, StdOutEnabled: true,
		Reporter: &ErrorReporter{Nice: true}}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("req: %v", r)
		w.Header().Add("h1", "v1")
		require.Equal(t, "127.0.0.1", r.Header.Get("X-Real-IP"))
		fmt.Fprintf(w, "response %s", r.URL.String())
	}))

	svc := discovery.NewService([]discovery.Provider{
		&provider.Static{Rules: []string{
			"localhost,^/api/(.*)," + ds.URL + "/123/$1,",
			"127.0.0.1,^/api/(.*)," + ds.URL + "/567/$1,",
		},
		}}, time.Millisecond*10)

	go func() {
		_ = svc.Run(context.Background())
	}()

	time.Sleep(50 * time.Millisecond)
	h.Matcher = svc
	h.Metrics = mgmt.NewMetrics()

	go func() {
		_ = h.Run(ctx)
	}()
	time.Sleep(10 * time.Millisecond)

	client := http.Client{}

	{
		req, err := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(port)+"/api/something", nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		t.Logf("%+v", resp.Header)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "response /567/something", string(body))
		assert.Equal(t, "reproxy", resp.Header.Get("App-Name"))
		assert.Equal(t, "v1", resp.Header.Get("h1"))
		assert.Equal(t, "vv1", resp.Header.Get("hh1"))
		assert.Equal(t, "vv2", resp.Header.Get("hh2"))
	}

	{
		resp, err := client.Get("http://localhost:" + strconv.Itoa(port) + "/api/something")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		t.Logf("%+v", resp.Header)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "response /123/something", string(body))
		assert.Equal(t, "reproxy", resp.Header.Get("App-Name"))
		assert.Equal(t, "v1", resp.Header.Get("h1"))
	}

	{
		resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/bad/something")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(b), "Sorry for the inconvenience")
		assert.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))
	}
}

func TestHttp_DoWithAssets(t *testing.T) {
	port := rand.Intn(10000) + 40000
	cc := NewCacheControl(time.Hour * 12)
	h := Http{Timeouts: Timeouts{ResponseHeader: 200 * time.Millisecond}, Address: fmt.Sprintf("127.0.0.1:%d", port),
		AccessLog: io.Discard, AssetsWebRoot: "/static", AssetsLocation: "testdata", CacheControl: cc, Reporter: &ErrorReporter{Nice: false}}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("req: %v", r)
		w.Header().Add("h1", "v1")
		require.Equal(t, "127.0.0.1", r.Header.Get("X-Real-IP"))
		fmt.Fprintf(w, "response %s", r.URL.String())
	}))

	svc := discovery.NewService([]discovery.Provider{
		&provider.Static{Rules: []string{
			"localhost,^/api/(.*)," + ds.URL + "/123/$1,",
			"127.0.0.1,^/api/(.*)," + ds.URL + "/567/$1,",
		},
		}}, time.Millisecond*10)

	go func() {
		_ = svc.Run(context.Background())
	}()
	time.Sleep(50 * time.Millisecond)
	h.Matcher = svc
	h.Metrics = mgmt.NewMetrics()

	go func() {
		_ = h.Run(ctx)
	}()
	time.Sleep(10 * time.Millisecond)

	client := http.Client{}

	{
		req, err := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(port)+"/api/something", nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		t.Logf("%+v", resp.Header)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "response /567/something", string(body))
		assert.Equal(t, "", resp.Header.Get("App-Name"))
		assert.Equal(t, "v1", resp.Header.Get("h1"))
	}

	{
		resp, err := client.Get("http://localhost:" + strconv.Itoa(port) + "/static/1.html")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		t.Logf("%+v", resp.Header)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "test html", string(body))
		assert.Equal(t, "", resp.Header.Get("App-Name"))
		assert.Equal(t, "", resp.Header.Get("h1"))
		assert.Equal(t, "public, max-age=43200", resp.Header.Get("Cache-Control"))
	}

	{
		resp, err := client.Get("http://localhost:" + strconv.Itoa(port) + "/svcbad")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), "Server error")
		assert.Equal(t, "text/plain; charset=utf-8", resp.Header.Get("Content-Type"))
	}

}

func TestHttp_DoWithAssetRules(t *testing.T) {
	port := rand.Intn(10000) + 40000
	cc := NewCacheControl(time.Hour * 12)
	h := Http{Timeouts: Timeouts{ResponseHeader: 200 * time.Millisecond}, Address: fmt.Sprintf("127.0.0.1:%d", port),
		AccessLog: io.Discard, CacheControl: cc, Reporter: &ErrorReporter{}}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("req: %v", r)
		w.Header().Add("h1", "v1")
		require.Equal(t, "127.0.0.1", r.Header.Get("X-Real-IP"))
		fmt.Fprintf(w, "response %s", r.URL.String())
	}))

	svc := discovery.NewService([]discovery.Provider{
		&provider.Static{Rules: []string{
			"localhost,^/api/(.*)," + ds.URL + "/123/$1,",
			"127.0.0.1,^/api/(.*)," + ds.URL + "/567/$1,",
			"*,/web,assets:testdata,",
		},
		}}, time.Millisecond*10)

	go func() {
		_ = svc.Run(context.Background())
	}()
	time.Sleep(50 * time.Millisecond)
	h.Matcher = svc
	h.Metrics = mgmt.NewMetrics()

	go func() {
		_ = h.Run(ctx)
	}()
	time.Sleep(10 * time.Millisecond)

	client := http.Client{}

	{
		req, err := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(port)+"/api/something", nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		t.Logf("%+v", resp.Header)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "response /567/something", string(body))
		assert.Equal(t, "", resp.Header.Get("App-Name"))
		assert.Equal(t, "v1", resp.Header.Get("h1"))
	}

	{
		resp, err := client.Get("http://localhost:" + strconv.Itoa(port) + "/web/1.html")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		t.Logf("%+v", resp.Header)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "test html", string(body))
		assert.Equal(t, "", resp.Header.Get("App-Name"))
		assert.Equal(t, "", resp.Header.Get("h1"))
		assert.Equal(t, "public, max-age=43200", resp.Header.Get("Cache-Control"))
	}
}

func TestHttp_DoLimitedReq(t *testing.T) {
	port := rand.Intn(10000) + 40000
	h := Http{Timeouts: Timeouts{ResponseHeader: 200 * time.Millisecond}, Address: fmt.Sprintf("127.0.0.1:%d", port),
		AccessLog: io.Discard, Signature: true, ProxyHeaders: []string{"hh1:vv1", "hh2:vv2"}, StdOutEnabled: true,
		Reporter: &ErrorReporter{Nice: true}, MaxBodySize: 10}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("req: %v", r)
		w.Header().Add("h1", "v1")
		require.Equal(t, "127.0.0.1", r.Header.Get("X-Real-IP"))
		fmt.Fprintf(w, "response %s", r.URL.String())
	}))

	svc := discovery.NewService([]discovery.Provider{
		&provider.Static{Rules: []string{
			"localhost,^/api/(.*)," + ds.URL + "/123/$1,",
			"127.0.0.1,^/api/(.*)," + ds.URL + "/567/$1,",
		},
		}}, time.Millisecond*10)

	go func() {
		_ = svc.Run(context.Background())
	}()

	time.Sleep(50 * time.Millisecond)
	h.Matcher, h.Metrics = svc, mgmt.NewMetrics()

	go func() {
		_ = h.Run(ctx)
	}()
	time.Sleep(10 * time.Millisecond)

	client := http.Client{}

	{
		req, err := http.NewRequest("POST", "http://127.0.0.1:"+strconv.Itoa(port)+"/api/something", bytes.NewBufferString("abcdefg"))
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		t.Logf("%+v", resp.Header)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "response /567/something", string(body))
		assert.Equal(t, "reproxy", resp.Header.Get("App-Name"))
		assert.Equal(t, "v1", resp.Header.Get("h1"))
		assert.Equal(t, "vv1", resp.Header.Get("hh1"))
		assert.Equal(t, "vv2", resp.Header.Get("hh2"))
	}

	{
		req, err := http.NewRequest("POST", "http://127.0.0.1:"+strconv.Itoa(port)+"/api/something", bytes.NewBufferString("abcdefg1234567"))
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
	}
}

func TestHttp_toHttp(t *testing.T) {

	tbl := []struct {
		addr string
		port int
		res  string
	}{
		{"localhost:1234", 80, "localhost:80"},
		{"m.example.com:443", 8080, "m.example.com:8080"},
		{"192.168.1.1:1443", 8080, "192.168.1.1:8080"},
	}

	h := Http{}
	for i, tt := range tbl {
		tt := tt
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			assert.Equal(t, tt.res, h.toHTTP(tt.addr, tt.port))
		})
	}

}

func TestHttp_isAssetRequest(t *testing.T) {
	tbl := []struct {
		req            string
		assetsLocation string
		assetsWebRoot  string
		res            bool
	}{
		{"/static/123.html", "/tmp", "/static", true},
		{"/static/123.html", "/tmp", "/static/", true},
		{"/static", "/tmp", "/static", true},
		{"/static/", "/tmp", "/static", true},
		{"/bad/", "/tmp", "/static", false},
		{"/static/", "", "/static", false},
		{"/static/", "/tmp", "", false},
	}

	for i, tt := range tbl {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			h := Http{AssetsLocation: tt.assetsLocation, AssetsWebRoot: tt.assetsWebRoot}
			r, err := http.NewRequest("GET", tt.req, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.res, h.isAssetRequest(r))
		})
	}

}
