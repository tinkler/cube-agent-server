// Package configloader 加载数据源配置
// 独立小工具,避免循环依赖
package configloader

import (
	"fmt"
	"os"

	"github.com/tinkler/cube-agent-server/internal/engine/source"
	"gopkg.in/yaml.v3"
)

// LoadDataSources 加载 datasources.yaml(相对工作目录)
func LoadDataSources() ([]*source.DataSourceConfig, error) {
	candidates := []string{
		"config/datasources.yaml",
		"./config/datasources.yaml",
		"../config/datasources.yaml",
	}
	var data []byte
	var err error
	for _, p := range candidates {
		data, err = os.ReadFile(p)
		if err == nil {
			break
		}
	}
	if data == nil {
		// 文件不存在不算错,只是没数据源
		return nil, nil
	}
	cfg := struct {
		DataSources []*source.DataSourceConfig `yaml:"datasources"`
	}{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse datasources.yaml: %w", err)
	}
	return cfg.DataSources, nil
}
