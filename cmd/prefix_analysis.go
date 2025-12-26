package main

import (
	"fmt"
	"strings"
)

// 更新前缀统计
// updatePrefixStats 更新前缀统计 - 使用预定义前缀
func (s *StreamAnalyzer) updatePrefixStats(analysis KeyAnalysis) {
	dbIndex := analysis.Database

	// 初始化数据库的前缀统计 map
	if _, exists := s.prefixStatsByDB[dbIndex]; !exists {
		s.prefixStatsByDB[dbIndex] = make(map[string]*PrefixStat)
	}

	// 如果配置了前缀管理器，使用预定义前缀匹配
	if s.PrefixConfigManager != nil {
		s.updatePrefixStatsWithConfiguredPrefix(analysis)
	} else {
		// 使用原有的分隔符方式
		s.updatePrefixStatsWithSeparator(analysis)
	}
}

// 使用预定义前缀进行统计
func (s *StreamAnalyzer) updatePrefixStatsWithConfiguredPrefix(analysis KeyAnalysis) {
	dbIndex := analysis.Database
	key := analysis.Key

	// 匹配预定义前缀
	matchedPrefix := s.PrefixConfigManager.MatchPrefix(key)

	// 更新全局前缀统计
	globalKey := fmt.Sprintf("%d:%s", dbIndex, matchedPrefix)
	if stat, exists := s.prefixStats[globalKey]; exists {
		stat.Size += analysis.Size
		stat.Count++
		stat.AvgSize = float64(stat.Size) / float64(stat.Count)
	} else {
		s.prefixStats[globalKey] = &PrefixStat{
			Prefix:   matchedPrefix,
			Depth:    0, // 对于预定义前缀,固定配置0即可,因为不应该截取
			Size:     analysis.Size,
			Count:    1,
			Database: dbIndex,
			AvgSize:  float64(analysis.Size),
		}
	}

	// 更新数据库内前缀统计
	if stat, exists := s.prefixStatsByDB[dbIndex][matchedPrefix]; exists {
		stat.Size += analysis.Size
		stat.Count++
		stat.AvgSize = float64(stat.Size) / float64(stat.Count)
	} else {
		s.prefixStatsByDB[dbIndex][matchedPrefix] = &PrefixStat{
			Prefix:   matchedPrefix,
			Depth:    0,
			Size:     analysis.Size,
			Count:    1,
			Database: dbIndex,
			AvgSize:  float64(analysis.Size),
		}
	}
}

// 使用分隔符进行统计（保留原有功能）
func (s *StreamAnalyzer) updatePrefixStatsWithSeparator(analysis KeyAnalysis) {
	dbIndex := analysis.Database
	key := analysis.Key

	// 将 key 按分隔符分割
	parts := s.splitKey(key)

	// 生成从深度1到指定深度的前缀
	maxDepth := s.PrefixDepth
	if maxDepth == 0 || maxDepth > len(parts) {
		maxDepth = len(parts)
	}

	// 构建前缀并统计
	for depth := 1; depth <= maxDepth; depth++ {
		prefix := s.buildPrefix(parts, depth)

		// 更新全局前缀统计
		globalKey := fmt.Sprintf("%d:%s", dbIndex, prefix)
		if stat, exists := s.prefixStats[globalKey]; exists {
			stat.Size += analysis.Size
			stat.Count++
			stat.AvgSize = float64(stat.Size) / float64(stat.Count)
		} else {
			s.prefixStats[globalKey] = &PrefixStat{
				Prefix:   prefix,
				Depth:    depth,
				Size:     analysis.Size,
				Count:    1,
				Database: dbIndex,
				AvgSize:  float64(analysis.Size),
			}
		}

		// 更新数据库内前缀统计
		if stat, exists := s.prefixStatsByDB[dbIndex][prefix]; exists {
			stat.Size += analysis.Size
			stat.Count++
			stat.AvgSize = float64(stat.Size) / float64(stat.Count)
		} else {
			s.prefixStatsByDB[dbIndex][prefix] = &PrefixStat{
				Prefix:   prefix,
				Depth:    depth,
				Size:     analysis.Size,
				Count:    1,
				Database: dbIndex,
				AvgSize:  float64(analysis.Size),
			}
		}
	}
}

// splitKey 分割 key 为部分
func (s *StreamAnalyzer) splitKey(key string) []string {
	// 如果指定了分隔符，使用指定的分隔符
	if s.Separator != "" && s.Separator != ":" && strings.Contains(key, s.Separator) {
		return strings.Split(key, s.Separator)
	}

	// 自动检测常见分隔符
	separators := []string{":", "|", "_", "-", ".", "/", "#", ";"}

	for _, sep := range separators {
		if strings.Contains(key, sep) {
			return strings.Split(key, sep)
		}
	}

	// 如果没有分隔符，整个key作为一个部分
	return []string{key}
}

// 构建前缀
func (s *StreamAnalyzer) buildPrefix(parts []string, depth int) string {
	if depth <= 0 || depth > len(parts) {
		return ""
	}

	// 取前 depth 个部分
	prefixParts := parts[:depth]

	// 使用配置的分隔符
	return strings.Join(prefixParts, s.Separator) + s.Separator
}
