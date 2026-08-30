package httpapi

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/uploadstore"
)

func TestR2UploadEndpointAbortsTruncatedStreams(t *testing.T) {
	t.Parallel()
	for _, framing := range []string{"length", "chunked"} {
		for _, status := range []int{http.StatusOK, http.StatusPartialContent} {
			t.Run(fmt.Sprintf("%s/%d", framing, status), func(t *testing.T) {
				t.Parallel()
				prefix := strings.Repeat("x", 4096)
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPut {
						_, _ = io.Copy(io.Discard, r.Body)
						w.WriteHeader(http.StatusNoContent)
						return
					}
					conn, rw, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					_, _ = fmt.Fprintf(rw, "HTTP/1.1 %d %s\r\n", status, http.StatusText(status))
					if status == http.StatusPartialContent {
						_, _ = rw.WriteString("Content-Range: bytes 0-8191/16384\r\n")
					}
					if framing == "length" {
						_, _ = rw.WriteString("Content-Length: 8192\r\n\r\n" + prefix)
					} else {
						_, _ = rw.WriteString("Transfer-Encoding: chunked\r\n\r\n1000\r\n" + prefix + "\r\n")
					}
					_ = rw.Flush()
				}))
				defer origin.Close()
				storage, err := uploadstore.NewR2(uploadstore.R2Config{Endpoint: origin.URL, Bucket: "synthetic-bucket", AccessKeyID: "synthetic-access", SecretAccessKey: "synthetic-secret"})
				if err != nil {
					t.Fatal(err)
				}
				st := newEmptyHTTPStore(t)
				owner, err := st.EnsureBootstrap(t.Context(), "Synthetic stream owner", "stream@example.com")
				if err != nil {
					t.Fatal(err)
				}
				workspaces, err := st.ListWorkspaces(t.Context(), owner.ID)
				if err != nil {
					t.Fatal(err)
				}
				api := httptest.NewServer(withHTTPDeadlines(New(st, realtime.NewHub(), Options{UploadStorage: storage}).Handler()))
				defer api.Close()
				upload := uploadFileAsUser(t, owner.ID, api.URL+"/api/uploads", workspaces[0].ID, "synthetic.txt", strings.Repeat("x", 8192))
				req, _ := http.NewRequest(http.MethodGet, api.URL+"/api/uploads/"+upload.ID, nil)
				req.Header.Set("X-ClickClack-User", owner.ID)
				if status == http.StatusPartialContent {
					req.Header.Set("Range", "bytes=0-8191")
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				if resp.StatusCode != status || err == nil || string(body) != prefix {
					t.Fatalf("truncated file must fail without JSON: status=%d bytes=%d error=%v suffix=%q", resp.StatusCode, len(body), err, body[min(len(body), len(prefix)):])
				}
			})
		}
	}
}

func TestHTTPBodyDeadlinePreservesProgressingResponses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(withHTTPDeadlines(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for range 36 {
			if _, err := w.Write(bytes.Repeat([]byte("x"), 256)); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Second):
			}
		}
	})))
	defer server.Close()
	resp, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || len(body) != 36*256 {
		t.Fatalf("bodyless GET interrupted despite progress: bytes=%d error=%v", len(body), err)
	}
}

func TestHTTPBodyDeadlineStillBoundsStalledRequestBodies(t *testing.T) {
	t.Parallel()
	readResult := make(chan error, 1)
	server := httptest.NewServer(withHTTPDeadlines(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		readResult <- err
		w.WriteHeader(http.StatusRequestTimeout)
	})))
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(40 * time.Second))
	started := time.Now()
	_, _ = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: synthetic\r\nContent-Length: 100\r\n\r\n")
	select {
	case err := <-readResult:
		if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() || time.Since(started) < 29*time.Second {
			t.Fatalf("stalled request-body read did not time out: elapsed=%s error=%v", time.Since(started), err)
		}
	case <-time.After(35 * time.Second):
		t.Fatal("stalled request-body read escaped its deadline")
	}
}
