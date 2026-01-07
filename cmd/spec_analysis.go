package main

import (
	"fmt"
	"strings"
)

// UpdateSpecStats 使用指定前缀进行统计
func (s *StreamAnalyzer) UpdateSpecStats(analysis KeyAnalysis) {
	dbIndex := analysis.Database

	// 原始KEY，该模式下是使用原始KEY进行统计的
	key := analysis.Key

	upperKey := strings.ToUpper(key)

	// 指定的 Key 前缀
	specKey := strings.ToUpper(s.Config.SpecKey)

	// 如果不匹配的就忽略掉
	if !strings.HasPrefix(upperKey, specKey) {
		return
	}

	fmt.Printf("检测到前缀指定KEY: %s\n", key)

	// 更新全局前缀统计
	s.prefixStats[key] = &PrefixStat{
		Prefix:   key,
		Depth:    0, // 对于预定义前缀,固定配置0即可,因为不应该截取
		Size:     analysis.Size,
		Count:    1,
		Database: dbIndex,
		AvgSize:  float64(analysis.Size),
	}

	// 更新数据库内前缀统计
	s.prefixStatsByDB[dbIndex][key] = &PrefixStat{
		Prefix:   key,
		Depth:    0, // 对于预定义前缀,固定配置0即可,因为不应该截取
		Size:     analysis.Size,
		Count:    1,
		Database: dbIndex,
		AvgSize:  float64(analysis.Size),
	}
}
