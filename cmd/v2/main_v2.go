package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hdt3213/rdb/parser"
)

// Config 配置参数
type Config struct {
	InputFile     string
	OutputFile    string
	ThresholdKB   int
	MaxKeys       int
	PrefixDepth   int
	TopN          int
	ShowProgress  bool
	SkipErrors    bool
	Mode          string // "bigkey", "prefix", "both", "flame", "all"
	FlameOutput   string
	FlameDepth    int
	FlameMinValue int64
	FlameFormat   string // "json", "collapsed", "csv"
	Separator     string
	GroupByType   bool
}

// KeyAnalysis 大Key分析结果
type KeyAnalysis struct {
	Database int
	Type     string
	Key      string
	Size     int64
	Elements int
	Encoding string
	Expiry   int64
	TTL      int64
}

// PrefixStat 前缀统计结果
type PrefixStat struct {
	Prefix       string
	Depth        int
	Size         int64
	Count        int64
	Database     int
	SizeReadable string
	AvgSize      float64
}

// FlameNode 火焰图节点
type FlameNode struct {
	Name     string       `json:"name"`
	Value    int64        `json:"value"`
	Children []*FlameNode `json:"children,omitempty"`
	Type     string       `json:"type,omitempty"`
	Database int          `json:"database,omitempty"`
	AvgSize  float64      `json:"avgSize,omitempty"`
	Count    int64        `json:"count,omitempty"`
}

