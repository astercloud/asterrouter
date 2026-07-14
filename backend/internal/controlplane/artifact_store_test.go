package controlplane

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestArtifactStoresPutRangeAndDelete(t *testing.T) {
	local, err := NewLocalArtifactStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		store ArtifactStore
	}{
		{name: "memory", store: NewMemoryArtifactStore()},
		{name: "local", store: local},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			payload := []byte("0123456789")
			if written, err := test.store.Put(ctx, "tenant/artifact/content", bytes.NewReader(payload), -1, "application/octet-stream"); err != nil || written != int64(len(payload)) {
				t.Fatalf("Put() written=%d err=%v", written, err)
			}
			opened, err := test.store.Open(ctx, "tenant/artifact/content", &ArtifactByteRange{Offset: 3, Length: 4})
			if err != nil {
				t.Fatal(err)
			}
			data, readErr := io.ReadAll(opened.Body)
			_ = opened.Body.Close()
			if readErr != nil || string(data) != "3456" || opened.Offset != 3 || opened.SizeBytes != 4 || opened.TotalBytes != 10 {
				t.Fatalf("Open() data=%q read=%+v err=%v", data, opened, readErr)
			}
			if _, err := test.store.Open(ctx, "tenant/artifact/content", &ArtifactByteRange{Offset: 10}); !errors.Is(err, ErrArtifactUnavailable) {
				t.Fatalf("unsatisfiable range error=%v", err)
			}
			if err := test.store.Delete(ctx, "tenant/artifact/content"); err != nil {
				t.Fatal(err)
			}
			if _, err := test.store.Open(ctx, "tenant/artifact/content", nil); !errors.Is(err, ErrArtifactUnavailable) {
				t.Fatalf("deleted Open() error=%v", err)
			}
		})
	}
}

func TestLocalArtifactStoreRejectsTraversal(t *testing.T) {
	store, err := NewLocalArtifactStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../secret", "/absolute", "nested/../../secret", `nested\\secret`, "nested\nsecret"} {
		if _, err := store.Put(context.Background(), key, bytes.NewBufferString("data"), 4, "text/plain"); err == nil {
			t.Fatalf("Put(%q) accepted an unsafe key", key)
		}
	}
}

func TestS3ArtifactStoreRangeAndUnavailableMapping(t *testing.T) {
	client := &fakeArtifactS3Client{getResult: &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewBufferString("3456")), ContentLength: aws.Int64(4), ContentRange: aws.String("bytes 3-6/10"),
	}}
	store := &S3ArtifactStore{client: client, config: S3ArtifactStoreConfig{Bucket: "bucket", Prefix: "artifacts"}}
	opened, err := store.Open(context.Background(), "tenant/content", &ArtifactByteRange{Offset: 3, Length: 4})
	if err != nil || opened.Offset != 3 || opened.SizeBytes != 4 || opened.TotalBytes != 10 || aws.ToString(client.lastGet.Range) != "bytes=3-6" || aws.ToString(client.lastGet.Key) != "artifacts/tenant/content" {
		t.Fatalf("Open() opened=%+v input=%+v err=%v", opened, client.lastGet, err)
	}
	_ = opened.Body.Close()
	client.getResult = nil
	client.getErr = codedArtifactStoreError{code: "InvalidRange"}
	if _, err := store.Open(context.Background(), "tenant/content", &ArtifactByteRange{Offset: 99}); !errors.Is(err, ErrArtifactUnavailable) {
		t.Fatalf("InvalidRange error=%v", err)
	}
}

type fakeArtifactS3Client struct {
	lastGet   *s3.GetObjectInput
	getResult *s3.GetObjectOutput
	getErr    error
}

func (c *fakeArtifactS3Client) PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return &s3.PutObjectOutput{}, nil
}

func (c *fakeArtifactS3Client) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	c.lastGet = input
	return c.getResult, c.getErr
}

func (c *fakeArtifactS3Client) DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	return &s3.DeleteObjectOutput{}, nil
}

type codedArtifactStoreError struct {
	code string
}

func (e codedArtifactStoreError) Error() string     { return e.code }
func (e codedArtifactStoreError) ErrorCode() string { return e.code }
