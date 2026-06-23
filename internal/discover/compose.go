package discover

import "gopkg.in/yaml.v3"

// ComposeFile is the subset of docker-compose.yml we read.
type ComposeFile struct {
	Services map[string]ComposeService `yaml:"services"`
}

// ComposeService is a single entry under services:.
type ComposeService struct {
	Image       string             `yaml:"image"`
	Ports       []string           `yaml:"ports"`
	Healthcheck ComposeHealthcheck `yaml:"healthcheck"`
}

// ComposeHealthcheck mirrors the healthcheck block.
type ComposeHealthcheck struct {
	Test []string `yaml:"test"`
}

// ParseCompose decodes a docker-compose.yml document.
func ParseCompose(data []byte) (*ComposeFile, error) {
	var f ComposeFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}
