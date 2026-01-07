package main

import (
	"time"
)

// StreamAnalyzer 流式分析器
type StreamAnalyzer struct {
	Config
	PrefixConfigManager *PrefixConfigManager
	totalKeys           int
	bigKeys             []KeyAnalysis                  // 大 Key 统计结果
	prefixStats         map[string]*PrefixStat         // 前缀统计结果
	specStats           map[string]*SpecStat           // 指定统计结果
	prefixStatsByDB     map[int]map[string]*PrefixStat // 按数据库进行统计
	flameRoot           *FlameNode                     // 火焰图根节点
	flameStats          map[string]int64               // 火焰图统计
	flameStatsByType    map[string]map[string]int64    // 火焰图统计数据类型
	startTime           time.Time
	bytesRead           int64
	fileSize            int64
	errorCount          int
	skippedKeys         int
	totalSize           int64
	keyTypes            map[string]int
}

func NewStreamAnalyzer(config Config) *StreamAnalyzer {
	// 设置默认分隔符
	if config.Separator == "" {
		config.Separator = ":"
	}

	// 设置默认火焰图深度
	if config.FlameDepth <= 0 {
		config.FlameDepth = 5
	}

	// 设置默认火焰图最小值 1024
	if config.FlameMinValue <= 0 {
		config.FlameMinValue = 1024
	}

	// 设置默认火焰图格式
	if config.FlameFormat == "" {
		config.FlameFormat = "json"
	}

	analyzer := &StreamAnalyzer{
		Config:          config,
		bigKeys:         make([]KeyAnalysis, 0, 1000),
		prefixStats:     make(map[string]*PrefixStat),
		prefixStatsByDB: make(map[int]map[string]*PrefixStat),
		flameRoot: &FlameNode{
			Name:     "root",
			Value:    0,
			Children: make([]*FlameNode, 0),
		},
		flameStats:       make(map[string]int64),
		flameStatsByType: make(map[string]map[string]int64),
		startTime:        time.Now(),
		keyTypes:         make(map[string]int),
	}

	return analyzer
}
