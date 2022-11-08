package file

import (
	"net/url"
)

//s3://my-bucket/my-dir/my-object.db

type BackupInfo struct {
	Type   string
	Bucket string
	Path   string
}

func ParseBackupUrl(backupUrl string) (*BackupInfo, error) {
	u, err := url.Parse(backupUrl)
	if err != nil {
		return nil, err
	}
	return &BackupInfo{
		Type:   u.Scheme,
		Bucket: u.Host,
		Path:   u.Path[1:],
	}, nil
}
