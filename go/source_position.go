package yamldslspec

import "fmt"

// Position is where a value was written: a file, a 1-based line and a 1-based column.
type Position struct {
	File   string
	Line   int
	Column int
}

// NewPosition is the position at a line and a column of a file.
func NewPosition(fileName string, lineNumber, columnNumber int) Position {
	return Position{File: fileName, Line: lineNumber, Column: columnNumber}
}

// FileStartPosition is the start of a file, for a value that has no narrower place.
func FileStartPosition(fileName string) Position {
	return NewPosition(fileName, 1, 1)
}

func (sourcePosition Position) String() string {
	return fmt.Sprintf("%s:%d:%d", sourcePosition.File, sourcePosition.Line, sourcePosition.Column)
}
