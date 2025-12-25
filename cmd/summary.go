package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *StreamAnalyzer) PrintSummary() {
	elapsed := time.Since(s.startTime)

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("分析完成!")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("输入文件: %s\n", s.InputFile)
	fmt.Printf("处理时间: %v\n", elapsed)
	fmt.Printf("总Key数: %d\n", s.totalKeys)
	fmt.Printf("总大小: %s\n", s.formatBytes(s.totalSize))
	fmt.Printf("错误数: %d\n", s.errorCount)
	fmt.Printf("跳过Key数: %d\n", s.skippedKeys)

	// 键类型统计
	fmt.Println("\n键类型统计:")
	fmt.Println(strings.Repeat("-", 40))
	for keyType, count := range s.keyTypes {
		percentage := float64(count) / float64(s.totalKeys) * 100
		fmt.Printf("%-10s: %d (%.1f%%)\n", keyType, count, percentage)
	}

	if s.Mode == "bigkey" || s.Mode == "both" || s.Mode == "all" {
		fmt.Printf("\n大Key数(>%dKB): %d\n", s.ThresholdKB, len(s.bigKeys))

		if len(s.bigKeys) > 0 {
			fmt.Println("\nTop 10 大Key:")
			fmt.Println(strings.Repeat("-", 100))
			fmt.Printf("%-8s %-10s %-40s %-12s %-10s\n",
				"DB", "Type", "Key", "Size(MB)", "Elements")
			fmt.Println(strings.Repeat("-", 100))

			topN := 10
			if len(s.bigKeys) < topN {
				topN = len(s.bigKeys)
			}

			for i := 0; i < topN; i++ {
				key := s.bigKeys[i]
				keyDisplay := key.Key
				if len(keyDisplay) > 40 {
					keyDisplay = keyDisplay[:37] + "..."
				}
				fmt.Printf("%-8d %-10s %-40s %-12.2f %-10d\n",
					key.Database, key.Type, keyDisplay,
					float64(key.Size)/(1024*1024), key.Elements)
			}
		}
	}

	if s.Mode == "prefix" || s.Mode == "both" || s.Mode == "all" {
		fmt.Printf("\n前缀统计数: %d\n", len(s.prefixStats))

		prefixStats := s.getSortedPrefixStats()
		topN := 10
		if len(prefixStats) < topN {
			topN = len(prefixStats)
		}

		if topN > 0 {
			fmt.Println("\nTop 10 前缀统计:")
			fmt.Println(strings.Repeat("-", 100))
			fmt.Printf("%-8s %-25s %-6s %-12s %-10s %-8s\n",
				"DB", "Prefix", "Depth", "Size", "Count", "AvgSize")
			fmt.Println(strings.Repeat("-", 100))

			for i := 0; i < topN; i++ {
				stat := prefixStats[i]
				fmt.Printf("%-8d %-25s %-6d %-12s %-10d %-8.2fKB\n",
					stat.Database,
					s.truncateString(stat.Prefix, 25),
					stat.Depth,
					s.formatBytes(stat.Size),
					stat.Count,
					stat.AvgSize/1024)
			}
		}
	}

	if s.Mode == "flame" || s.Mode == "all" {
		fmt.Printf("\n火焰图节点数: %d\n", len(s.flameStats))
		fmt.Printf("火焰图最小阈值: %s\n", s.formatBytes(s.FlameMinValue))

		// 获取Top 10火焰图路径
		type flameEntry struct {
			path  string
			value int64
		}

		entries := make([]flameEntry, 0, len(s.flameStats))
		for path, value := range s.flameStats {
			entries = append(entries, flameEntry{path, value})
		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].value > entries[j].value
		})

		topN := 10
		if len(entries) < topN {
			topN = len(entries)
		}

		if topN > 0 {
			fmt.Println("\nTop 10 火焰图路径:")
			fmt.Println(strings.Repeat("-", 120))
			fmt.Printf("%-60s %-12s\n", "Path", "Size")
			fmt.Println(strings.Repeat("-", 120))

			for i := 0; i < topN; i++ {
				entry := entries[i]
				pathDisplay := entry.path
				if len(pathDisplay) > 60 {
					pathDisplay = pathDisplay[:57] + "..."
				}
				fmt.Printf("%-60s %-12s\n", pathDisplay, s.formatBytes(entry.value))
			}
		}
	}

	fmt.Println(strings.Repeat("=", 60))
}

// 截断字符串
func (s *StreamAnalyzer) truncateString(str string, maxLen int) string {
	if len(str) <= maxLen {
		return str
	}
	return str[:maxLen-3] + "..."
}
