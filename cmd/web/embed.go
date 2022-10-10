package main

import (
	"embed"
	_ "embed"
	"github.com/tyler-sommer/stick"
	"io"
	"path/filepath"
)

//go:embed templates
var templates embed.FS

type loader struct {
}

type fileTemplate struct {
	name   string
	reader io.Reader
}

func (t *fileTemplate) Name() string {
	return t.name
}

func (t *fileTemplate) Contents() io.Reader {
	return t.reader
}

func (l *loader) Load(name string) (stick.Template, error) {
	f, err := templates.Open(filepath.Join("templates", name))
	if err != nil {
		return nil, err
	}
	return &fileTemplate{name, f}, nil
}
