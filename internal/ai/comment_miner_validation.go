package ai

import "fmt"

// String returns the string representation of a CommentSource
func (s CommentSource) String() string {
	switch s {
	case SingleLineComments:
		return "SingleLine"
	case MultiLineComments:
		return "MultiLine"
	case DocComments:
		return "Doc"
	case InlineComments:
		return "Inline"
	case PackageComments:
		return "Package"
	case FileHeaderComments:
		return "FileHeader"
	case TODOComments:
		return "TODO"
	case FIXMEComments:
		return "FIXME"
	case NOTEComments:
		return "NOTE"
	case HACKComments:
		return "HACK"
	case WARNINGComments:
		return "WARNING"
	default:
		return "Unknown"
	}
}

// Validate checks if a comment is valid and complete
func (c *Comment) Validate() error {
	if c == nil {
		return fmt.Errorf("comment is nil")
	}

	if c.Text == "" {
		return fmt.Errorf("comment text is empty")
	}

	if c.FilePath == "" {
		return fmt.Errorf("comment file path is empty")
	}

	if c.LineNumber <= 0 {
		return fmt.Errorf("comment line number must be positive")
	}

	return nil
}
