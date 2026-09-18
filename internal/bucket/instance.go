package bucket

type InstanceStorage interface {
	GetInstanceID() (string, error)
	PersistInstanceID(id string) error
}
