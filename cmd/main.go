package main

import (
	"encoding/csv"
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
	totalKeys int
	bigKeys   []KeyAnalysis
	startTime time.Time
	bytesRead int64
	fileSize  int64
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
	}

	// 运行内存监控
	if s.ShowProgress {
		s.monitorMemory()
	}

	// 创建解析器
	decoder := parser.NewDecoder(file)

	// 解析回调函数
	err = decoder.Parse(func(o parser.RedisObject) bool {
		s.totalKeys++

		// 每处理 10000个 key 显示进度
		if s.ShowProgress && s.totalKeys%10000 == 0 {
			progress := float64(s.bytesRead) / float64(s.fileSize) * 100
			elapsed := time.Since(s.startTime)
			rate := float64(s.totalKeys) / elapsed.Seconds()

			fmt.Printf("\r进度: %.1f%% | 已处理: %d | 大Key: %d | 速度: %.0f keys/sec",
				progress, s.totalKeys, len(s.bigKeys), rate)
		}

		// 解析 key
		analysis := s.parseObject(o)
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
		return fmt.Errorf("解析RDB文件失败: %v", err)
	}

	if s.ShowProgress {
		fmt.Println() // 换行
	}

	return nil
}

func (s *StreamAnalyzer) parseObject(o parser.RedisObject) KeyAnalysis {
	analysis := KeyAnalysis{
		Database: o.GetDBIndex(),
		Type:     o.GetType(),
		Key:      o.GetKey(),
		Expiry:   o.GetExpiration().Unix(),
	}

	// 计算过期时间
	if analysis.Expiry > 0 {
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
		}

	case "list":
		if list, ok := o.(*parser.ListObject); ok {
			analysis.Size = int64(len(list.Key))
			for _, item := range list.Values {
				analysis.Size += int64(len(item))
			}
			analysis.Elements = len(list.Values)
			analysis.Encoding = list.Encoding
		}

	case "hash":
		if hash, ok := o.(*parser.HashObject); ok {
			analysis.Size = int64(len(hash.Key))
			for field, value := range hash.Hash {
				analysis.Size += int64(len(field) + len(value))
			}
			analysis.Elements = len(hash.Hash)
			analysis.Encoding = hash.Encoding
		}

	case "set":
		if set, ok := o.(*parser.SetObject); ok {
			analysis.Size = int64(len(set.Key))
			for _, member := range set.Members {
				analysis.Size += int64(len(member))
			}
			analysis.Elements = len(set.Members)
			analysis.Encoding = set.Encoding
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
		}
	}

	return analysis
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
	// 简单的冒泡排序（对于少量数据足够）
	for i := 0; i < len(s.bigKeys); i++ {
		for j := i + 1; j < len(s.bigKeys); j++ {
			if s.bigKeys[i].Size < s.bigKeys[j].Size {
				s.bigKeys[i], s.bigKeys[j] = s.bigKeys[j], s.bigKeys[i]
			}
		}
	}
}

// 添加内存监控函数
func (s *StreamAnalyzer) monitorMemory() {
	go func() {
		for {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			fmt.Printf("\r内存使用: Alloc=%vMB, TotalAlloc=%vMB, Sys=%vMB, NumGC=%v",
				m.Alloc/1024/1024,
				m.TotalAlloc/1024/1024,
				m.Sys/1024/1024,
				m.NumGC)

			time.Sleep(5 * time.Second)
		}
	}()
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
	// 配置参数
	config := Config{
		InputFile:    "/data/dump.rdb",    // 输入 RDB 文件路径
		OutputFile:   "/data/bigkeys.csv", // 输出文件路径
		ThresholdKB:  3,                   // 3KB 阈值
		MaxKeys:      1000000,             // 最多记录的大 Key 数量, 我打算先找出top 100w 的
		ShowProgress: true,                // 显示进度
	}

	// 创建分析器
	analyzer := NewStreamAnalyzer(config)

	// 开始分析
	fmt.Println("开始流式分析RDB文件...")
	if err := analyzer.Analyze(); err != nil {
		log.Fatalf("分析失败: %v", err)
	}

	// 保存结果
	if err := analyzer.SaveResults(); err != nil {
		log.Fatalf("保存结果失败: %v", err)
	}

	// 打印摘要
	analyzer.PrintSummary()
}
