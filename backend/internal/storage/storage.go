// Package storage issues S3 presigned URLs for medical documents.
//
// Why presigned URLs: the browser uploads and downloads files DIRECTLY to and from S3 using a
// short-lived URL signed with the Lambda's permissions. File bytes never pass through
// Lambda or API Gateway. That avoids their payload limits (6 MB and 10 MB), keeps Lambda
// cheap and fast, and means the bucket can stay fully private (Block Public Access on).
// Each URL is valid for 5 minutes and for exactly one object and one operation.
package storage

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// URLTTL is how long a presigned URL stays valid.
const URLTTL = 5 * time.Minute

// ErrObjectMissing means nothing has been uploaded at the key yet.
var ErrObjectMissing = errors.New("object not found")

type S3 struct {
	Client *s3.Client
	Bucket string
}

// PresignUpload returns a PUT URL. The signature includes Content-Type, so the browser MUST
// send exactly that header, which stops the client from uploading a different file type.
func (s S3) PresignUpload(ctx context.Context, key, contentType string) (string, time.Time, error) {
	req, err := s3.NewPresignClient(s.Client).PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.Bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(URLTTL))
	if err != nil {
		return "", time.Time{}, err
	}
	return req.URL, time.Now().Add(URLTTL).UTC(), nil
}

// PresignDownload returns a GET URL that makes the browser download the file with its original name.
func (s S3) PresignDownload(ctx context.Context, key, fileName string) (string, time.Time, error) {
	req, err := s3.NewPresignClient(s.Client).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket:                     aws.String(s.Bucket),
		Key:                        aws.String(key),
		ResponseContentDisposition: aws.String(fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(fileName))),
	}, s3.WithPresignExpires(URLTTL))
	if err != nil {
		return "", time.Time{}, err
	}
	return req.URL, time.Now().Add(URLTTL).UTC(), nil
}

// Size returns the uploaded object's size (HeadObject). It confirms the upload actually happened.
func (s S3) Size(ctx context.Context, key string) (int64, error) {
	out, err := s.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)})
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return 0, ErrObjectMissing
	}
	if err != nil {
		return 0, err
	}
	return aws.ToInt64(out.ContentLength), nil
}

// Delete removes an object. It is used to discard an upload that broke the size limit.
func (s S3) Delete(ctx context.Context, key string) error {
	_, err := s.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)})
	return err
}
