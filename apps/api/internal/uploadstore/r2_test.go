package uploadstore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestR2SaveServeAndDelete(t *testing.T) {
	t.Parallel()
	var savedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Fatalf("missing sigv4 authorization for %s", r.Method)
		}
		if r.Header.Get("X-Amz-Content-Sha256") == "" || r.Header.Get("X-Amz-Date") == "" {
			t.Fatalf("missing signed r2 headers for %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/bucket/prefix/upload-") {
			t.Fatalf("unexpected object path %q", r.URL.Path)
		}
		switch r.Method {
		case http.MethodPut:
			if r.ContentLength != 8 {
				t.Fatalf("unexpected put content length: %d", r.ContentLength)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != "hello r2" || r.Header.Get("Content-Type") != "text/plain" {
				t.Fatalf("unexpected put body/header: %q %q", string(body), r.Header.Get("Content-Type"))
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if r.Header.Get("Range") == "bytes=99-100" {
				w.Header().Set("Content-Range", "bytes */8")
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			if r.Header.Get("Range") != "bytes=0-4" {
				t.Fatalf("expected range passthrough, got %q", r.Header.Get("Range"))
			}
			w.Header().Set("Content-Range", "bytes 0-4/8")
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("hello"))
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	store, err := NewR2(R2Config{
		AccountID:       "account",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		Bucket:          "bucket",
		Prefix:          "prefix",
		Endpoint:        server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save(context.Background(), strings.NewReader("hello r2"), SaveOptions{ContentType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	savedPath = saved.Path
	if saved.ByteSize != 8 || !strings.HasPrefix(saved.Path, "r2://bucket/prefix/upload-") {
		t.Fatalf("unexpected saved object: %#v", saved)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/uploads/upl_1", nil)
	req.Header.Set("Range", "bytes=0-4")
	if err := store.ServeHTTP(recorder, req, Object{Path: saved.Path, ContentType: "text/plain", ByteSize: saved.ByteSize}); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusPartialContent || recorder.Body.String() != "hello" {
		t.Fatalf("unexpected serve response: %d %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Type") != "text/plain" || recorder.Header().Get("Content-Range") != "bytes 0-4/8" {
		t.Fatalf("unexpected serve headers: %#v", recorder.Header())
	}
	recorder = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/uploads/upl_1", nil)
	req.Header.Set("Range", "bytes=99-100")
	if err := store.ServeHTTP(recorder, req, Object{Path: saved.Path, ContentType: "text/plain", ByteSize: saved.ByteSize}); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusRequestedRangeNotSatisfiable || recorder.Header().Get("Content-Range") != "bytes */8" {
		t.Fatalf("unexpected unsatisfiable range response: %d %#v", recorder.Code, recorder.Header())
	}
	if err := store.Delete(context.Background(), savedPath); err != nil {
		t.Fatal(err)
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
