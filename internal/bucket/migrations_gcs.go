package bucket

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"cloud.google.com/go/storage"
)

func (b *GCSBucket) RetrieveMigrationHistory() ([]string, error) {
	ctx := context.Background()
	bh, err := b.bucketHandle(ctx)
	if err != nil {
		return nil, err
	}
	obj := bh.Object(b.prefixedKey(".migrationhistory"))
	r, err := obj.NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer r.Close()
	var migrations []string
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, r); err != nil {
		return nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line != "" {
			migrations = append(migrations, line)
		}
	}
	return migrations, nil
}

func (b *GCSBucket) ApplyMigration(migrationId string) error {
	ctx := context.Background()
	bh, err := b.bucketHandle(ctx)
	if err != nil {
		return err
	}
	history, err := b.RetrieveMigrationHistory()
	if err != nil {
		return fmt.Errorf("RetrieveMigrationHistory error: %w", err)
	}
	for _, id := range history {
		if id == migrationId {
			return nil
		}
	}
	current := strings.Join(history, "\n")
	if current != "" {
		current += "\n"
	}
	data := []byte(current + migrationId + "\n")
	w := bh.Object(b.prefixedKey(".migrationhistory")).NewWriter(ctx)
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func (b *GCSBucket) RemoveMigrationFromHistory(migrationId string) error {
	ctx := context.Background()
	bh, err := b.bucketHandle(ctx)
	if err != nil {
		return err
	}
	history, err := b.RetrieveMigrationHistory()
	if err != nil {
		return fmt.Errorf("RetrieveMigrationHistory error: %w", err)
	}
	// If not present, nothing to do
	found := false
	var filtered []string
	for _, id := range history {
		if id == migrationId {
			found = true
			continue
		}
		filtered = append(filtered, id)
	}
	if !found {
		return nil
	}
	content := ""
	if len(filtered) > 0 {
		content = strings.Join(filtered, "\n") + "\n"
	}
	w := bh.Object(b.prefixedKey(".migrationhistory")).NewWriter(ctx)
	if _, err := w.Write([]byte(content)); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}
