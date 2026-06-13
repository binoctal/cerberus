package ai

import "strings"

type promptBuilder struct {
	system  string
	context string
	task    string
	output  string
}

func NewPrompt() *promptBuilder {
	return &promptBuilder{}
}

func (p *promptBuilder) System(s string) *promptBuilder  { p.system = s; return p }
func (p *promptBuilder) Context(s string) *promptBuilder { p.context = s; return p }
func (p *promptBuilder) Task(s string) *promptBuilder    { p.task = s; return p }
func (p *promptBuilder) Output(s string) *promptBuilder  { p.output = s; return p }

func (p *promptBuilder) Build() string {
	var parts []string
	if p.system != "" {
		parts = append(parts, p.system)
	}
	if p.context != "" {
		parts = append(parts, "\n## Context\n"+p.context)
	}
	if p.task != "" {
		parts = append(parts, "\n## Task\n"+p.task)
	}
	if p.output != "" {
		parts = append(parts, "\n## Output Format\n"+p.output)
	}
	return strings.Join(parts, "\n")
}
