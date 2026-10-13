package store

import (
	"errors"
	bolt "go.etcd.io/bbolt"
	"strings"
	"unicode"
	"unicode/utf8"
)

func CleanDeviceName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 80 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", errors.New("invalid device name")
	}
	return name, nil
}
func (s *Store) RenameDevice(id, name string) (string, error) {
	name, err := CleanDeviceName(name)
	if err != nil {
		return "", err
	}
	err = s.db.Update(func(t *bolt.Tx) error {
		var d Device
		if err := s.get(t, "devices", id, &d); err != nil {
			return err
		}
		d.Name = name
		return s.put(t, "devices", id, d)
	})
	return name, err
}
