package uploadstore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestR2SaveServeAndDelete(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, prefix, storedPrefix, requestPrefix, legacyPath string
	}{
		{"plain", "prefix", "r2://bucket/prefix/", "/bucket/prefix/", "r2://bucket/prefix/upload-existing"},
		{"space", "team files", "r2://bucket/team%20files/", "/bucket/team%2520files/", "r2://bucket/team files/upload-existing"},
		{"percent", "100%", "r2://bucket/100%25/", "/bucket/100%2525/", ""},
		{"escaped percent", "literal%20", "r2://bucket/literal%2520/", "/bucket/literal%252520/", ""},
		{"fragment", "team#files", "r2://bucket/team%23files/", "/bucket/team%2523files/", ""},
		{"query", "team?files", "r2://bucket/team%3Ffiles/", "/bucket/team%253Ffiles/", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			objects := make(map[string][]byte)
			if tc.legacyPath != "" {
				objects[tc.requestPrefix+"upload-existing"] = []byte("hello r2")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
					t.Errorf("missing sigv4 authorization for %s", r.Method)
				}
				if r.Header.Get("X-Amz-Content-Sha256") == "" || r.Header.Get("X-Amz-Date") == "" {
					t.Errorf("missing signed r2 headers for %s", r.Method)
				}
				// Existing R2 objects use this wire encoding, including escaped prefixes.
				if !strings.HasPrefix(r.RequestURI, tc.requestPrefix+"upload-") {
					t.Errorf("unexpected object request URI %q", r.RequestURI)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				switch r.Method {
				case http.MethodPut:
					if r.ContentLength != 8 {
						t.Errorf("unexpected put content length: %d", r.ContentLength)
					}
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if string(body) != "hello r2" || r.Header.Get("Content-Type") != "text/plain" {
						t.Errorf("unexpected put body/header: %q %q", string(body), r.Header.Get("Content-Type"))
					}
					objects[r.RequestURI] = body
					w.WriteHeader(http.StatusOK)
				case http.MethodGet:
					body, ok := objects[r.RequestURI]
					if !ok {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					if r.Header.Get("Range") == "bytes=99-100" {
						w.Header().Set("Content-Range", "bytes */8")
						w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
						return
					}
					if r.Header.Get("Range") != "bytes=0-4" {
						t.Errorf("expected range passthrough, got %q", r.Header.Get("Range"))
					}
					w.Header().Set("Content-Range", "bytes 0-4/8")
					w.Header().Set("Accept-Ranges", "bytes")
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write(body[:5])
				case http.MethodDelete:
					delete(objects, r.RequestURI)
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected method %s", r.Method)
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			}))
			t.Cleanup(server.Close)

			store, err := NewR2(R2Config{
				AccountID:       "account",
				AccessKeyID:     "access",
				SecretAccessKey: "secret",
				Bucket:          "bucket",
				Prefix:          tc.prefix,
				Endpoint:        server.URL,
			})
			if err != nil {
				t.Fatal(err)
			}
			saved, err := store.Save(context.Background(), strings.NewReader("hello r2"), SaveOptions{ContentType: "text/plain"})
			if err != nil {
				t.Fatal(err)
			}
			if saved.ByteSize != 8 || !strings.HasPrefix(saved.Path, tc.storedPrefix+"upload-") {
				t.Errorf("unexpected saved object: %#v", saved)
			}
			paths := []string{saved.Path}
			if tc.legacyPath != "" {
				paths = append(paths, tc.legacyPath)
			}
			for _, path := range paths {
				recorder := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, "/api/uploads/upl_1", nil)
				req.Header.Set("Range", "bytes=0-4")
				if err := store.ServeHTTP(recorder, req, Object{Path: path, ContentType: "text/plain", ByteSize: saved.ByteSize}); err != nil {
					t.Fatalf("download saved upload %q: %v", path, err)
				}
				if recorder.Code != http.StatusPartialContent || recorder.Body.String() != "hello" {
					t.Fatalf("unexpected serve response: %d %q", recorder.Code, recorder.Body.String())
				}
				if recorder.Header().Get("Content-Type") != "text/plain" || recorder.Header().Get("Content-Range") != "bytes 0-4/8" {
					t.Fatalf("unexpected serve headers: %#v", recorder.Header())
				}

				recorder = httptest.NewRecorder()
				req.Header.Set("Range", "bytes=99-100")
				if err := store.ServeHTTP(recorder, req, Object{Path: path, ContentType: "text/plain", ByteSize: saved.ByteSize}); err != nil {
					t.Fatal(err)
				}
				if recorder.Code != http.StatusRequestedRangeNotSatisfiable || recorder.Header().Get("Content-Range") != "bytes */8" {
					t.Fatalf("unexpected unsatisfiable range response: %d %#v", recorder.Code, recorder.Header())
				}
				if err := store.Delete(context.Background(), path); err != nil {
					t.Fatalf("delete saved upload %q: %v", path, err)
				}
				recorder = httptest.NewRecorder()
				if err := store.ServeHTTP(recorder, req, Object{Path: path}); !errors.Is(err, ErrNotFound) || recorder.Body.Len() != 0 {
					t.Fatalf("deleted object must fail before writing file bytes: %v", err)
				}
			}
			if err := store.Delete(context.Background(), tc.prefix+"/upload-missing"); err != nil {
				t.Fatalf("deleting a missing object must succeed: %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(objects) != 0 {
				t.Fatalf("uploads left in storage after deletion: %d", len(objects))
			}
		})
	}
}

