package llm

import (
	"bufio"
	"io"
	"strings"
)

// sseScanner reads Server-Sent Events from a reader.
// Each event is a sequence of lines starting with "event:" or "data:",
// terminated by a blank line.
type sseScanner struct {
	scanner *bufio.Scanner
	event   string // last event type
	data    string // accumulated data lines
}

func newSSEScanner(r io.Reader) *sseScanner {
	s := &sseScanner{scanner: bufio.NewScanner(r)}
	s.scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return s
}

// Next advances to the next SSE event. Returns false when done or on error.
func (s *sseScanner) Next() bool {
	for s.scanner.Scan() {
		line := s.scanner.Text()

		// Blank line = end of event.
		if line == "" {
			if s.data != "" {
				return true
			}
			continue
		}

		// Comment line.
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Event type line.
		if strings.HasPrefix(line, "event:") {
			s.event = strings.TrimPrefix(line, "event:")
			s.event = strings.TrimSpace(s.event)
			continue
		}

		// Data line — may have space after colon.
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			if len(data) > 0 && data[0] == ' ' {
				data = data[1:]
			}
			if s.data != "" {
				s.data += "\n"
			}
			s.data += data
		}
	}
	// Return true if we have accumulated data.
	return s.data != ""
}

// Event returns the event type and data for the current event, then resets.
func (s *sseScanner) Event() (eventType, data string) {
	eventType = s.event
	data = s.data
	s.event = ""
	s.data = ""
	return
}
