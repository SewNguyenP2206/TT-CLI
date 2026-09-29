package content

import (
	_ "embed"

	"github.com/tt-dev/tt/internal/core"
	"gopkg.in/yaml.v3"
)

//go:embed rules.yaml
var rulesYAML []byte

func Rules() (core.Rules, error) {
	var rules core.Rules
	err := yaml.Unmarshal(rulesYAML, &rules)
	return rules, err
}