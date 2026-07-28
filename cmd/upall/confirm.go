package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// confirmPrompt asks a yes/no question and reads one line of the answer.
// Anything other than "y"/"yes" (any case) is a no, so an accidental newline
// declines. Reader and writer are parameters rather than os.Stdin/os.Stdout so
// the prompt is testable without a real terminal.
func confirmPrompt(r io.Reader, w io.Writer, question string) (bool, error) {
	fmt.Fprintf(w, "%s [y/N] ", question)

	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && line == "" {
		// EOF with nothing typed reads as a decline, not a failure.
		if err == io.EOF {
			fmt.Fprintln(w)
			return false, nil
		}
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
