package bucket

type MigrationStorage interface {
	RetrieveMigrationHistory() ([]string, error)
	ApplyMigration(migrationId string) error
	RemoveMigrationFromHistory(migrationId string) error
}
