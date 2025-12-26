package main

import (
	"fmt"
	"strings"
)

// 更新火焰图统计 - 支持预定义前缀
func (s *StreamAnalyzer) updateFlameStats(analysis KeyAnalysis) {
	// 过滤小值
	if analysis.Size < s.FlameMinValue {
		return
	}

	dbIndex := analysis.Database
	key := analysis.Key

	var flamePath []string

	// 如果有配置的前缀管理器，优先使用预定义前缀
	if s.PrefixConfigManager != nil {
		matchedPrefix := s.PrefixConfigManager.MatchPrefix(key)
		// 使用匹配的预定义前缀构建火焰图路径
		flamePath = s.buildFlamePathWithConfiguredPrefix(dbIndex, matchedPrefix, analysis)
	} else {
		// 使用原有的分隔符方式
		flamePath = s.buildFlamePathWithSeparator(dbIndex, key, analysis)
	}

	// 构建火焰图路径字符串
	flamePathStr := strings.Join(flamePath, s.Separator)

	// 更新火焰图统计
	s.flameStats[flamePathStr] += analysis.Size

	// 按类型分组统计
	if s.GroupByType {
		if _, exists := s.flameStatsByType[analysis.Type]; !exists {
			s.flameStatsByType[analysis.Type] = make(map[string]int64)
		}
		s.flameStatsByType[analysis.Type][flamePathStr] += analysis.Size
	}

	// 更新火焰图树结构
	s.updateFlameTree(flamePath, analysis)
}

// 使用预定义前缀构建火焰图路径
func (s *StreamAnalyzer) buildFlamePathWithConfiguredPrefix(dbIndex int, matchedPrefix string, analysis KeyAnalysis) []string {
	var flamePath []string

	// 添加数据库节点
	if s.GroupByType {
		flamePath = append(flamePath, fmt.Sprintf("db%d", dbIndex))
		flamePath = append(flamePath, analysis.Type)
	} else {
		flamePath = append(flamePath, fmt.Sprintf("db%d_%s", dbIndex, analysis.Type))
	}

	// 添加匹配的预定义前缀
	flamePath = append(flamePath, matchedPrefix)

	// 如果还有剩余部分，添加剩余部分
	remainingPart := strings.TrimPrefix(analysis.Key, matchedPrefix)
	if remainingPart != "" {
		remainingParts := strings.Split(remainingPart, s.Separator)
		maxDepth := s.FlameDepth - len(flamePath) // 预留空间给预定义前缀
		if maxDepth > 0 {
			for i := 0; i < maxDepth && i < len(remainingParts); i++ {
				if remainingParts[i] != "" {
					flamePath = append(flamePath, remainingParts[i])
				}
			}
		}
	}

	return flamePath
}

// 使用分隔符构建火焰图路径
func (s *StreamAnalyzer) buildFlamePathWithSeparator(dbIndex int, key string, analysis KeyAnalysis) []string {
	// 分割键
	parts := s.splitKey(key)

	// 构建火焰图路径
	var flamePath []string

	// 添加数据库节点
	if s.GroupByType {
		flamePath = append(flamePath, fmt.Sprintf("db%d", dbIndex))
		flamePath = append(flamePath, analysis.Type)
	} else {
		flamePath = append(flamePath, fmt.Sprintf("db%d_%s", dbIndex, analysis.Type))
	}

	// 添加键的各个部分
	maxDepth := s.FlameDepth
	if maxDepth <= 0 || maxDepth > len(parts) {
		maxDepth = len(parts)
	}

	for i := 0; i < maxDepth && i < len(parts); i++ {
		flamePath = append(flamePath, parts[i])
	}

	// 如果还有剩余部分，添加为"..."表示剩余
	if maxDepth < len(parts) {
		flamePath = append(flamePath, "...")
	}

	return flamePath
}

// 更新火焰图树结构
func (s *StreamAnalyzer) updateFlameTree(path []string, analysis KeyAnalysis) {
	current := s.flameRoot
	current.Value += analysis.Size

	for i, part := range path {
		// 查找子节点
		found := false
		for _, child := range current.Children {
			if child.Name == part {
				child.Value += analysis.Size
				child.Count++
				child.AvgSize = float64(child.Value) / float64(child.Count)
				current = child
				found = true
				break
			}
		}

		// 如果没找到，创建新节点
		if !found {
			newNode := &FlameNode{
				Name:     part,
				Value:    analysis.Size,
				Children: make([]*FlameNode, 0),
				Count:    1,
				AvgSize:  float64(analysis.Size),
			}

			// 如果是类型节点，设置类型
			if i == 1 && s.GroupByType {
				newNode.Type = part
			}

			current.Children = append(current.Children, newNode)
			current = newNode
		}
	}
}
