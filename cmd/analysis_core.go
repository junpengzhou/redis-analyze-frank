package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/hdt3213/rdb/parser"
)

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

	// 开始分析前打印日志
	s.beforeAdbAnalyzePrint()

	// 开启单独的线程运行内存监控
	go s.monitorMemory()

	// 创建解析器
	decoder := parser.NewDecoder(file)

	// 解析回调函数
	err = decoder.Parse(func(o parser.RedisObject) bool {
		s.totalKeys++

		// 每处理 10000个 key 显示进度（当配置需要展示进度时候）
		if s.totalKeys%10000 == 0 {
			s.printProgressInfo()
		}

		// 解析 key
		analysis, err := s.parseObjectSafe(o)
		if err != nil {
			s.skippedKeys++
			return true
		}

		// 更新总大小
		s.totalSize += analysis.Size

		// 更新键类型统计
		s.keyTypes[analysis.Type]++

		// 大 Key 分析
		if s.Mode == "bigkey" || s.Mode == "both" || s.Mode == "all" {
			if analysis.Size > int64(s.ThresholdKB*1024) {
				// 追加大 Key 到数据库
				s.bigKeys = append(s.bigKeys, analysis)
			}
		}

		// 前缀分析
		if s.Mode == "prefix" || s.Mode == "spec" || s.Mode == "both" || s.Mode == "all" {
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

	fmt.Println()

	return nil
}

// printProgressInfo 打印进度
func (s *StreamAnalyzer) printProgressInfo() {
	elapsed := time.Since(s.startTime)
	rate := float64(s.totalKeys) / elapsed.Seconds()
	memStats := s.getMemoryUsage()

	progressInfo := s.getProgressInfo()

	fmt.Printf("\r进度: 已处理: %d | %s | 速度: %.0f keys/sec | 内存: %.1fMB | 错误: %d",
		s.totalKeys, progressInfo, rate, memStats.AllocMB, s.errorCount)
}

// beforeAdbAnalyzePrint 开始前的日志打印工作
func (s *StreamAnalyzer) beforeAdbAnalyzePrint() {
	fmt.Printf("开始分析RDB文件: %s (大小: %.2f GB)\n",
		s.InputFile, float64(s.fileSize)/(1024*1024*1024))
	fmt.Printf("分析模式: %s\n", s.Mode)

	if s.Mode == "bigkey" || s.Mode == "both" || s.Mode == "all" {
		fmt.Printf("大Key分析 | 阈值: %dKB\n", s.ThresholdKB)
	}

	if s.Mode == "spec" || s.Mode == "both" || s.Mode == "all" {
		fmt.Printf("指定分析 | 指定Key: %s\n", s.Config.SpecKey)
	}

	if s.Mode == "prefix" || s.Mode == "both" || s.Mode == "all" {
		fmt.Printf("前缀分析 | 最大深度: %d | TopN: %d\n", s.PrefixDepth, s.TopN)
	}

	if s.Mode == "flame" || s.Mode == "both" || s.Mode == "all" {
		fmt.Printf("火焰图分析 | 深度: %d | 格式: %s\n", s.FlameDepth, s.FlameFormat)
	}

	fmt.Printf("键分隔符: '%s'\n", s.Separator)
	fmt.Printf("跳过错误: %v\n", s.SkipErrors)
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

	if s.Mode == "spec" || s.Mode == "both" || s.Mode == "all" {
		info = append(info, fmt.Sprintf("命中: %d", len(s.prefixStats)))
	}

	if s.Mode == "flame" || s.Mode == "all" {
		info = append(info, fmt.Sprintf("火焰节点: %d", len(s.flameStats)))
	}

	return strings.Join(info, " | ")
}

// parseObjectSafe 安全地解析对象
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
	case parser.StringType:
		str := o.(*parser.StringObject)
		analysis.Size = int64(len(str.Key) + len(str.Value))
		analysis.Elements = 1
		analysis.Encoding = str.Encoding

	case parser.ListType:
		list := o.(*parser.ListObject)
		analysis.Size = int64(len(list.Key))
		for _, item := range list.Values {
			analysis.Size += int64(len(item))
		}
		analysis.Elements = len(list.Values)
		analysis.Encoding = list.Encoding

	case parser.HashType:
		hash := o.(*parser.HashObject)
		analysis.Size = int64(len(hash.Key))
		for field, value := range hash.Hash {
			analysis.Size += int64(len(field) + len(value))
		}
		analysis.Elements = len(hash.Hash)
		analysis.Encoding = hash.Encoding

	case parser.SetType:
		set := o.(*parser.SetObject)
		analysis.Size = int64(len(set.Key))
		for _, member := range set.Members {
			analysis.Size += int64(len(member))
		}
		analysis.Elements = len(set.Members)
		analysis.Encoding = set.Encoding

	case parser.ZSetType:
		zset := o.(*parser.ZSetObject)
		analysis.Size = int64(len(zset.Key))
		for _, entry := range zset.Entries {
			// 8字节是 score 的空间
			analysis.Size += int64(len(entry.Member) + 8)
		}
		analysis.Elements = len(zset.Entries)
		analysis.Encoding = zset.Encoding

	case parser.StreamType:
		analysis.Size = 0
		return analysis, fmt.Errorf("跳过流类型: %s", o.GetType())

	default:
		analysis.Size = 0
		return analysis, fmt.Errorf("未知类型: %s", o.GetType())
	}

	return analysis, nil
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
