// Package compiler 阉割版 Query 编译器入口
// 3 pass:
//   pass1: JSON → 内部 IR(本文件)
//   pass2: 引用解析(从 schema 找 measure/dimension/segment 定义)
//   pass3: IR → SQL AST → SQL 字符串
package compiler

import (
	"github.com/tinkler/cube-agent-server/internal/compiler/query"
)

// IR Pass1 输出:解析后的内部表示
// 比 query.Query 多一些内部字段,Pass2/Pass3 用
type IR struct {
	Query          *query.Query
	ReferencedCubes []string
}

// Pass1 解析 JSON Query
func Pass1(data []byte) (*IR, error) {
	q, err := query.Parse(data)
	if err != nil {
		return nil, err
	}
	return &IR{
		Query:           q,
		ReferencedCubes: q.ReferencedCubes(),
	}, nil
}