// StreamAnalyzer 流式分析器
type StreamAnalyzer struct {
	Config
	totalKeys        int
	bigKeys          []KeyAnalysis
	prefixStats      map[string]*PrefixStat
	prefixStatsByDB  map[int]map[string]*PrefixStat
	flameRoot        *FlameNode
	flameStats       map[string]int64 // 用于快速查找的平面映射
	flameStatsByType map[string]map[string]int64
	startTime        time.Time
	bytesRead        int64
	fileSize         int64
	errorCount       int
	skippedKeys      int
	totalSize        int64
	keyTypes         map[string]int
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

	// 设置默认火焰图最小值
	if config.FlameMinValue <= 0 {
		config.FlameMinValue = 1024 // 1KB
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

func (s *StreamAnalyzer) Analyze() error {
	// 打开 RDB 文件
	file, err := os.Open(s.InputFile)
	if err != nil {
		return fmt.Errorf("无法打开RDB文件: %v", err)
	}
	// 关闭文件流
	defer s.closeFile(file)

	// 获取文件大小
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("无法获取文件信息: %v", err)
	}
	s.fileSize = fileInfo.Size()

	if s.ShowProgress {
		fmt.Printf("开始分析RDB文件: %s (大小: %.2f GB)\n",
			s.InputFile, float64(s.fileSize)/(1024*1024*1024))
		fmt.Printf("分析模式: %s\n", s.Mode)

		if s.Mode == "bigkey" || s.Mode == "both" || s.Mode == "all" {
			fmt.Printf("大Key分析 | 阈值: %dKB\n", s.ThresholdKB)
		}

		if s.Mode == "prefix" || s.Mode == "both" || s.Mode == "all" {
			fmt.Printf("前缀分析 | 最大深度: %d | TopN: %d\n", s.PrefixDepth, s.TopN)
		}

		if s.Mode == "flame" || s.Mode == "all" {
			fmt.Printf("火焰图分析 | 深度: %d | 格式: %s\n", s.FlameDepth, s.FlameFormat)
		}

		fmt.Printf("键分隔符: '%s'\n", s.Separator)
		fmt.Printf("跳过错误: %v\n", s.SkipErrors)
	}

	// 运行内存监控
	if s.ShowProgress {
		go s.monitorMemory()
	}

	// 创建解析器
	decoder := parser.NewDecoder(file)

	// 解析回调函数
	err = decoder.Parse(func(o parser.RedisObject) bool {
		// 使用 defer 恢复 panic
		defer func() {
			if r := recover(); r != nil {
				s.errorCount++
				if s.ShowProgress && s.errorCount <= 10 { // 只显示前10个错误
					fmt.Printf("\n[警告] 处理Key时发生panic: %v\n", r)
				}
				if s.errorCount > 100 && !s.SkipErrors {
					panic(fmt.Sprintf("发生过多错误(%d)，停止处理", s.errorCount))
				}
			}
		}()

		s.totalKeys++

		// 每处理 10000个 key 显示进度
		if s.ShowProgress && s.totalKeys%10000 == 0 {
			elapsed := time.Since(s.startTime)
			rate := float64(s.totalKeys) / elapsed.Seconds()
			memStats := s.getMemoryUsage()

			progressInfo := s.getProgressInfo()

			fmt.Printf("\r进度: 已处理: %d | %s | 速度: %.0f keys/sec | 内存: %.1fMB | 错误: %d",
				s.totalKeys, progressInfo, rate, memStats.AllocMB, s.errorCount)
		}

		// 解析 key
		analysis, err := s.parseObjectSafe(o)
		if err != nil {
			s.skippedKeys++
			return true // 继续处理下一个
		}

		// 更新总大小
		s.totalSize += analysis.Size

		// 更新键类型统计
		s.keyTypes[analysis.Type]++

		// 大 Key 分析
		if s.Mode == "bigkey" || s.Mode == "both" || s.Mode == "all" {
			if analysis.Size > int64(s.ThresholdKB*1024) {
				s.bigKeys = append(s.bigKeys, analysis)

				// 如果达到最大数量限制，停止处理
				if s.MaxKeys > 0 && len(s.bigKeys) >= s.MaxKeys {
					return false
				}
			}
		}

		// 前缀分析
		if s.Mode == "prefix" || s.Mode == "both" || s.Mode == "all" {
			s.updatePrefixStats(analysis)
		}

		// 火焰图分析
		if s.Mode == "flame" || s.Mode == "all" {
			s.updateFlameStats(analysis)
		}

		return true
	})

	if err != nil {
		return fmt.Errorf("解析 RDB 文件失败: %v", err)
	}

	if s.ShowProgress {
		fmt.Println() // 换行
	}

	return nil
}

// 安全关闭文件
func (s *StreamAnalyzer) closeFile(file *os.File) {
	if file != nil {
		if closeErr := file.Close(); closeErr != nil {
			log.Printf("关闭文件时发生错误: %v", closeErr)
		}
	}
}

// 获取进度信息
func (s *StreamAnalyzer) getProgressInfo() string {
	var info []string

	if s.Mode == "bigkey" || s.Mode == "both" || s.Mode == "all" {
		info = append(info, fmt.Sprintf("大Key: %d", len(s.bigKeys)))
	}

	if s.Mode == "prefix" || s.Mode == "both" || s.Mode == "all" {
		info = append(info, fmt.Sprintf("前缀: %d", len(s.prefixStats)))
	}

	if s.Mode == "flame" || s.Mode == "all" {
		info = append(info, fmt.Sprintf("火焰节点: %d", len(s.flameStats)))
	}

	return strings.Join(info, " | ")
}

// 更新前缀统计
func (s *StreamAnalyzer) updatePrefixStats(analysis KeyAnalysis) {
	dbIndex := analysis.Database
	key := analysis.Key

	// 初始化数据库的前缀统计 map
	if _, exists := s.prefixStatsByDB[dbIndex]; !exists {
		s.prefixStatsByDB[dbIndex] = make(map[string]*PrefixStat)
	}

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

// 安全地解析对象
func (s *StreamAnalyzer) parseObjectSafe(o parser.RedisObject) (KeyAnalysis, error) {
	analysis := KeyAnalysis{
		Database: o.GetDBIndex(),
		Type:     o.GetType(),
		Key:      o.GetKey(),
	}

	// 安全地获取过期时间
	if expiry := o.GetExpiration(); expiry != nil {
		analysis.Expiry = expiry.Unix()
		analysis.TTL = analysis.Expiry - time.Now().Unix()
		if analysis.TTL < 0 {
			analysis.TTL = 0
		}
	}

	// 根据类型计算大小
	switch o.GetType() {
	case "string":
		if str, ok := o.(*parser.StringObject); ok {
			analysis.Size = int64(len(str.Key) + len(str.Value))
			analysis.Elements = 1
			analysis.Encoding = str.Encoding
		} else {
			return analysis, fmt.Errorf("string 类型断言失败")
		}

	case "list":
		if list, ok := o.(*parser.ListObject); ok {
			analysis.Size = int64(len(list.Key))
			for _, item := range list.Values {
				analysis.Size += int64(len(item))
			}
			analysis.Elements = len(list.Values)
			analysis.Encoding = list.Encoding
		} else {
			return analysis, fmt.Errorf("list 类型断言失败")
		}

	case "hash":
		if hash, ok := o.(*parser.HashObject); ok {
			analysis.Size = int64(len(hash.Key))
			for field, value := range hash.Hash {
				analysis.Size += int64(len(field) + len(value))
			}
			analysis.Elements = len(hash.Hash)
			analysis.Encoding = hash.Encoding
		} else {
			return analysis, fmt.Errorf("hash 类型断言失败")
		}

	case "set":
		if set, ok := o.(*parser.SetObject); ok {
			analysis.Size = int64(len(set.Key))
			for _, member := range set.Members {
				analysis.Size += int64(len(member))
			}
			analysis.Elements = len(set.Members)
			analysis.Encoding = set.Encoding
		} else {
			return analysis, fmt.Errorf("set 类型断言失败")
		}

	case "zset":
		if zset, ok := o.(*parser.ZSetObject); ok {
			analysis.Size = int64(len(zset.Key))
			for _, entry := range zset.Entries {
				analysis.Size += int64(len(entry.Member) + 8) // 8字节给分数
			}
			analysis.Elements = len(zset.Entries)
			analysis.Encoding = zset.Encoding
		} else {
			return analysis, fmt.Errorf("zset 类型断言失败")
		}

	case "module", "module2", "stream":
		// Redis 8.3 可能包含新类型，跳过但不报错
		analysis.Size = 0
		return analysis, fmt.Errorf("跳过未支持的类型: %s", o.GetType())

	default:
		analysis.Size = 0
		return analysis, fmt.Errorf("未知类型: %s", o.GetType())
	}

	return analysis, nil
}

// SaveFlameResults 保存火焰图结果
func (s *StreamAnalyzer) SaveFlameResults(outputFile string) error {
	if s.FlameFormat == "json" {
		return s.saveFlameJson(outputFile)
	} else if s.FlameFormat == "collapsed" {
		return s.saveFlameCollapsed(outputFile)
	} else if s.FlameFormat == "csv" {
		return s.saveFlameCSV(outputFile)
	}

	return fmt.Errorf("不支持的火焰图格式: %s", s.FlameFormat)
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

// MemoryStats 获取内存使用情况
type MemoryStats struct {
	AllocMB   float64
	TotalMB   float64
	SysMB     float64
	NumGC     uint32
	HeapAlloc float64
	HeapSys   float64
}

func (s *StreamAnalyzer) getMemoryUsage() MemoryStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return MemoryStats{
		AllocMB:   float64(m.Alloc) / 1024 / 1024,
		TotalMB:   float64(m.TotalAlloc) / 1024 / 1024,
		SysMB:     float64(m.Sys) / 1024 / 1024,
		NumGC:     m.NumGC,
		HeapAlloc: float64(m.HeapAlloc) / 1024 / 1024,
		HeapSys:   float64(m.HeapSys) / 1024 / 1024,
	}
}

// 添加内存监控函数
func (s *StreamAnalyzer) monitorMemory() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		memStats := s.getMemoryUsage()
		fmt.Printf("\n[内存监控] Alloc=%.1fMB, Total=%.1fMB, Sys=%.1fMB, HeapAlloc=%.1fMB, GC次数=%d\n",
			memStats.AllocMB, memStats.TotalMB, memStats.SysMB, memStats.HeapAlloc, memStats.NumGC)
	}
}

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

