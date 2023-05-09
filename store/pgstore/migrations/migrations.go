package migrations

import (
	"embed"
	"io/fs"
	"os"
)

var FS = &migratorFS{migrationsFS}

//go:generate ./gen-docs.sh

//go:embed *.sql
var migrationsFS embed.FS

type migratorFS struct{ fsys fs.FS }

// ReadDir implements the MigratorFS interface.
func (m *migratorFS) ReadDir(dirname string) ([]fs.FileInfo, error) {
	d, err := fs.ReadDir(m.fsys, dirname)
	if err != nil {
		return nil, err
	}
	var fis []os.FileInfo
	for _, v := range d {
		fi, err := v.Info()
		if err != nil {
			return nil, err
		}
		fis = append(fis, fi)
	}
	return fis, nil
}

// ReadFile implements the MigratorFS interface.
func (m *migratorFS) ReadFile(filename string) ([]byte, error) {
	return fs.ReadFile(m.fsys, filename)
}

// Glob implements the MigratorFS interface.
func (m *migratorFS) Glob(pattern string) (matches []string, err error) {
	return fs.Glob(m.fsys, pattern)
}
