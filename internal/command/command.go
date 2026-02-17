package command

import (
	"strings"

	"github.com/ivoronin/pll/internal/job"
)

type Template struct {
	raw string
	dir string
}

func NewTemplate(raw string, dir string) *Template {
	return &Template{raw: raw, dir: dir}
}

func (t *Template) Expand(line string) *job.Job {
	return &job.Job{
		Command: strings.ReplaceAll(t.raw, "{}", line),
		Dir:     strings.ReplaceAll(t.dir, "{}", line),
	}
}
