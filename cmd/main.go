package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

func main() {
	// 命令行参数
	inputFile := flag.String("input", "", "输入 RDB 文件路径")
	outputFile := flag.String("output", "", "输出 CSV 文件路径 (大Key分析)")
	prefixOutputFile := flag.String("prefix-output", "", "输出 CSV 文件路径 (前缀分析)")
	flameOutputFile := flag.String("flame-output", "", "输出文件路径 (火焰图分析)")
	mode := flag.String("mode", "all", "分析模式: bigkey, prefix, flame, both, all, spec")
	thresholdKB := flag.Int("threshold", 3, "大 Key 阈值(单位：KB), 默认:3")
	prefixDepth := flag.Int("prefix-depth", 3, "前缀分析的最大深度, 默认:3")
	topN := flag.Int("topn", 0, "前缀分析的TopN数量, 默认:100")
	specKey := flag.String("spec-prefix", "", "前缀匹配直接显示模式，指定的前缀")
	flameDepth := flag.Int("flame-depth", 5, "火焰图分析的最大深度, 默认:5")
	flameMinValue := flag.Int64("flame-min", 1024, "火焰图分析的最小值(字节), 默认:1024(1KB)")
	flameFormat := flag.String("flame-format", "folded", "火焰图输出格式: json, folded, csv, 默认:folded")
	prefixConfigFile := flag.String("prefix-config", "", "前缀配置文件路径 (JSON格式)")
	separator := flag.String("separator", ":", "键分隔符, 默认:':'")
	skipErrors := flag.Bool("skip-errors", true, "遇到错误时跳过而不是停止")

	flag.Parse()

	if *inputFile == "" {
		fmt.Println("请输入 RDB 文件路径")
		fmt.Println("用法: rdb-analyzer -input <rdb文件> [-mode <bigkey|prefix|flame|spec|both|all>]")
		fmt.Println("\n(bigkey)大Key分析参数:")
		fmt.Println("  -output <文件>        输出文件路径")
		fmt.Println("  -threshold <KB>      大Key阈值(默认:3KB)")
		fmt.Println("\n(prefix)前缀分析参数:")
		fmt.Println("  -prefix-output <文件> 前缀输出文件路径")
		fmt.Println("  -prefix-depth <深度>  前缀分析最大深度(默认:3)")
		fmt.Println("  -prefix-config <文件> 前缀配置文件路径 (JSON格式)")
		fmt.Println("  -topn <数量>          输出前N个前缀(默认:100)")
		fmt.Println("\n(spec)指定分析参数:")
		fmt.Println("  -prefix-output <文件> 前缀输出文件路径")
		fmt.Println("  -prefix-config <文件> 前缀配置文件路径 (JSON格式)")
		fmt.Println("  -spec-prefix  <前缀>  前缀")
		fmt.Println("\n(flame)火焰图分析参数:")
		fmt.Println("  -flame-output <文件>  火焰图输出文件路径")
		fmt.Println("  -flame-depth <深度>   火焰图最大深度(默认:5)")
		fmt.Println("  -flame-min <字节>     火焰图最小值(默认:1024)")
		fmt.Println("  -flame-format <格式>  输出格式: json, folded, csv (默认:folded)")
		fmt.Println("  -prefix-config <文件> 前缀配置文件路径 (JSON格式)")
		fmt.Println("\n(common)通用参数:")
		fmt.Println("  -separator <字符>     键分隔符(默认:':')")
		fmt.Println("  -skip-errors         跳过错误继续处理(默认:true)")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// 验证文件存在
	if _, err := os.Stat(*inputFile); os.IsNotExist(err) {
		log.Fatalf("RDB 文件不存在: %s", *inputFile)
	}

	// 验证模式参数
	validModes := map[string]bool{
		"bigkey": true,
		"prefix": true,
		"spec":   true,
		"flame":  true,
		"both":   true,
		"all":    true,
	}
	if !validModes[*mode] {
		log.Fatalf("无效的模式: %s，必须是 bigkey, prefix, flame, both 或 all", *mode)
	}

	// 验证火焰图格式
	validFlameFormats := map[string]bool{
		"json": true, "folded": true, "csv": true,
	}
	if !validFlameFormats[*flameFormat] {
		log.Fatalf("无效的火焰图格式: %s，必须是 json, folded 或 csv", *flameFormat)
	}

	// 设置输出文件默认值
	if *mode == "bigkey" || *mode == "both" || *mode == "all" {
		if *outputFile == "" {
			*outputFile = strings.TrimSuffix(*inputFile, ".rdb") + "_bigkeys.csv"
		}
	}

	if *mode == "prefix" || *mode == "both" || *mode == "spec" || *mode == "all" {
		if *prefixOutputFile == "" {
			*prefixOutputFile = strings.TrimSuffix(*inputFile, ".rdb") + "_prefix.csv"
		}
	}

	if *mode == "flame" || *mode == "all" {
		if *flameOutputFile == "" {
			*flameOutputFile = strings.TrimSuffix(*inputFile, ".rdb") + "_flame." + *flameFormat
		}
	}

	// 配置参数
	config := Config{
		InputFile:     *inputFile,
		OutputFile:    *outputFile,
		ThresholdKB:   *thresholdKB,
		PrefixDepth:   *prefixDepth,
		SpecKey:       *specKey,
		TopN:          *topN,
		ShowProgress:  true,
		SkipErrors:    *skipErrors,
		Mode:          *mode,
		FlameOutput:   *flameOutputFile,
		FlameDepth:    *flameDepth,
		FlameMinValue: *flameMinValue,
		FlameFormat:   *flameFormat,
		Separator:     *separator,
	}

	// 创建分析器
	analyzer := NewStreamAnalyzer(config)

	// 如果指定了前缀配置文件，则加载配置
	if *prefixConfigFile != "" {
		prefixManager, err := NewPrefixConfigManager(*prefixConfigFile)
		if err != nil {
			log.Fatalf("加载前缀配置失败: %v", err)
		}
		analyzer.PrefixConfigManager = prefixManager
		fmt.Printf("已加载前缀配置文件: %s，包含 %d 个前缀\n", *prefixConfigFile, len(prefixManager.GetAllPrefixes()))
	}

	// 记录开始时间
	startTime := time.Now()

	// 执行分析器
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

	if config.Mode == "prefix" || config.Mode == "spec" || config.Mode == "both" || config.Mode == "all" {
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
