package policy

import (
	"os"

	"gopkg.in/yaml.v3"
)

// PolicyConfig represents user-overridable action policy settings loaded from YAML.
type PolicyConfig struct {
	Commands struct {
		Allow []string `yaml:"allow,omitempty"`
		Deny  []string `yaml:"deny,omitempty"`
	} `yaml:"commands,omitempty"`
	Paths struct {
		Deny []string `yaml:"deny,omitempty"`
	} `yaml:"paths,omitempty"`
	Env struct {
		Deny []string `yaml:"deny,omitempty"`
	} `yaml:"env,omitempty"`
	MCP struct {
		Allow []string `yaml:"allow,omitempty"`
	} `yaml:"mcp,omitempty"`
}

// LoadPolicyConfig reads a policy YAML file. Returns nil if the file does not exist.
func LoadPolicyConfig(path string) (*PolicyConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cfg PolicyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Apply merges the YAML overrides into the DefaultActionPolicy.
// Deny takes precedence over allow (deny removes from the allowed set).
func (o *PolicyConfig) Apply(p *DefaultActionPolicy) {
	if o == nil {
		return
	}
	// Add allowed commands.
	for _, cmd := range o.Commands.Allow {
		p.allowedCmds[cmd] = true
	}
	// Remove denied commands (deny wins over allow).
	for _, cmd := range o.Commands.Deny {
		delete(p.allowedCmds, cmd)
	}
	// Append denied paths.
	p.deniedPaths = append(p.deniedPaths, o.Paths.Deny...)
	// Append denied env keys.
	p.deniedEnvKeys = append(p.deniedEnvKeys, o.Env.Deny...)
	// Add allowed MCP methods.
	for _, method := range o.MCP.Allow {
		p.allowedMCP[method] = true
	}
}
