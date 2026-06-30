package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Pair 是一组单向同步映射：from 的 star 同步给 to 列表中每个账号。
type Pair struct {
	From   string   `yaml:"from"`
	To     []string `yaml:"to"`
	Mirror bool     `yaml:"mirror"`
}

// Config 是配置文件根结构。只描述映射关系，不含任何 token。
type Config struct {
	Syncs []Pair `yaml:"syncs"`
}

// Load 读取并校验 YAML 配置文件。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置 %q 失败: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置 %q 失败: %w", path, err)
	}
	if len(cfg.Syncs) == 0 {
		return nil, fmt.Errorf("配置 %q 中 syncs 为空", path)
	}
	for i, p := range cfg.Syncs {
		if p.From == "" {
			return nil, fmt.Errorf("syncs[%d].from 不能为空", i)
		}
		if len(p.To) == 0 {
			return nil, fmt.Errorf("syncs[%d].to 不能为空", i)
		}
	}
	return &cfg, nil
}
