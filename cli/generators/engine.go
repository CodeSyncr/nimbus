package generators

import (
	"bytes"
	"embed"
	"os"
	"path/filepath"
	"text/template"
	"time"
)

//go:embed templates/*
var fs embed.FS

// Data is a simple map for passing template parameters.
type Data map[string]any

// WithTimestamp adds a standard migration-style timestamp.
func (d Data) WithTimestamp() Data {
	d["Timestamp"] = time.Now().Format("20060102150405")
	return d
}

// RenderToFile renders the named template from the embedded templates/
// directory to the destination path on disk.
func RenderToFile(tmplPath, destPath string, data Data) error {
	raw, err := fs.ReadFile(filepath.Join("templates", tmplPath))
	if err != nil {
		return err
	}
	t := template.Must(template.New(filepath.Base(tmplPath)).Parse(string(raw)))
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(destPath, buf.Bytes(), 0644)
}

