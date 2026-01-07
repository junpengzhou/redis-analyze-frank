package main

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
	Mode          string // "bigkey", "prefix", "spec", "both", "flame", "all"
	FlameOutput   string
	FlameDepth    int
	FlameMinValue int64
	FlameFormat   string // "json", "folded", "csv"
	Separator     string
	SpecKey       string // 指定键
}

// KeyAnalysis 大Key分析结果
type KeyAnalysis struct {
	Database int    // 数据库角标
	Type     string // 数据类型
	Key      string // 键
	Size     int64  // 大小
	Elements int    // 数量
	Encoding string // 编码
	Expiry   int64  // 过期
	TTL      int64  // 过期时间
}

// PrefixStat 前缀统计结果
type PrefixStat struct {
	Prefix       string  // 前缀
	Depth        int     // 深度
	Size         int64   // 大小
	Count        int64   // 数量
	Database     int     // 数据库角标
	SizeReadable string  // 人类可读大小
	AvgSize      float64 // 平均大小
}

// SpecStat 指定统计结果
type SpecStat struct {
	Database     int    // 数据库角标
	Type         string // 数据类型
	Key          string // KEY
	Size         int64  // 大小
	SizeReadable string // 人类可读大小
	Ttl          int64  // 过期时间
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

// MemoryStats 获取内存使用情况
type MemoryStats struct {
	AllocMB   float64
	TotalMB   float64
	SysMB     float64
	NumGC     uint32
	HeapAlloc float64
	HeapSys   float64
}
