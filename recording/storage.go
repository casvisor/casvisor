// Copyright 2026 The casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package recording

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/conf"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Recordings are always written to the local directory first, so a session never waits on
// the object storage. With recordingStorage = s3, a finished recording is uploaded to an
// S3-compatible bucket (AWS S3, Alibaba Cloud OSS, Tencent COS, Cloudflare R2, MinIO...) and
// the local copy removed; uploads that fail are retried every few minutes.

var (
	activeFiles sync.Map
	uploading   sync.Map

	s3Mutex  sync.Mutex
	s3Client *minio.Client
	s3Config string
)

func isS3() bool {
	return conf.GetConfigString("recordingStorage") == "s3"
}

func getS3Client() (*minio.Client, error) {
	endpoint := conf.GetConfigString("recordingS3Endpoint")
	accessKey := conf.GetConfigString("recordingS3AccessKey")
	secretKey := conf.GetConfigString("recordingS3SecretKey")
	region := conf.GetConfigString("recordingS3Region")
	secure := !conf.GetConfigBool("recordingS3DisableSsl")
	pathStyle := conf.GetConfigBool("recordingS3PathStyle")
	if endpoint == "" || getS3Bucket() == "" {
		return nil, fmt.Errorf("recordingS3Endpoint and recordingS3Bucket must be set when recordingStorage is s3")
	}

	config := fmt.Sprintf("%s|%s|%s|%s|%t|%t", endpoint, accessKey, secretKey, region, secure, pathStyle)
	s3Mutex.Lock()
	defer s3Mutex.Unlock()
	if s3Client != nil && s3Config == config {
		return s3Client, nil
	}

	lookup := minio.BucketLookupAuto
	if pathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://"), &minio.Options{
		Creds:        credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure:       secure,
		Region:       region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, err
	}
	s3Client, s3Config = client, config
	return client, nil
}

func getS3Bucket() string {
	return conf.GetConfigString("recordingS3Bucket")
}

func getObjectKey(name string) string {
	return path.Join(conf.GetConfigString("recordingS3Prefix"), name)
}

// upload moves a finished recording to the bucket.
func upload(name string) error {
	if _, loaded := uploading.LoadOrStore(name, true); loaded {
		return nil
	}
	defer uploading.Delete(name)

	client, err := getS3Client()
	if err != nil {
		return err
	}
	localPath, err := GetPath(name)
	if err != nil {
		return err
	}
	file, err := os.Open(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	_, err = client.PutObject(ctx, getS3Bucket(), getObjectKey(name), file, info.Size(), minio.PutObjectOptions{ContentType: "application/octet-stream"})
	_ = file.Close()
	if err != nil {
		return err
	}

	if !conf.GetConfigBool("recordingKeepLocal") {
		err = os.Remove(localPath)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func uploadInBackground(name string) {
	if !isS3() {
		return
	}
	go func() {
		err := upload(name)
		if err != nil {
			logs.Error(fmt.Sprintf("recording.upload(): %s, will retry: %s", name, err.Error()))
		}
	}()
}

// uploadPending uploads the local recordings that are finished but not in the bucket yet,
// after a failed upload or a restart.
func uploadPending() {
	if !isS3() {
		return
	}

	root := getRoot()
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".cvr") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		name := filepath.ToSlash(rel)
		if _, ok := activeFiles.Load(name); ok {
			return nil
		}
		if conf.GetConfigBool("recordingKeepLocal") && existsInBucket(name) {
			return nil
		}
		err = upload(name)
		if err != nil {
			logs.Error(fmt.Sprintf("recording.uploadPending(): %s: %s", name, err.Error()))
		}
		return nil
	})
}

func existsInBucket(name string) bool {
	client, err := getS3Client()
	if err != nil {
		return false
	}
	_, err = client.StatObject(context.Background(), getS3Bucket(), getObjectKey(name), minio.StatObjectOptions{})
	return err == nil
}

// File is a recording opened for reading, from the local directory or from the bucket.
type File interface {
	io.ReadSeekCloser
}

// Open opens a recording for reading, with its size and modification time.
func Open(name string) (File, time.Time, error) {
	localPath, err := GetPath(name)
	if err != nil {
		return nil, time.Time{}, err
	}

	file, err := os.Open(localPath)
	if err == nil {
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return nil, time.Time{}, err
		}
		return file, info.ModTime(), nil
	}
	if !os.IsNotExist(err) || !isS3() {
		return nil, time.Time{}, err
	}

	client, err := getS3Client()
	if err != nil {
		return nil, time.Time{}, err
	}
	object, err := client.GetObject(context.Background(), getS3Bucket(), getObjectKey(name), minio.GetObjectOptions{})
	if err != nil {
		return nil, time.Time{}, err
	}
	info, err := object.Stat()
	if err != nil {
		_ = object.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, time.Time{}, os.ErrNotExist
		}
		return nil, time.Time{}, err
	}
	return object, info.LastModified, nil
}

func deleteFromBucket(name string) {
	if !isS3() {
		return
	}
	client, err := getS3Client()
	if err != nil {
		logs.Error(fmt.Sprintf("recording.deleteFromBucket(): %s", err.Error()))
		return
	}
	err = client.RemoveObject(context.Background(), getS3Bucket(), getObjectKey(name), minio.RemoveObjectOptions{})
	if err != nil {
		logs.Error(fmt.Sprintf("recording.deleteFromBucket(): %s: %s", name, err.Error()))
	}
}

// deleteExpiredFromBucket removes the recordings in the bucket last modified before the deadline.
func deleteExpiredFromBucket(deadline time.Time) {
	if !isS3() {
		return
	}
	client, err := getS3Client()
	if err != nil {
		logs.Error(fmt.Sprintf("recording.deleteExpiredFromBucket(): %s", err.Error()))
		return
	}

	prefix := conf.GetConfigString("recordingS3Prefix")
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	ctx := context.Background()
	for object := range client.ListObjects(ctx, getS3Bucket(), minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if object.Err != nil {
			logs.Error(fmt.Sprintf("recording.deleteExpiredFromBucket(): %s", object.Err.Error()))
			return
		}
		if !strings.HasSuffix(object.Key, ".cvr") || object.LastModified.After(deadline) {
			continue
		}
		err = client.RemoveObject(ctx, getS3Bucket(), object.Key, minio.RemoveObjectOptions{})
		if err != nil {
			logs.Error(fmt.Sprintf("recording.deleteExpiredFromBucket(): %s: %s", object.Key, err.Error()))
		}
	}
}
