package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/hdt3213/rdb/parser"
)

// Config 配置参数
type Config struct {
	InputFile    string
	OutputFile   string
	ThresholdKB  int
	MaxKeys      int
	KeyPattern   string
	TypeFilter   string
	ShowProgress bool
	SkipErrors   bool
}

// KeyAnalysis 分析结果
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

// StreamAnalyzer 流式分析器
type StreamAnalyzer struct {
	Config
	totalKeys   int
	bigKeys     []KeyAnalysis
	startTime   time.Time
	bytesRead   int64
	fileSize    int64
	errorCount  int
	skippedKeys int
}

func NewStreamAnalyzer(config Config) *StreamAnalyzer {
	return &StreamAnalyzer{
		Config:    config,
		bigKeys:   make([]KeyAnalysis, 0, 1000),
		startTime: time.Now(),
	}
}

func (s *StreamAnalyzer) Analyze() error {
	// 打开 RDB 文件
	file, err := os.Open(s.InputFile)
	if err != nil {
		return fmt.Errorf("无法打开RDB文件: %v", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Printf("关闭文件时发生错误: %v", closeErr)
		}
	}()

	// 获取文件大小
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("无法获取文件信息: %v", err)
	}
	s.fileSize = fileInfo.Size()

	if s.ShowProgress {
		fmt.Printf("开始分析RDB文件: %s (大小: %.2f GB)\n",
			s.InputFile, float64(s.fileSize)/(1024*1024*1024))
		fmt.Printf("阈值: %dKB\n", s.ThresholdKB)
		fmt.Printf("跳过错误: %v\n", s.SkipErrors)
	}

	// 运行内存监控
	if s.ShowProgress {
		go s.monitorMemory()
	}

	// 创建解析器
	decoder := parser.NewDecoder(file)

	// 使用安全的解析方法
	err = decoder.Parse(func(o parser.RedisObject) bool {
		// 使用 defer 恢复 panic
		defer func() {
			if r := recover(); r != nil {
				s.errorCount++
				if s.ShowProgress && s.errorCount <= 10 {
					// 只显示前10个错误
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
			memStats := getMemoryUsage()

			fmt.Printf("\r进度: 已处理: %d | 大Key: %d | 速度: %.0f keys/sec | 内存: %.1fMB | 错误: %d | 跳过: %d",
				s.totalKeys, len(s.bigKeys), rate, memStats.AllocMB, s.errorCount, s.skippedKeys)
		}

		// 解析 key
		analysis, err := s.parseObjectSafe(o)
		if err != nil {
			s.skippedKeys++
			if s.ShowProgress && s.skippedKeys <= 5 {
				fmt.Printf("\n[跳过] 无法解析Key: %v\n", err)
			}
			// 继续处理下一个
			return true
		}

		if analysis.Size > int64(s.ThresholdKB*1024) {
			s.bigKeys = append(s.bigKeys, analysis)

			// 如果达到最大数量限制，停止处理
			if s.MaxKeys > 0 && len(s.bigKeys) >= s.MaxKeys {
				return false
			}
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
				// 8字节给分数
				analysis.Size += int64(len(entry.Member) + 8)
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

// MemoryStats 获取内存使用情况
type MemoryStats struct {
	AllocMB   float64
	TotalMB   float64
	SysMB     float64
	NumGC     uint32
	HeapAlloc float64
	HeapSys   float64
}

func getMemoryUsage() MemoryStats {
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
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		memStats := getMemoryUsage()
		fmt.Printf("\n[内存监控] Alloc=%.1fMB, Total=%.1fMB, Sys=%.1fMB, HeapAlloc=%.1fMB, GC次数=%d\n",
			memStats.AllocMB, memStats.TotalMB, memStats.SysMB, memStats.HeapAlloc, memStats.NumGC)
	}
}

func (s *StreamAnalyzer) SaveResults() error {
	// 按大小排序
	s.sortBySize()

	// 保存到 CSV 文件
	file, err := os.Create(s.OutputFile)
	if err != nil {
		return fmt.Errorf("无法创建输出文件: %v", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Printf("关闭文件时发生错误: %v", closeErr)
		}
	}()

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

func (s *StreamAnalyzer) sortBySize() {
	// 使用快速排序优化性能
	if len(s.bigKeys) < 2 {
		return
	}

	// 使用标准库排序
	quickSort(s.bigKeys, 0, len(s.bigKeys)-1)
}

func quickSort(arr []KeyAnalysis, low, high int) {
	if low < high {
		pi := partition(arr, low, high)
		quickSort(arr, low, pi-1)
		quickSort(arr, pi+1, high)
	}
}

func partition(arr []KeyAnalysis, low, high int) int {
	pivot := arr[high].Size
	i := low - 1

	for j := low; j < high; j++ {
		if arr[j].Size > pivot { // 降序排序
			i++
			arr[i], arr[j] = arr[j], arr[i]
		}
	}
	arr[i+1], arr[high] = arr[high], arr[i+1]
	return i + 1
}

func (s *StreamAnalyzer) PrintSummary() {
	elapsed := time.Since(s.startTime)

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("分析完成!")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("输入文件: %s\n", s.InputFile)
	fmt.Printf("输出文件: %s\n", s.OutputFile)
	fmt.Printf("处理时间: %v\n", elapsed)
	fmt.Printf("总Key数: %d\n", s.totalKeys)
	fmt.Printf("大Key数(>%dKB): %d\n", s.ThresholdKB, len(s.bigKeys))
	fmt.Printf("错误数: %d\n", s.errorCount)
	fmt.Printf("跳过Key数: %d\n", s.skippedKeys)

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
	fmt.Println(strings.Repeat("=", 60))
}

func main() {
	// 命令行参数
	inputFile := flag.String("input", "", "输入 RDB 文件路径")
	outputFile := flag.String("output", "", "输出 CSV 文件路径")
	thresholdKB := flag.Int("threshold", 3, "大 Key 阈值(单位：KB), 默认:3")
	maxKeys := flag.Int("max", 10000, "最多记录的大 Key 数量, 默认:10000")
	skipErrors := flag.Bool("skip-errors", true, "遇到错误时跳过而不是停止")

	flag.Parse()

	if *inputFile == "" {
		fmt.Println("请输入 RDB 文件路径")
		fmt.Println("用法: rdb-analyzer -input <rdb文件> [-output <输出文件>] [-threshold <KB>] [-max <数量>] [-skip-errors]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// 验证文件存在
	if _, err := os.Stat(*inputFile); os.IsNotExist(err) {
		log.Fatalf("RDB 文件不存在: %s", *inputFile)
	}

	if *outputFile == "" {
		*outputFile = strings.TrimSuffix(*inputFile, ".rdb") + "_analysis.csv"
	}

	if *thresholdKB < 0 {
		log.Fatalf("阈值不能小于0")
	}

	// 配置参数
	config := Config{
		InputFile:    *inputFile,   // 输入 RDB 文件路径
		OutputFile:   *outputFile,  // 输出文件路径
		ThresholdKB:  *thresholdKB, // 阈值
		MaxKeys:      *maxKeys,     // 最多记录的大 Key 数量
		ShowProgress: true,         // 显示进度
		SkipErrors:   *skipErrors,  // 跳过错误
	}

	// 创建分析器
	analyzer := NewStreamAnalyzer(config)

	// 开始分析
	fmt.Println("开始流式分析RDB文件...")
	fmt.Println("如果遇到错误，程序会尝试跳过并继续处理...")

	// 记录开始时间
	startTime := time.Now()

	if err := analyzer.Analyze(); err != nil {
		log.Fatalf("分析失败: %v", err)
	}

	// 保存结果
	if err := analyzer.SaveResults(); err != nil {
		log.Fatalf("保存结果失败: %v", err)
	}

	// 统计耗时情况
	elapsed := time.Since(startTime)
	log.Printf("分析完成! 耗时: %v", elapsed)

	// 打印摘要
	analyzer.PrintSummary()
}
