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
	Mode          string // "bigkey", "prefix", "both", "flame", "all"
	FlameOutput   string
	FlameDepth    int
	FlameMinValue int64
	FlameFormat   string // "json", "folded", "csv"
	Separator     string
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

// MemoryStats 获取内存使用情况
type MemoryStats struct {
	AllocMB   float64
	TotalMB   float64
	SysMB     float64
	NumGC     uint32
	HeapAlloc float64
	HeapSys   float64
}
