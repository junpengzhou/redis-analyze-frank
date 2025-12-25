package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SaveBigKeyResults 保存大 Key 分析结果
func (s *StreamAnalyzer) SaveBigKeyResults() error {
	// 按大小排序
	s.sortBigKeysBySize()

	// 保存到 CSV 文件
	file, err := os.Create(s.OutputFile)
	if err != nil {
		return fmt.Errorf("无法创建输出文件: %v", err)
	}
	// 关闭文件流
	defer s.closeFile(file)

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 写入表头
	header := []string{
		"database", "type", "key", "size_bytes", "size_kb",
		"size_mb", "elements", "encoding", "expiry", "ttl_seconds",
	}
	if err := writer.Write(header); err != nil {
		return err
	}

	// 写入数据
	for _, key := range s.bigKeys {
		row := []string{
			strconv.Itoa(key.Database),
			key.Type,
			key.Key,
			strconv.FormatInt(key.Size, 10),
			strconv.FormatFloat(float64(key.Size)/1024, 'f', 2, 64),
			strconv.FormatFloat(float64(key.Size)/(1024*1024), 'f', 2, 64),
			strconv.Itoa(key.Elements),
			key.Encoding,
			strconv.FormatInt(key.Expiry, 10),
			strconv.FormatInt(key.TTL, 10),
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}

// SavePrefixResults 保存前缀分析结果
func (s *StreamAnalyzer) SavePrefixResults(outputFile string) error {
	// 获取排序后的前缀统计
	prefixStats := s.getSortedPrefixStats()

	// 限制输出数量
	if s.TopN > 0 && len(prefixStats) > s.TopN {
		prefixStats = prefixStats[:s.TopN]
	}

	// 保存到 CSV 文件
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("无法创建前缀输出文件: %v", err)
	}
	// 关闭文件流
	defer s.closeFile(file)

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 写入表头
	header := []string{
		"database", "prefix", "depth", "size_bytes", "size_readable",
		"key_count", "avg_size_bytes", "avg_size_readable",
	}
	if err := writer.Write(header); err != nil {
		return err
	}

	// 写入数据
	for _, stat := range prefixStats {
		// 计算可读大小
		sizeReadable := s.formatBytes(stat.Size)
		avgSizeReadable := s.formatBytes(int64(stat.AvgSize))

		row := []string{
			strconv.Itoa(stat.Database),
			stat.Prefix,
			strconv.Itoa(stat.Depth),
			strconv.FormatInt(stat.Size, 10),
			sizeReadable,
			strconv.FormatInt(stat.Count, 10),
			strconv.FormatFloat(stat.AvgSize, 'f', 2, 64),
			avgSizeReadable,
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}

// SaveFlameResults 保存火焰图结果
func (s *StreamAnalyzer) SaveFlameResults(outputFile string) error {
	if s.FlameFormat == "json" {
		return s.saveFlameJson(outputFile)
	} else if s.FlameFormat == "folded" {
		return s.saveFlameCollapsed(outputFile)
	} else if s.FlameFormat == "csv" {
		return s.saveFlameCSV(outputFile)
	}
	// 默认采用折叠格式保存
	return s.saveFlameCollapsed(outputFile)
}

// 获取排序后的前缀统计
func (s *StreamAnalyzer) getSortedPrefixStats() []PrefixStat {
	// 转换为切片
	stats := make([]PrefixStat, 0, len(s.prefixStats))
	for _, stat := range s.prefixStats {
		stats = append(stats, *stat)
	}

	// 按大小降序排序
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Size > stats[j].Size
	})

	return stats
}

// 格式化字节数为可读格式
func (s *StreamAnalyzer) formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	var (
		value float64
		unit  string
	)

	switch {
	case bytes >= TB:
		value = float64(bytes) / TB
		unit = "TB"
	case bytes >= GB:
		value = float64(bytes) / GB
		unit = "GB"
	case bytes >= MB:
		value = float64(bytes) / MB
		unit = "MB"
	case bytes >= KB:
		value = float64(bytes) / KB
		unit = "KB"
	default:
		value = float64(bytes)
		unit = "B"
	}

	return fmt.Sprintf("%.2f%s", value, unit)
}

