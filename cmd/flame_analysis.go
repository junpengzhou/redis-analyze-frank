package main

import (
	"fmt"
	"strings"
)

// 更新火焰图统计
func (s *StreamAnalyzer) updateFlameStats(analysis KeyAnalysis) {
	// 过滤小值
	if analysis.Size < s.FlameMinValue {
		return
	}

	dbIndex := analysis.Database
	key := analysis.Key

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
