package bucket

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (b *LocalBucket) RetrieveMigrationHistory() ([]string, error) {
	if b.BasePath == "" {
		return nil, errors.New("BasePath not set")
	}
	migrationHistoryPath := filepath.Join(b.rootPath(), ".migrationhistory")
	file, err := os.Open(migrationHistoryPath)
	if err != nil {
		return nil, nil
	}
	defer file.Close()
	var migrations []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		migrations = append(migrations, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return migrations, nil
}

func (b *LocalBucket) ApplyMigration(migrationId string) error {
	if b.BasePath == "" {
		return errors.New("BasePath not set")
	}

	migrationHistoryPath := filepath.Join(b.rootPath(), ".migrationhistory")

	migrations, err := b.RetrieveMigrationHistory()
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("RetrieveMigrationHistory error: %w", err)
	}
	for _, id := range migrations {
		if id == migrationId {
			return nil
		}
	}

	file, err := os.OpenFile(migrationHistoryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open .migrationhistory error: %w", err)
	}
	defer file.Close()

	if _, err := file.WriteString(migrationId + "\n"); err != nil {
		return fmt.Errorf("write .migrationhistory error: %w", err)
	}

	return nil
}

func (b *LocalBucket) RemoveMigrationFromHistory(migrationId string) error {
	if b.BasePath == "" {
		return errors.New("BasePath not set")
	}

	migrationHistoryPath := filepath.Join(b.rootPath(), ".migrationhistory")

	migrations, err := b.RetrieveMigrationHistory()
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("RetrieveMigrationHistory error: %w", err)
	}
	hasMigration := false
	for _, id := range migrations {
		if id == migrationId {
			hasMigration = true
			break
		}
	}
	if !hasMigration {
		return nil
	}

	var newMigrations []string
	for _, id := range migrations {
		if id != migrationId {
			newMigrations = append(newMigrations, id)
		}
	}

	file, err := os.OpenFile(migrationHistoryPath, os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open .migrationhistory error: %w", err)
	}
	defer file.Close()

	for _, id := range newMigrations {
		if _, err := file.WriteString(id + "\n"); err != nil {
			return fmt.Errorf("write .migrationhistory error: %w", err)
		}
	}

	return nil
}
