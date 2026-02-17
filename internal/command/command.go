// Package command provides template expansion for shell commands.
package command

import (
	"strings"

	"github.com/ivoronin/pll/internal/job"
)

// Template holds a raw command string and directory with {} placeholders.
type Template struct {
	raw string
	dir string
}

// NewTemplate creates a Template with the given command and directory patterns.
func NewTemplate(raw string, dir string) *Template {
	return &Template{raw: raw, dir: dir}
}

// Expand replaces {} placeholders in the command and directory with the given line.
func (t *Template) Expand(line string) *job.Job {
	return &job.Job{
		Command: strings.ReplaceAll(t.raw, "{}", line),
		Dir:     strings.ReplaceAll(t.dir, "{}", line),
	}
}
