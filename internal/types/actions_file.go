package types

import "fmt"

// FileReadAction represents reading a file.
type FileReadAction struct {
	// Path is the file path to read.
	Path string `json:"path"`
	// Offset is the starting byte offset (0-based).
	Offset int `json:"offset,omitempty"`
	// Limit is the maximum bytes to read.
	Limit int `json:"limit,omitempty"`
}

func (a FileReadAction) GetActionType() ActionType { return ActionFileRead }
func (a FileReadAction) Target() string            { return a.Path }
func (a FileReadAction) Validate() error {
	if a.Path == "" {
		return fmt.Errorf("path is required")
	}
	return nil
}

// FileWriteAction represents writing content to a file.
type FileWriteAction struct {
	// Path is the file path to write.
	Path string `json:"path"`
	// Content is the content to write.
	Content string `json:"content"`
	// CreateParentDirs creates parent directories if they don't exist.
	CreateParentDirs bool `json:"create_parent_dirs,omitempty"`
	// Mode is the file permission mode (octal).
	Mode int `json:"mode,omitempty"`
}

func (a FileWriteAction) GetActionType() ActionType { return ActionFileWrite }
func (a FileWriteAction) Target() string            { return a.Path }
func (a FileWriteAction) Validate() error {
	if a.Path == "" {
		return fmt.Errorf("path is required")
	}
	if a.Content == "" {
		return fmt.Errorf("content is required")
	}
	return nil
}

// FileExistsAction represents checking if a file exists.
type FileExistsAction struct {
	// Path is the file path to check.
	Path string `json:"path"`
}

func (a FileExistsAction) GetActionType() ActionType { return ActionFileExists }
func (a FileExistsAction) Target() string            { return a.Path }
func (a FileExistsAction) Validate() error {
	if a.Path == "" {
		return fmt.Errorf("path is required")
	}
	return nil
}

// FileGlobAction represents finding files by pattern.
type FileGlobAction struct {
	// Pattern is the glob pattern (e.g., "**/*.go").
	Pattern string `json:"pattern"`
	// Path is the base directory for the search.
	Path string `json:"path,omitempty"`
}

func (a FileGlobAction) GetActionType() ActionType { return ActionFileGlob }
func (a FileGlobAction) Target() string            { return a.Pattern }
func (a FileGlobAction) Validate() error {
	if a.Pattern == "" {
		return fmt.Errorf("pattern is required")
	}
	return nil
}
