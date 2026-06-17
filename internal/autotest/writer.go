package autotest

import "os"

// Writer writes a generated test and can revert it.
type Writer interface {
	Write(tf TestFile) error
	Revert(path string) error
}

// FSWriter is the default Writer: writes to disk, reverts via os.Remove.
type FSWriter struct{}

func (FSWriter) Write(tf TestFile) error  { return os.WriteFile(tf.Path, tf.Content, 0o644) }
func (FSWriter) Revert(path string) error { return os.Remove(path) }
