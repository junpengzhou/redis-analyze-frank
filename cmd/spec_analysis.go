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
	s.specStats[key] = &SpecStat{
		Database: dbIndex,       // 数据库角标
		Type:     analysis.Type, // 数据类型
		Key:      key,           // KEY
		Size:     analysis.Size, // 大小
		Ttl:      analysis.TTL,  // 过期时间
	}
}
