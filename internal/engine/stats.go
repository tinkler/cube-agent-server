// Package engine stats - 运行时统计
// 记录每个 query 的耗时/行数/错误;W4 接 Prometheus
package engine

import (
	"sync"
	"sync/atomic"
	"time"
)

// Stats 全局统计
type Stats struct {
	mu             sync.RWMutex
	QueryTotal     int64 // atomic
	QuerySuccess   int64 // atomic
	QueryErrors    int64 // atomic
	PerCube        map[string]*CubeStats
	PerDatasource  map[string]*DatasourceStats
}

// CubeStats 单 cube 统计
type CubeStats struct {
	Name         string
	QueryCount   int64
	SuccessCount int64
	ErrorCount   int64
	TotalMs      int64
	MaxMs        int64
	LastQueryAt  time.Time
}

// DatasourceStats 单数据源统计
type DatasourceStats struct {
	Name         string
	Driver       string
	QueryCount   int64
	SuccessCount int64
	ErrorCount   int64
	TotalMs      int64
	MaxMs        int64
	LastError    string
	LastErrorAt  time.Time
}

// NewStats 构造
func NewStats() *Stats {
	return &Stats{
		PerCube:       map[string]*CubeStats{},
		PerDatasource: map[string]*DatasourceStats{},
	}
}

// Record 记录一次 query
func (s *Stats) Record(cube, dsName, driver string, durationMs int64, rowCount int, err error) {
	atomic.AddInt64(&s.QueryTotal, 1)
	if err != nil {
		atomic.AddInt64(&s.QueryErrors, 1)
	} else {
		atomic.AddInt64(&s.QuerySuccess, 1)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if cube != "" {
		cs, ok := s.PerCube[cube]
		if !ok {
			cs = &CubeStats{Name: cube}
			s.PerCube[cube] = cs
		}
		cs.QueryCount++
		if err != nil {
			cs.ErrorCount++
		} else {
			cs.SuccessCount++
		}
		cs.TotalMs += durationMs
		if durationMs > cs.MaxMs {
			cs.MaxMs = durationMs
		}
		cs.LastQueryAt = time.Now()
	}

	if dsName != "" {
		ds, ok := s.PerDatasource[dsName]
		if !ok {
			ds = &DatasourceStats{Name: dsName}
			s.PerDatasource[dsName] = ds
		}
		ds.Driver = driver
		ds.QueryCount++
		if err != nil {
			ds.ErrorCount++
			ds.LastError = err.Error()
			ds.LastErrorAt = time.Now()
		} else {
			ds.SuccessCount++
		}
		ds.TotalMs += durationMs
		if durationMs > ds.MaxMs {
			ds.MaxMs = durationMs
		}
	}
}

// Snapshot 全局统计快照
type StatsSnapshot struct {
	QueryTotal    int64                 `json:"query_total"`
	QuerySuccess  int64                 `json:"query_success"`
	QueryErrors   int64                 `json:"query_errors"`
	PerCube       map[string]*CubeStats `json:"per_cube"`
	PerDatasource map[string]*DatasourceStats `json:"per_datasource"`
}

// Snapshot 返回
func (s *Stats) Snapshot() StatsSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	perCube := make(map[string]*CubeStats, len(s.PerCube))
	for k, v := range s.PerCube {
		cp := *v
		perCube[k] = &cp
	}
	perDS := make(map[string]*DatasourceStats, len(s.PerDatasource))
	for k, v := range s.PerDatasource {
		cp := *v
		perDS[k] = &cp
	}
	return StatsSnapshot{
		QueryTotal:    atomic.LoadInt64(&s.QueryTotal),
		QuerySuccess:  atomic.LoadInt64(&s.QuerySuccess),
		QueryErrors:   atomic.LoadInt64(&s.QueryErrors),
		PerCube:       perCube,
		PerDatasource: perDS,
	}
}
