package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// RemoteSettings is an S3-compatible destination (AWS, Backblaze B2,
// Cloudflare R2, MinIO, Wasabi). The secret access key and the passphrase
// are stored apart and never returned.
type RemoteSettings struct {
	Enabled bool `json:"enabled"`
	// Endpoint is the service URL, e.g. https://s3.eu-central-003.backblazeb2.com.
	Endpoint    string `json:"endpoint"`
	Region      string `json:"region"`
	Bucket      string `json:"bucket"`
	Prefix      string `json:"prefix"`
	AccessKeyID string `json:"accessKeyId"`
	// PathStyle addresses the bucket in the path (MinIO and most
	// self-hosted services) instead of the host name.
	PathStyle bool `json:"pathStyle"`
}

// Object is a stored remote object.
type Object struct {
	Key  string
	Size int64
}

// ObjectStore is the part of an S3 bucket the backups use.
type ObjectStore interface {
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	List(ctx context.Context, prefix string) ([]Object, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}

// ErrRemote wraps failures of the remote destination.
var ErrRemote = errors.New("remote backup destination")

// normalize validates rs and fills defaults (region us-east-1, the prefix
// ending in "/").
func (rs RemoteSettings) normalize() (RemoteSettings, *url.URL, error) {
	rs.Endpoint = strings.TrimSpace(rs.Endpoint)
	u, err := url.Parse(rs.Endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		return rs, nil, fmt.Errorf("%w: the endpoint must be a URL like https://s3.example.com", ErrInvalidSettings)
	}
	rs.Bucket = strings.TrimSpace(rs.Bucket)
	if rs.Bucket == "" || strings.ContainsAny(rs.Bucket, "/ ") {
		return rs, nil, fmt.Errorf("%w: enter the bucket name", ErrInvalidSettings)
	}
	rs.AccessKeyID = strings.TrimSpace(rs.AccessKeyID)
	if rs.AccessKeyID == "" {
		return rs, nil, fmt.Errorf("%w: enter the access key ID", ErrInvalidSettings)
	}
	rs.Region = strings.TrimSpace(rs.Region)
	if rs.Region == "" {
		rs.Region = "us-east-1"
	}
	p := strings.Trim(strings.TrimSpace(rs.Prefix), "/")
	if p != "" {
		if path.Clean(p) != p || strings.Contains(p, "..") {
			return rs, nil, fmt.Errorf("%w: the prefix is a folder name like piworker/kitchen-pi", ErrInvalidSettings)
		}
		p += "/"
	}
	rs.Prefix = p
	return rs, u, nil
}

// NewS3 connects to an S3-compatible service. transport may be nil.
func NewS3(rs RemoteSettings, secretKey string, transport http.RoundTripper) (ObjectStore, error) {
	rs, u, err := rs.normalize()
	if err != nil {
		return nil, err
	}
	lookup := minio.BucketLookupAuto
	if rs.PathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(rs.AccessKeyID, secretKey, ""),
		Secure:       u.Scheme == "https",
		Region:       rs.Region,
		BucketLookup: lookup,
		Transport:    transport,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRemote, err)
	}
	return &s3Store{client: client, bucket: rs.Bucket}, nil
}

type s3Store struct {
	client *minio.Client
	bucket string
}

func remoteErr(err error) error {
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code != "" {
		return fmt.Errorf("%w: %s (%s)", ErrRemote, resp.Message, resp.Code)
	}
	return fmt.Errorf("%w: %v", ErrRemote, err)
}

func (s *s3Store) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return remoteErr(err)
}

func (s *s3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, remoteErr(err)
	}
	// GetObject is lazy: Stat surfaces a missing object or bad credentials.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, remoteErr(err)
	}
	return obj, nil
}

func (s *s3Store) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	for o := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix}) {
		if o.Err != nil {
			return nil, remoteErr(o.Err)
		}
		out = append(out, Object{Key: o.Key, Size: o.Size})
	}
	return out, nil
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	return remoteErr(s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}))
}

func (s *s3Store) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	if minio.ToErrorResponse(err).StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, remoteErr(err)
}
