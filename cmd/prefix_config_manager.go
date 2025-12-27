package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// PrefixConfigManager 前缀配置管理器
type PrefixConfigManager struct {
	// 存储按长度排序的前缀列表，从长到短排序
	sortedPrefixes []string
	// 存储原始前缀集合，用于快速查找
	prefixSet map[string]bool
}

// NewPrefixConfigManager 创建新的前缀配置管理器
func NewPrefixConfigManager(configFile string) (*PrefixConfigManager, error) {
	manager := &PrefixConfigManager{
		prefixSet: make(map[string]bool),
	}

	if err := manager.loadConfig(configFile); err != nil {
		return nil, fmt.Errorf("加载前缀配置失败: %v", err)
	}

	return manager, nil
}

// MatchPrefix 匹配前缀，返回匹配到的前缀和是否匹配成功
func (p *PrefixConfigManager) MatchPrefix(key string) string {
	upperKey := strings.ToUpper(key)

	// 从长到短遍历前缀，找到第一个匹配的
	for _, prefix := range p.sortedPrefixes {
		if strings.HasPrefix(upperKey, prefix) {
			return prefix
		}
	}

	// 没有匹配到同一归类到其他类型中,改成其他类型拼上第一个:往前的值,组成变更Others:abc这样的格式
	parts := strings.Split(upperKey, ":")
	if len(parts) > 0 {
		var prefixParts []string
		// 字典外的，最多只展示前两层即可
		for i := 0; i < 2 && i < len(parts); i++ {
			prefixParts = append(prefixParts, parts[i])
		}
		return "Others:" + strings.Join(prefixParts, ":")
	}
	return "Others"
}

// GetAllPrefixes 获取所有前缀
func (p *PrefixConfigManager) GetAllPrefixes() []string {
	return p.sortedPrefixes
}

// loadConfig 加载配置文件
func (p *PrefixConfigManager) loadConfig(configFile string) error {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %v", err)
	}

	var prefixes []string
	if err := json.Unmarshal(data, &prefixes); err != nil {
		return fmt.Errorf("解析配置文件失败: %v", err)
	}

	// 构建前缀集合
	for _, prefix := range prefixes {
		// 转换为大写用于统一匹配
		p.prefixSet[strings.ToUpper(prefix)] = true
	}

	// 使用临时 map 进行去重，还是java和python好，去重这个要写好多代码...无语子
	uniquePrefixes := make(map[string]bool)
	var deduplicatedPrefixes []string

	// 都转为大写然后比对是否有重复
	for _, prefix := range prefixes {
		upperPrefix := strings.ToUpper(prefix)
		if !uniquePrefixes[upperPrefix] {
			uniquePrefixes[upperPrefix] = true
			deduplicatedPrefixes = append(deduplicatedPrefixes, upperPrefix)
		}
	}

	// 构建前缀集合
	for _, prefix := range deduplicatedPrefixes {
		p.prefixSet[prefix] = true
	}

	// 按长度从长到短排序
	p.sortedPrefixes = make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		p.sortedPrefixes = append(p.sortedPrefixes, strings.ToUpper(prefix))
	}

	sort.Slice(p.sortedPrefixes, func(i, j int) bool {
		return len(p.sortedPrefixes[i]) > len(p.sortedPrefixes[j])
	})

	return nil
}