// 大 Key 按大小排序
func (s *StreamAnalyzer) sortBigKeysBySize() {
	if len(s.bigKeys) < 2 {
		return
	}

	sort.Slice(s.bigKeys, func(i, j int) bool {
		return s.bigKeys[i].Size > s.bigKeys[j].Size
	})
}

// 保存 JSON 格式火焰图
func (s *StreamAnalyzer) saveFlameJson(outputFile string) error {
	// 准备数据
	flameData := struct {
		Root     *FlameNode             `json:"root"`
		Stats    map[string]int64       `json:"stats,omitempty"`
		Metadata map[string]interface{} `json:"metadata"`
	}{
		Root:  s.flameRoot,
		Stats: s.flameStats,
		Metadata: map[string]interface{}{
			"totalKeys":   s.totalKeys,
			"totalSize":   s.totalSize,
			"errorCount":  s.errorCount,
			"skippedKeys": s.skippedKeys,
			"keyTypes":    s.keyTypes,
			"separator":   s.Separator,
			"flameDepth":  s.FlameDepth,
			"analyzeTime": time.Now().Format(time.RFC3339),
		},
	}

	// 保存到 JSON 文件
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("无法创建火焰图输出文件: %v", err)
	}
	// 关闭文件流
	defer s.closeFile(file)

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(flameData); err != nil {
		return fmt.Errorf("编码火焰图数据失败: %v", err)
	}

	return nil
}

// 保存折叠格式火焰图（适用于FlameGraph.pl）
func (s *StreamAnalyzer) saveFlameCollapsed(outputFile string) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("无法创建火焰图输出文件: %v", err)
	}
	// 关闭文件流
	defer s.closeFile(file)

	// 按值排序
	type flameEntry struct {
		path  string
		value int64
	}

	entries := make([]flameEntry, 0, len(s.flameStats))
	for path, value := range s.flameStats {
		entries = append(entries, flameEntry{path, value})
	}

	// 按值降序排序
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].value > entries[j].value
	})

	// 写入数据
	for _, entry := range entries {
		// 过滤小值
		if entry.value < s.FlameMinValue {
			continue
		}

		// 写入格式: 路径 值
		if _, err := fmt.Fprintf(file, "%s %d\n", entry.path, entry.value); err != nil {
			return err
		}
	}

	return nil
}

// 保存 CSV 格式火焰图
func (s *StreamAnalyzer) saveFlameCSV(outputFile string) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("无法创建火焰图输出文件: %v", err)
	}
	// 关闭文件流
	defer s.closeFile(file)

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 写入表头
	header := []string{"path", "size_bytes", "size_readable", "depth", "type", "database"}
	if err := writer.Write(header); err != nil {
		return err
	}

	// 按值排序
	type flameEntry struct {
		path    string
		value   int64
		depth   int
		keyType string
		db      int
	}

	entries := make([]flameEntry, 0, len(s.flameStats))
	for path, value := range s.flameStats {
		// 解析路径获取深度和类型
		parts := strings.Split(path, s.Separator)
		depth := len(parts)

		var keyType string
		var db int

		if len(parts) >= 2 {
			// 尝试解析数据库和类型
			if strings.HasPrefix(parts[0], "db") {
				// 格式: db0_string_key1_key2
				dbPart := strings.TrimPrefix(parts[0], "db")
				if idx := strings.Index(dbPart, "_"); idx != -1 {
					db, _ = strconv.Atoi(dbPart[:idx])
					keyType = dbPart[idx+1:]
				}
			} else if s.GroupByType && len(parts) >= 2 {
				// 格式: db0/string/key1/key2
				dbPart := strings.TrimPrefix(parts[0], "db")
				db, _ = strconv.Atoi(dbPart)
				keyType = parts[1]
			}
		}

		entries = append(entries, flameEntry{
			path:    path,
			value:   value,
			depth:   depth,
			keyType: keyType,
			db:      db,
		})
	}

	// 按值降序排序
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].value > entries[j].value
	})

	// 写入数据
	for _, entry := range entries {
		// 过滤小值
		if entry.value < s.FlameMinValue {
			continue
		}

		row := []string{
			entry.path,
			strconv.FormatInt(entry.value, 10),
			s.formatBytes(entry.value),
			strconv.Itoa(entry.depth),
			entry.keyType,
			strconv.Itoa(entry.db),
		}

		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}