func TestR2ConfigValidation(t *testing.T) {
	t.Parallel()
	if _, err := NewR2(R2Config{AccessKeyID: "access", SecretAccessKey: "secret", Bucket: "bucket"}); err == nil {
		t.Fatal("expected missing account id or endpoint error")
	}
	if _, err := NewR2(R2Config{AccountID: "account", SecretAccessKey: "secret", Bucket: "bucket"}); err == nil {
		t.Fatal("expected missing access key error")
	}
	if _, err := NewR2(R2Config{AccountID: "account", AccessKeyID: "access", Bucket: "bucket"}); err == nil {
		t.Fatal("expected missing secret error")
	}
	if _, err := NewR2(R2Config{AccountID: "account", AccessKeyID: "access", SecretAccessKey: "secret"}); err == nil {
		t.Fatal("expected missing bucket error")
	}
	store, err := NewR2(R2Config{AccountID: "account", AccessKeyID: "access", SecretAccessKey: "secret", Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	if store.httpClient == nil || store.httpClient.Timeout != 0 {
		t.Fatalf("expected streaming-safe client timeout, got %#v", store.httpClient)
	}
	customClient := &http.Client{}
	store, err = NewR2(R2Config{AccountID: "account", AccessKeyID: "access", SecretAccessKey: "secret", Bucket: "bucket", HTTPClient: customClient})
	if err != nil {
		t.Fatal(err)
	}
	if store.httpClient != customClient {
		t.Fatal("expected custom client to be preserved")
	}
}

func TestR2SaveEmptyUploadUsesContentLength(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			t.Fatalf("expected content-length zero, got length=%d transfer=%v", r.ContentLength, r.TransferEncoding)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "" {
			t.Fatalf("unexpected body %q", string(body))
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	store, err := NewR2(R2Config{
		AccountID:       "account",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		Bucket:          "bucket",
		Endpoint:        server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save(context.Background(), strings.NewReader(""), SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ByteSize != 0 {
		t.Fatalf("unexpected byte size %d", saved.ByteSize)
	}
}

func TestR2RejectsKeysOutsidePrefix(t *testing.T) {
	t.Parallel()
	store, err := NewR2(R2Config{
		AccountID:       "account",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		Bucket:          "bucket",
		Prefix:          "prefix",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, objectPath := range []string{"r2://bucket/other/upload-1", "other/upload-1"} {
		if _, err := store.keyFromPath(objectPath); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected %q outside prefix to be rejected, got %v", objectPath, err)
		}
	}
	key, err := store.keyFromPath("r2://bucket/prefix/upload-1")
	if err != nil {
		t.Fatal(err)
	}
	if key != "prefix/upload-1" {
		t.Fatalf("unexpected key: %q", key)
	}
}
