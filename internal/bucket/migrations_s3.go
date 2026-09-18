package bucket

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"xprem/internal/providers/aws"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func (b *S3Bucket) RetrieveMigrationHistory() ([]string, error) {
	if b.BucketName == "" {
		return nil, errors.New("BucketName not set")
	}
	s3Client, errS3 := aws.GetS3Client()
	if errS3 != nil {
		return nil, errS3
	}
	input := &s3.GetObjectInput{
		Bucket: awssdk.String(b.BucketName),
		Key:    awssdk.String(b.prefixedKey(".migrationhistory")),
	}
	resp, err := s3Client.GetObject(context.TODO(), input)
	if err != nil {
		var noSuchKey *s3types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			// handle empty migration history if file doesn't exist (first time setup)
			return nil, nil
		}
		return nil, fmt.Errorf("GetObject error: %w", err)
	}
	defer resp.Body.Close()
	var migrationHistory []string
	for {
		var line string
		_, err := fmt.Fscanln(resp.Body, &line)
		if err != nil {
			break
		}
		migrationHistory = append(migrationHistory, line)
	}
	return migrationHistory, nil
}

func (b *S3Bucket) ApplyMigration(migrationId string) error {
	if b.BucketName == "" {
		return errors.New("BucketName not set")
	}

	migrationHistory, err := b.RetrieveMigrationHistory()
	if err != nil {
		return fmt.Errorf("RetrieveMigrationHistory error: %w", err)
	}
	isAlreadyApplied := false
	for _, id := range migrationHistory {
		if id == migrationId {
			isAlreadyApplied = true
			break
		}
	}
	if isAlreadyApplied {
		return nil
	}

	s3Client, errS3 := aws.GetS3Client()
	if errS3 != nil {
		return errS3
	}

	var currentContent []byte
	obj, err := s3Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: awssdk.String(b.BucketName),
		Key:    awssdk.String(b.prefixedKey(".migrationhistory")),
	})
	if err == nil {
		defer obj.Body.Close()
		currentContent, _ = io.ReadAll(obj.Body)
	}

	newContent := append(currentContent, []byte(migrationId+"\n")...)

	_, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: awssdk.String(b.BucketName),
		Key:    awssdk.String(b.prefixedKey(".migrationhistory")),
		Body:   bytes.NewReader(newContent),
	})
	if err != nil {
		return fmt.Errorf("PutObject error: %w", err)
	}

	return nil
}

func (b *S3Bucket) RemoveMigrationFromHistory(migrationId string) error {
	if b.BucketName == "" {
		return errors.New("BucketName not set")
	}

	migrationHistory, err := b.RetrieveMigrationHistory()
	if err != nil {
		return fmt.Errorf("RetrieveMigrationHistory error: %w", err)
	}

	hasMigration := false
	for _, id := range migrationHistory {
		if id == migrationId {
			hasMigration = true
			break
		}
	}
	if !hasMigration {
		return nil
	}

	var newContent []byte
	for _, id := range migrationHistory {
		if id != migrationId {
			newContent = append(newContent, []byte(id+"\n")...)
		}
	}

	s3Client, errS3 := aws.GetS3Client()
	if errS3 != nil {
		return errS3
	}

	_, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: awssdk.String(b.BucketName),
		Key:    awssdk.String(b.prefixedKey(".migrationhistory")),
		Body:   bytes.NewReader(newContent),
	})
	if err != nil {
		return fmt.Errorf("PutObject error: %w", err)
	}

	return nil
}
