package bucket

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func (b *LocalBucket) GetInstanceID() (string, error) {
	if b.BasePath == "" {
		return "", errors.New("BasePath not set")
	}
	content, err := os.ReadFile(filepath.Join(b.rootPath(), ".instanceid"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}

func (b *LocalBucket) PersistInstanceID(id string) error {
	if b.BasePath == "" {
		return errors.New("BasePath not set")
	}
	return os.WriteFile(filepath.Join(b.rootPath(), ".instanceid"), []byte(id+"\n"), 0644)
}
