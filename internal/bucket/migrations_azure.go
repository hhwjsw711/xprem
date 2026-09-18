package bucket

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
)

func (b *AzureBucket) RetrieveMigrationHistory() ([]string, error) {
	ctx := context.Background()
	cc, err := b.containerClient()
	if err != nil {
		return nil, err
	}
	resp, err := cc.NewBlobClient(b.prefixedKey(".migrationhistory")).DownloadStream(ctx, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil, nil
		}
		return nil, err
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, resp.Body); err != nil {
		return nil, err
	}
	var migrations []string
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line != "" {
			migrations = append(migrations, line)
		}
	}
	return migrations, nil
}

func (b *AzureBucket) writeMigrationHistory(content string) error {
	ctx := context.Background()
	cc, err := b.containerClient()
	if err != nil {
		return err
	}
	_, err = cc.NewBlockBlobClient(b.prefixedKey(".migrationhistory")).UploadBuffer(ctx, []byte(content), nil)
	return err
}

func (b *AzureBucket) ApplyMigration(migrationId string) error {
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
	return b.writeMigrationHistory(current + migrationId + "\n")
}

func (b *AzureBucket) RemoveMigrationFromHistory(migrationId string) error {
	history, err := b.RetrieveMigrationHistory()
	if err != nil {
		return fmt.Errorf("RetrieveMigrationHistory error: %w", err)
	}
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
	return b.writeMigrationHistory(content)
}
