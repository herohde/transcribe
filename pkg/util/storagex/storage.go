package storagex

import (
	"context"
	"fmt"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/storage/v1"
	"log"
	"os"
)

// NewClient returns a new GCS client using Application Default Credentials and
// with Full scope.
func NewClient(ctx context.Context) (*storage.Service, error) {
	httpClient, err := google.DefaultClient(ctx, storage.CloudPlatformScope)
	if err != nil {
		return nil, err
	}
	return storage.New(httpClient)
}

// NewBucket creates a new GCS bucket in the given project.
func NewBucket(cl *storage.Service, project, bucket string) error {
	_, err := cl.Buckets.Insert(project, &storage.Bucket{Name: bucket}).Do()
	return err
}

// TryDeleteBucket tries to delete the given bucket and logs any errors.
// Intended to deferred cleanup.
func TryDeleteBucket(cl *storage.Service, bucket string) {
	if err := cl.Buckets.Delete(bucket).Do(); err != nil {
		log.Printf("Failed to delete bucket %v: %v", bucket, err)
	}
}

// UploadFile uploads the given file to GCS. It assumes the bucket exists.
func UploadFile(cl *storage.Service, bucket, object, filename string) error {
	fd, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer fd.Close()

	if _, err := cl.Objects.Insert(bucket, &storage.Object{Name: object}).Media(fd).Do(); err != nil {
		return fmt.Errorf("failed to create object: %v", err)
	}
	return nil
}

// TryDeleteObject tries to delete the given object and logs any errors.
// Intended to deferred cleanup.
func TryDeleteObject(cl *storage.Service, bucket, object string) {
	if err := cl.Objects.Delete(bucket, object).Do(); err != nil {
		log.Printf("Failed to delete object gs://%v/%v: %v", bucket, object, err)
	}
}
