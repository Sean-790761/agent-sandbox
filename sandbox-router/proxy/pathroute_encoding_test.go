// Copyright 2026 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"

	sandboxdserver "sigs.k8s.io/agent-sandbox/packages/sandboxd/pkg/server"
	"sigs.k8s.io/agent-sandbox/sandbox-router/cache"
	"sigs.k8s.io/agent-sandbox/sandbox-router/config"
)

// TestPathRoutingPreservesEncodedSlash is the end-to-end regression test
// for the escaping bug caught in review: r.URL.Path is already
// percent-decoded, so naively deriving the upstream remainder from it
// would turn a request for "/router/test/my-box/8080/some%2Ffile" into an
// upstream request for "/some/file" — two segments instead of one,
// silently renaming whatever resource "some%2Ffile" actually named (a
// filename containing "/", URL-encoded to keep it within a single path
// segment, is exactly the kind of thing a browser-facing tool like
// code-server can be asked to serve). ParsePathRoute/resolveTarget fix
// this by working from r.URL.EscapedPath() and setting the outbound URL's
// RawPath alongside Path; this test proves the fix holds through the
// actual httputil.ReverseProxy hop, not just the parser in isolation
// (which pathroute_test.go already covers).
//
// Deliberately NOT behind the "integration" build tag, unlike its
// siblings in this package: it needs nothing beyond two in-process
// httptest servers, same as pathroute_test.go's table tests, and
// dev/tools/test-unit (the only Go test job wired into CI here — there is
// no separate integration presubmit) runs without -tags=integration. A
// regression this specific is worth keeping under the suite that
// actually runs.
func TestPathRoutingPreservesEncodedSlash(t *testing.T) {
	var (
		mu             sync.Mutex
		gotEscapedPath string
	)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotEscapedPath = r.URL.EscapedPath()
		mu.Unlock()
		w.WriteHeader(204)
	}))
	defer backend.Close()
	bu, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend: %v", err)
	}

	cfg := config.Defaults()
	cfg.PathRoutingPrefix = "/router"
	lookup := &stubLookup{entries: map[types.UID]cache.Entry{
		"path-routed-uid": {PodIP: bu.Hostname(), SandboxName: "my-box", Namespace: "test"},
	}}
	router := httptest.NewServer(NewHandler(Options{
		Config: &cfg,
		Cache:  lookup,
		Logger: logr.Discard(),
	}))
	defer router.Close()

	resp, err := http.Get(router.URL + "/router/test/my-box/" + bu.Port() + "/some%2Ffile")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("status: got %d want 204", resp.StatusCode)
	}

	mu.Lock()
	got := gotEscapedPath
	mu.Unlock()
	const want = "/some%2Ffile"
	if got != want {
		t.Fatalf("backend saw escaped path %q, want %q (the encoded slash must survive the hop)", got, want)
	}
}

func TestHeaderRoutingPreservesSandboxdFilePaths(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(filepath.Join(root, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"a.txt":          "root file",
		"dir/child.txt":  "child file",
		"literal%2F.txt": "literal percent",
		"café space.txt": "unicode file",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(base, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside root"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside-link")); err != nil {
		t.Fatal(err)
	}
	daemon, err := sandboxdserver.New(sandboxdserver.Options{RootDir: root, Log: logr.Discard()})
	if err != nil {
		t.Fatalf("new sandboxd: %v", err)
	}
	requests := make(chan string, 1)
	restHandler := daemon.RESTHandler()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.EscapedPath()
		restHandler.ServeHTTP(w, r)
	}))
	defer backend.Close()
	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}
	cfg := config.Defaults()
	cfg.AllowLoopbackPodIP = true
	lookup := &stubLookup{entries: map[types.UID]cache.Entry{
		"file-path-uid": {PodIP: backendURL.Hostname(), SandboxName: "my-box", Namespace: "test"},
	}}
	router := httptest.NewServer(NewHandler(Options{Config: &cfg, Cache: lookup, Logger: logr.Discard()}))
	defer router.Close()
	client := router.Client()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

	// The Python SDK keeps file-path slashes and dot segments escaped so
	// HTTP routing does not interpret them before sandboxd resolves the path.
	tests := []struct {
		name      string
		method    string
		tail      string
		status    int
		body      string
		listEntry string
	}{
		{name: "dot root", tail: "", status: http.StatusOK, listEntry: "a.txt"},
		{name: "slash root", tail: "%2F", status: http.StatusOK, listEntry: "a.txt"},
		{name: "root relative directory", tail: "%2Fdir", status: http.StatusOK, listEntry: "child.txt"},
		{name: "leading dot", tail: "%2E%2Fa.txt", status: http.StatusOK, body: "root file"},
		{name: "parent segment", tail: "dir%2F%2E%2E%2Fa.txt", status: http.StatusOK, body: "root file"},
		{name: "repeated slash", tail: "dir%2F%2Fchild.txt", status: http.StatusOK, body: "child file"},
		{name: "absolute file", tail: url.PathEscape(filepath.Join(root, "a.txt")), status: http.StatusOK, body: "root file"},
		{name: "literal encoded slash", tail: "literal%252F.txt", status: http.StatusOK, body: "literal percent"},
		{name: "unicode and space", tail: "caf%C3%A9%20space.txt", status: http.StatusOK, body: "unicode file"},
		{name: "head leading dot", method: http.MethodHead, tail: "%2E%2Fa.txt", status: http.StatusOK},
		{name: "parent escape", tail: "%2E%2E%2Foutside.txt", status: http.StatusForbidden},
		{name: "nested parent escape", tail: "dir%2F%2E%2E%2F%2E%2E%2Foutside.txt", status: http.StatusForbidden},
		{name: "symlink escape", tail: "outside-link", status: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			method := test.method
			if method == "" {
				method = http.MethodGet
			}
			endpoint := "/v1/files/" + test.tail
			req, err := http.NewRequest(method, router.URL+endpoint, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set(HeaderSandboxID, "my-box")
			req.Header.Set(HeaderSandboxNamespace, "test")
			req.Header.Set(HeaderSandboxUID, "file-path-uid")
			req.Header.Set(HeaderSandboxPort, backendURL.Port())
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request through router: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			var upstreamPath string
			select {
			case upstreamPath = <-requests:
			default:
				t.Fatal("request did not reach sandboxd")
			}
			if resp.StatusCode != test.status {
				t.Fatalf("status: got %d want %d, body %s", resp.StatusCode, test.status, body)
			}
			if upstreamPath != endpoint {
				t.Fatalf("upstream path: got %q want %q", upstreamPath, endpoint)
			}
			if test.listEntry != "" {
				var listing sandboxdserver.DirectoryListing
				if err := json.Unmarshal(body, &listing); err != nil {
					t.Fatalf("decode listing: %v", err)
				}
				for _, entry := range listing.Entries {
					if entry.Name == test.listEntry {
						return
					}
				}
				t.Fatalf("listing lacks %q: %s", test.listEntry, body)
			}
			if test.status == http.StatusOK && string(body) != test.body {
				t.Fatalf("file content: got %q want %q", body, test.body)
			}
		})
	}
}
