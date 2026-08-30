package uploadstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const defaultR2ResponseHeaderTimeout = 30 * time.Second

type R2Config struct {
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	Prefix          string
	Endpoint        string
	Region          string
	HTTPClient      *http.Client
}

type R2 struct {
	accountID       string
	accessKeyID     string
	secretAccessKey string
	bucket          string
	prefix          string
	endpoint        string
	region          string
	httpClient      *http.Client
}

func NewR2(cfg R2Config) (*R2, error) {
	if cfg.AccountID == "" && cfg.Endpoint == "" {
		return nil, errors.New("r2 account id is required")
	}
	if cfg.AccessKeyID == "" {
		return nil, errors.New("r2 access key id is required")
	}
	if cfg.SecretAccessKey == "" {
		return nil, errors.New("r2 secret access key is required")
	}
	if cfg.Bucket == "" {
		return nil, errors.New("r2 bucket is required")
	}
	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	if endpoint == "" {
		endpoint = "https://" + cfg.AccountID + ".r2.cloudflarestorage.com"
	}
	region := cfg.Region
	if region == "" {
		region = "auto"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = defaultR2HTTPClient()
	}
	prefix := strings.Trim(cfg.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	return &R2{
		accountID:       cfg.AccountID,
		accessKeyID:     cfg.AccessKeyID,
		secretAccessKey: cfg.SecretAccessKey,
		bucket:          cfg.Bucket,
		prefix:          prefix,
		endpoint:        endpoint,
		region:          region,
		httpClient:      client,
	}, nil
}

func defaultR2HTTPClient() *http.Client {
	return &http.Client{Transport: r2Transport{}}
}

func (s *R2) Save(ctx context.Context, body io.Reader, options SaveOptions) (SavedObject, error) {
	tmp, err := os.CreateTemp("", "clickclack-r2-*")
	if err != nil {
		return SavedObject{}, err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	hasher := sha256.New()
	size, err := io.Copy(tmp, io.TeeReader(body, hasher))
	if err != nil {
		return SavedObject{}, err
	}
	payloadHash := hex.EncodeToString(hasher.Sum(nil))
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return SavedObject{}, err
	}
	key, err := randomKey(s.prefix)
	if err != nil {
		return SavedObject{}, err
	}
	var reqBody io.Reader = tmp
	if size == 0 {
		reqBody = bytes.NewReader(nil)
	}
	req, err := s.newRequest(ctx, http.MethodPut, key, reqBody)
	if err != nil {
		return SavedObject{}, err
	}
	req.ContentLength = size
	if options.ContentType != "" {
		req.Header.Set("Content-Type", options.ContentType)
	}
	s.signHash(req, payloadHash)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return SavedObject{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SavedObject{}, responseError("save r2 upload", resp)
	}
	return SavedObject{Path: s.objectPath(key), ByteSize: size}, nil
}

func (s *R2) Delete(ctx context.Context, objectPath string) error {
	key, err := s.keyFromPath(objectPath)
	if err != nil {
		return err
	}
	req, err := s.newRequest(ctx, http.MethodDelete, key, nil)
	if err != nil {
		return err
	}
	s.sign(req, nil)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError("delete r2 upload", resp)
	}
	return nil
}

func (s *R2) ServeHTTP(w http.ResponseWriter, r *http.Request, object Object) error {
	key, err := s.keyFromPath(object.Path)
	if err != nil {
		return err
	}
	req, err := s.newRequest(r.Context(), http.MethodGet, key, nil)
	if err != nil {
		return err
	}
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}
	s.sign(req, nil)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		return responseError("serve r2 upload", resp)
	}
	copyHeader(w.Header(), resp.Header, "Accept-Ranges")
	copyHeader(w.Header(), resp.Header, "Content-Length")
	copyHeader(w.Header(), resp.Header, "Content-Range")
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		copyHeader(w.Header(), resp.Header, "ETag")
		copyHeader(w.Header(), resp.Header, "Last-Modified")
		if object.ContentType != "" {
			w.Header().Set("Content-Type", object.ContentType)
		} else {
			copyHeader(w.Header(), resp.Header, "Content-Type")
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		// The file response is committed: abort the stream instead of appending
		// an API error or successfully terminating a truncated chunked body.
		panic(http.ErrAbortHandler)
	}
	return nil
}

func (s *R2) newRequest(ctx context.Context, method, key string, body io.Reader) (*http.Request, error) {
	u, err := url.Parse(s.endpoint)
	if err != nil {
		return nil, err
	}
	u.Path = joinEscapedPath(u.Path, s.bucket, key)
	return http.NewRequestWithContext(ctx, method, u.String(), body)
}

func (s *R2) objectPath(key string) string {
	return "r2://" + s.bucket + "/" + strings.TrimLeft(key, "/")
}

func (s *R2) keyFromPath(objectPath string) (string, error) {
	if strings.HasPrefix(objectPath, "r2://") {
		u, err := url.Parse(objectPath)
		if err != nil {
			return "", err
		}
		if u.Host != s.bucket {
			return "", fmt.Errorf("r2 upload bucket %q does not match configured bucket %q", u.Host, s.bucket)
		}
		key := strings.TrimLeft(u.Path, "/")
		if key == "" {
			return "", ErrNotFound
		}
		return s.validateKey(key)
	}
	key := strings.TrimLeft(objectPath, "/")
	if key == "" {
		return "", ErrNotFound
	}
	return s.validateKey(key)
}

func (s *R2) validateKey(key string) (string, error) {
	if s.prefix != "" && !strings.HasPrefix(key, s.prefix) {
		return "", ErrNotFound
	}
	return key, nil
}

func (s *R2) sign(req *http.Request, payload []byte) {
	s.signHash(req, sha256Hex(payload))
}

func (s *R2) signHash(req *http.Request, payloadHash string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	req.Header.Set("X-Amz-Date", amzDate)
	signedHeaders, canonicalHeaders := canonicalHeaders(req.Header)
	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		canonicalQuery(req.URL.Query()),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	scope := dateStamp + "/" + s.region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(signingKey(s.secretAccessKey, dateStamp, s.region), []byte(stringToSign)))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.accessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func canonicalHeaders(headers http.Header) (string, string) {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, strings.ToLower(name))
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		values := headers.Values(name)
		for i := range values {
			values[i] = strings.Join(strings.Fields(values[i]), " ")
		}
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(strings.Join(values, ","))
		b.WriteByte('\n')
	}
	return strings.Join(names, ";"), b.String()
}

func canonicalQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		vals := append([]string(nil), values[key]...)
		sort.Strings(vals)
		for _, value := range vals {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
	}
	return strings.Join(parts, "&")
}

func signingKey(secret, date, region string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte("s3"))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func randomKey(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + "upload-" + hex.EncodeToString(b[:]), nil
}

func joinEscapedPath(base, bucket, key string) string {
	parts := []string{strings.Trim(base, "/"), bucket}
	parts = append(parts, strings.Split(strings.TrimLeft(key, "/"), "/")...)
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		escaped = append(escaped, url.PathEscape(part))
	}
	return "/" + strings.Join(escaped, "/")
}

func copyHeader(dst, src http.Header, name string) {
	if value := src.Get(name); value != "" {
		dst.Set(name, value)
	}
}

func responseError(prefix string, resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	message := fmt.Sprintf("%s: %s", prefix, resp.Status)
	if len(body) > 0 {
		message += ": " + strings.TrimSpace(string(body))
	}
	if err != nil {
		return fmt.Errorf("%s: %w", message, err)
	}
	return errors.New(message)
}