func main() {
	// 命令行参数
	inputFile := flag.String("input", "", "输入 RDB 文件路径")
	outputFile := flag.String("output", "", "输出 CSV 文件路径 (大Key分析)")
	prefixOutputFile := flag.String("prefix-output", "", "输出 CSV 文件路径 (前缀分析)")
	flameOutputFile := flag.String("flame-output", "", "输出文件路径 (火焰图分析)")
	mode := flag.String("mode", "all", "分析模式: bigkey, prefix, flame, both, all")
	thresholdKB := flag.Int("threshold", 3, "大 Key 阈值(单位：KB), 默认:3")
	maxKeys := flag.Int("max", 10000, "最多记录的大 Key 数量, 默认:10000")
	prefixDepth := flag.Int("prefix-depth", 3, "前缀分析的最大深度, 默认:3")
	topN := flag.Int("topn", 100, "前缀分析的TopN数量, 默认:100")
	flameDepth := flag.Int("flame-depth", 5, "火焰图分析的最大深度, 默认:5")
	flameMinValue := flag.Int64("flame-min", 1024, "火焰图分析的最小值(字节), 默认:1024(1KB)")
	flameFormat := flag.String("flame-format", "json", "火焰图输出格式: json, collapsed, csv, 默认:json")
	separator := flag.String("separator", ":", "键分隔符, 默认:':'")
	groupByType := flag.Bool("group-by-type", false, "按数据类型分组火焰图")
	skipErrors := flag.Bool("skip-errors", true, "遇到错误时跳过而不是停止")

	flag.Parse()

	if *inputFile == "" {
		fmt.Println("请输入 RDB 文件路径")
		fmt.Println("用法: rdb-analyzer -input <rdb文件> [-mode <bigkey|prefix|flame|both|all>]")
		fmt.Println("\n大Key分析参数:")
		fmt.Println("  -output <文件>       输出文件路径")
		fmt.Println("  -threshold <KB>      大Key阈值(默认:3KB)")
		fmt.Println("  -max <数量>          最大记录数(默认:10000)")
		fmt.Println("\n前缀分析参数:")
		fmt.Println("  -prefix-output <文件> 前缀输出文件路径")
		fmt.Println("  -prefix-depth <深度>  前缀分析最大深度(默认:3)")
		fmt.Println("  -topn <数量>          输出前N个前缀(默认:100)")
		fmt.Println("\n火焰图分析参数:")
		fmt.Println("  -flame-output <文件>  火焰图输出文件路径")
		fmt.Println("  -flame-depth <深度>   火焰图最大深度(默认:5)")
		fmt.Println("  -flame-min <字节>     火焰图最小值(默认:1024)")
		fmt.Println("  -flame-format <格式>  输出格式: json, collapsed, csv (默认:json)")
		fmt.Println("  -group-by-type        按数据类型分组火焰图")
		fmt.Println("\n通用参数:")
		fmt.Println("  -separator <字符>     键分隔符(默认:':')")
		fmt.Println("  -skip-errors          跳过错误继续处理(默认:true)")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// 验证文件存在
	if _, err := os.Stat(*inputFile); os.IsNotExist(err) {
		log.Fatalf("RDB 文件不存在: %s", *inputFile)
	}

	// 验证模式参数
	validModes := map[string]bool{
		"bigkey": true, "prefix": true, "flame": true, "both": true, "all": true,
	}
	if !validModes[*mode] {
		log.Fatalf("无效的模式: %s，必须是 bigkey, prefix, flame, both 或 all", *mode)
	}

	// 验证火焰图格式
	validFlameFormats := map[string]bool{
		"json": true, "collapsed": true, "csv": true,
	}
	if !validFlameFormats[*flameFormat] {
		log.Fatalf("无效的火焰图格式: %s，必须是 json, collapsed 或 csv", *flameFormat)
	}

	// 设置输出文件默认值
	if *mode == "bigkey" || *mode == "both" || *mode == "all" {
		if *outputFile == "" {
			*outputFile = strings.TrimSuffix(*inputFile, ".rdb") + "_bigkeys.csv"
		}
	}

	if *mode == "prefix" || *mode == "both" || *mode == "all" {
		if *prefixOutputFile == "" {
			*prefixOutputFile = strings.TrimSuffix(*inputFile, ".rdb") + "_prefix.csv"
		}
	}

	if *mode == "flame" || *mode == "all" {
		if *flameOutputFile == "" {
			*flameOutputFile = strings.TrimSuffix(*inputFile, ".rdb") + "_flame." + *flameFormat
		}
	}

	if *thresholdKB < 0 {
		log.Fatalf("阈值不能小于0")
	}

	if *prefixDepth <= 0 {
		log.Fatalf("前缀深度必须大于0")
	}

	if *topN <= 0 {
		log.Fatalf("topn必须大于0")
	}

	if *flameDepth <= 0 {
		log.Fatalf("火焰图深度必须大于0")
	}

	if *flameMinValue < 0 {
		log.Fatalf("火焰图最小值不能小于0")
	}

	// 配置参数
	config := Config{
		InputFile:     *inputFile,
		OutputFile:    *outputFile,
		ThresholdKB:   *thresholdKB,
		MaxKeys:       *maxKeys,
		PrefixDepth:   *prefixDepth,
		TopN:          *topN,
		ShowProgress:  true,
		SkipErrors:    *skipErrors,
		Mode:          *mode,
		FlameOutput:   *flameOutputFile,
		FlameDepth:    *flameDepth,
		FlameMinValue: *flameMinValue,
		FlameFormat:   *flameFormat,
		Separator:     *separator,
		GroupByType:   *groupByType,
	}

	// 创建分析器
	analyzer := NewStreamAnalyzer(config)

	// 开始分析
	fmt.Println("开始流式分析RDB文件...")
	fmt.Printf("分析模式: %s\n", config.Mode)
	if config.SkipErrors {
		fmt.Println("如果遇到错误，程序会尝试跳过并继续处理...")
	}

	// 记录开始时间
	startTime := time.Now()

	if err := analyzer.Analyze(); err != nil {
		log.Fatalf("分析失败: %v", err)
	}

	// 保存结果
	if config.Mode == "bigkey" || config.Mode == "both" || config.Mode == "all" {
		if err := analyzer.SaveBigKeyResults(); err != nil {
			log.Fatalf("保存大Key结果失败: %v", err)
		}
		fmt.Printf("大Key分析结果已保存到: %s\n", config.OutputFile)
	}

	if config.Mode == "prefix" || config.Mode == "both" || config.Mode == "all" {
		if err := analyzer.SavePrefixResults(*prefixOutputFile); err != nil {
			log.Fatalf("保存前缀分析结果失败: %v", err)
		}
		fmt.Printf("前缀分析结果已保存到: %s\n", *prefixOutputFile)
	}

	if config.Mode == "flame" || config.Mode == "all" {
		if err := analyzer.SaveFlameResults(config.FlameOutput); err != nil {
			log.Fatalf("保存火焰图结果失败: %v", err)
		}
		fmt.Printf("火焰图结果已保存到: %s\n", config.FlameOutput)
	}

	// 统计耗时情况
	elapsed := time.Since(startTime)
	log.Printf("分析完成! 耗时: %v", elapsed)

	// 打印摘要
	analyzer.PrintSummary()
}
