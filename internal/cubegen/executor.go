// Package cubegen - 提供全局 plugin registry 给 compiler 调用
package cubegen

import (
	"fmt"
	"sync"

	"github.com/tinkler/cube-agent-server/internal/cubegenapi"
)

// GlobalExecutor 全局 plugin 调用入口(给 Pass3 用)
type GlobalExecutor struct {
	mu      sync.Mutex
	loader  *YaegiLoader
	enabled bool
}

var globalExec = &GlobalExecutor{}

func SetGlobalLoader(l *YaegiLoader) {
	globalExec.mu.Lock()
	defer globalExec.mu.Unlock()
	globalExec.loader = l
	globalExec.enabled = l != nil
}

// CallPlugin 全局调 plugin(给 Pass3 调)
func CallPlugin(path string, ctx *cubegenapi.BuildContext) (*cubegenapi.SQLPlan, error) {
	globalExec.mu.Lock()
	loader := globalExec.loader
	enabled := globalExec.enabled
	globalExec.mu.Unlock()

	if !enabled || loader == nil {
		return nil, fmt.Errorf("cubegen: plugin loader not enabled")
	}
	return loader.CallBuildSQL(path, ctx)
}
