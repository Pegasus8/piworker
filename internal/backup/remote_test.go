package backup

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/Pegasus8/piworker/internal/backup/fakes3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeS3 = fakes3.Server

func newFakeS3(bucket string) *fakeS3 { return fakes3.New(bucket) }

// startFakeS3 serves fake over HTTPS and returns settings for it and a
// transport that trusts its certificate.
func startFakeS3(t *testing.T, fake *fakeS3) (RemoteSettings, http.RoundTripper) {
	t.Helper()
	url, tr := fakes3.Start(t, fake)
	return RemoteSettings{Enabled: true, Endpoint: url, Bucket: fake.Bucket, Prefix: "pi", AccessKeyID: "AKID", PathStyle: true}, tr
}

func TestS3StoreAgainstFakeService(t *testing.T) {
	fake := newFakeS3("backups")
	rs, tr := startFakeS3(t, fake)
	store, err := NewS3(rs, "secret", tr)
	require.NoError(t, err)
	ctx := context.Background()

	payload := bytes.Repeat([]byte("piworker"), 4096)
	require.NoError(t, store.Put(ctx, "pi/a.tar.gz.age", bytes.NewReader(payload), int64(len(payload))))
	assert.Equal(t, []string{"pi/a.tar.gz.age"}, fake.Keys())

	ok, err := store.Exists(ctx, "pi/a.tar.gz.age")
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = store.Exists(ctx, "pi/missing")
	require.NoError(t, err)
	assert.False(t, ok)

	rc, err := store.Get(ctx, "pi/a.tar.gz.age")
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	rc.Close()
	assert.Equal(t, payload, got)

	_, err = store.Get(ctx, "pi/missing")
	assert.ErrorIs(t, err, ErrRemote)

	list, err := store.List(ctx, "pi/")
	require.NoError(t, err)
	assert.Equal(t, []Object{{Key: "pi/a.tar.gz.age", Size: int64(len(payload))}}, list)

	require.NoError(t, store.Delete(ctx, "pi/a.tar.gz.age"))
	assert.Empty(t, fake.Keys())

	fake.Deny(true)
	_, err = store.List(ctx, "pi/")
	require.ErrorIs(t, err, ErrRemote)
	assert.Contains(t, err.Error(), "Access Denied")
}

func TestRemoteSettingsValidation(t *testing.T) {
	good := RemoteSettings{Endpoint: "https://s3.example.com", Bucket: "b", AccessKeyID: "k", Prefix: "/piworker/kitchen/"}
	n, _, err := good.normalize()
	require.NoError(t, err)
	assert.Equal(t, "piworker/kitchen/", n.Prefix)
	assert.Equal(t, "us-east-1", n.Region)

	for name, rs := range map[string]RemoteSettings{
		"scheme":  {Endpoint: "s3.example.com", Bucket: "b", AccessKeyID: "k"},
		"ftp":     {Endpoint: "ftp://s3.example.com", Bucket: "b", AccessKeyID: "k"},
		"path":    {Endpoint: "https://s3.example.com/bucket", Bucket: "b", AccessKeyID: "k"},
		"user":    {Endpoint: "https://me:pw@s3.example.com", Bucket: "b", AccessKeyID: "k"},
		"bucket":  {Endpoint: "https://s3.example.com", AccessKeyID: "k"},
		"key":     {Endpoint: "https://s3.example.com", Bucket: "b"},
		"prefix":  {Endpoint: "https://s3.example.com", Bucket: "b", AccessKeyID: "k", Prefix: "a/../b"},
		"slashes": {Endpoint: "https://s3.example.com", Bucket: "b/c", AccessKeyID: "k"},
	} {
		_, _, err := rs.normalize()
		assert.ErrorIs(t, err, ErrInvalidSettings, name)
	}
}
