// Package fakes3 is an in-memory, path-style S3 endpoint for tests: enough
// of the protocol for PUT (plain or aws-chunked), GET, HEAD, DELETE and
// ListObjectsV2.
package fakes3

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Server holds a bucket's objects in memory.
type Server struct {
	mu      sync.Mutex
	Bucket  string
	objects map[string][]byte
	denied  bool
}

// New is an empty bucket.
func New(bucket string) *Server {
	return &Server{Bucket: bucket, objects: map[string][]byte{}}
}

// Deny makes every request fail with 403 (wrong credentials) or not.
func (f *Server) Deny(d bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.denied = d
}

// Object returns a stored object.
func (f *Server) Object(key string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.objects[key]
}

// Put stores an object directly.
func (f *Server) Put(key string, b []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = b
}

// Keys lists the stored keys, sorted.
func (f *Server) Keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for k := range f.objects {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func s3Error(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, msg)
}

// decodeChunked reads an aws-chunked body (signed or unsigned, with
// optional trailers).
func decodeChunked(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(r)
	var out bytes.Buffer
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		sizeHex := strings.SplitN(line, ";", 2)[0]
		n, err := strconv.ParseInt(sizeHex, 16, 64)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return out.Bytes(), nil
		}
		if _, err := io.CopyN(&out, br, n); err != nil {
			return nil, err
		}
		if _, err := br.ReadString('\n'); err != nil {
			return nil, err
		}
	}
}

// ServeHTTP answers S3 requests.
func (f *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	denied := f.denied
	f.mu.Unlock()
	if denied {
		s3Error(w, http.StatusForbidden, "AccessDenied", "Access Denied")
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	if parts[0] != f.Bucket {
		s3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	key := ""
	if len(parts) == 2 {
		key = parts[1]
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && key == "":
		prefix := r.URL.Query().Get("prefix")
		type content struct {
			Key          string
			Size         int64
			LastModified string
			ETag         string
		}
		res := struct {
			XMLName     xml.Name `xml:"ListBucketResult"`
			Name        string
			Prefix      string
			KeyCount    int
			MaxKeys     int
			IsTruncated bool
			Contents    []content
		}{Name: f.Bucket, Prefix: prefix, MaxKeys: 1000}
		var ks []string
		for k := range f.objects {
			if strings.HasPrefix(k, prefix) {
				ks = append(ks, k)
			}
		}
		sort.Strings(ks)
		for _, k := range ks {
			sum := md5.Sum(f.objects[k])
			res.Contents = append(res.Contents, content{Key: k, Size: int64(len(f.objects[k])), LastModified: time.Now().UTC().Format(time.RFC3339), ETag: `"` + hex.EncodeToString(sum[:]) + `"`})
		}
		res.KeyCount = len(res.Contents)
		w.Header().Set("Content-Type", "application/xml")
		_ = xml.NewEncoder(w).Encode(res)
	case r.Method == http.MethodPut:
		var body []byte
		var err error
		if strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") || strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
			body, err = decodeChunked(r.Body)
		} else {
			body, err = io.ReadAll(r.Body)
		}
		if err != nil {
			s3Error(w, http.StatusBadRequest, "IncompleteBody", err.Error())
			return
		}
		f.objects[key] = body
		sum := md5.Sum(body)
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		body, ok := f.objects[key]
		if !ok {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			s3Error(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
			return
		}
		sum := md5.Sum(body)
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		s3Error(w, http.StatusNotImplemented, "NotImplemented", r.Method)
	}
}

// Start serves f over HTTPS until the test ends and returns its URL and a
// transport that trusts its certificate.
func Start(t testing.TB, f *Server) (string, http.RoundTripper) {
	t.Helper()
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	return srv.URL, srv.Client().Transport
}
