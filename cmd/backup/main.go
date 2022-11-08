package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/noovertime7/etcd-operator/api/v1alpha1"
	"github.com/noovertime7/etcd-operator/pkg/file"
	"os"
	"path/filepath"
	"time"

	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/snapshot"
	"github.com/go-logr/zapr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func LogErr(log logr.Logger, err error, message string) error {
	log.Error(err, message)
	return fmt.Errorf("%s:%s", err.Error(), message)
}

func main() {

	var (
		backupTempDir     string
		backupUrl         string
		etcdUrl           string
		diaTimeOutSeconds int64
		timeoutSeconds    int64
	)

	flag.StringVar(&backupTempDir, "backup-temp-dir", os.TempDir(), "the directory to temp place backup")
	flag.StringVar(&backupUrl, "backup-url", "", "the etcd storage")
	flag.StringVar(&etcdUrl, "etcd-url", "", "Url for backup etcd")
	flag.Int64Var(&diaTimeOutSeconds, "dial-timeout-seconds", 5, "Timeout for dialing the Etcd")
	flag.Int64Var(&timeoutSeconds, "timeout-seconds", 60, "Timeout for backup the Etcd")
	endPoint := os.Getenv("ENDPOINT")
	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	accessSecret := os.Getenv("MINIO_SECRET_KEY")

	ctx := context.Background()
	//定义logger
	flag.Parse()
	log := ctrl.Log.WithName("bakckup")

	zapLogger := zap.NewRaw(zap.UseDevMode(true))
	ctrl.SetLogger(zapr.NewLogger(zapLogger))

	info, err := file.ParseBackupUrl(backupUrl)
	if err != nil {
		panic(LogErr(log, err, "failed to parse backup path"))
	}

	// 定义一个本地的数据目录
	localPath := filepath.Join(backupTempDir, "snapshot.db")

	//创建etcd snapshot manager
	etcdManager := snapshot.NewV3(zapLogger)

	//保存etcd snapshot 数据到local path
	if err := etcdManager.Save(ctx, clientv3.Config{
		Endpoints:   []string{etcdUrl},
		DialTimeout: time.Second * time.Duration(diaTimeOutSeconds),
	}, localPath); err != nil {
		panic(LogErr(log, err, "snapshot failed"))
	}
	log.Info("etcd backup finished,starting upload etcd backup file...")
	//开始上传oss
	switch info.Type {
	case string(v1alpha1.EtcdBackupStorageTypeS3):
		s3Uploader := file.NewS3UpLoader(endPoint, accessKey, accessSecret)
		log.Info("uploading snapshot")
		size, err := s3Uploader.Upload(ctx, localPath, info.Bucket, info.Path)
		if err != nil {
			panic(LogErr(log, err, "failed to upload backup etcd"))
		}
		log.WithValues("upload-size", size)
		log.Info("upload success")
	case string(v1alpha1.EtcdBackupStorageTypeOss):

	default:
		panic(LogErr(log, errors.New("unknown storage type "), "unknown storage type "))

	}
	log.Info("etcd backup successful")
}
