package file

import (
	"context"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// s3UpLoader
type s3UpLoader struct {
	Endpoint        string
	AccessKeyId     string
	SecretAccessKey string
}

func (su *s3UpLoader) InitClient() (*minio.Client, error) {
	return minio.New(su.Endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(su.AccessKeyId, su.SecretAccessKey, ""),
	})
}

func NewS3UpLoader(endpoint string, accessKeyId string, secretAccessKey string) *s3UpLoader {
	return &s3UpLoader{Endpoint: endpoint, AccessKeyId: accessKeyId, SecretAccessKey: secretAccessKey}
}

func (su *s3UpLoader) Upload(ctx context.Context, filePath string, backupName, objectName string) (int64, error) {
	c, err := su.InitClient()
	if err != nil {
		return 0, err
	}
	info, err := c.FPutObject(ctx, backupName, objectName, filePath, minio.PutObjectOptions{})
	if err != nil {
		return 0, err
	}
	return info.Size, nil
}
