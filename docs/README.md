# 一、用法 Help

```plaintext
请输入 RDB 文件路径
用法: rdb-analyzer -input <rdb文件> [-mode <bigkey|prefix|flame|both|all>]

大Key分析参数:
  -output <文件>        输出文件路径
  -threshold <KB>      大Key阈值(默认:3KB)

前缀分析参数:
  -prefix-output <文件> 前缀输出文件路径
  -prefix-depth <深度>  前缀分析最大深度(默认:3)
  -prefix-config <文件> 前缀配置文件路径 (JSON格式)
  -topn <数量>          输出前N个前缀(默认:100)

火焰图分析参数:
  -flame-output <文件>  火焰图输出文件路径
  -flame-depth <深度>   火焰图最大深度(默认:5)
  -flame-min <字节>     火焰图最小值(默认:1024)
  -flame-format <格式>  输出格式: json, folded, csv (默认:folded)
  -prefix-config <文件> 前缀配置文件路径 (JSON格式)
  -group-by-type       按数据类型分组火焰图

通用参数:
  -separator <字符>     键分隔符(默认:':')
  -skip-errors         跳过错误继续处理(默认:true)
  -flame-depth int
        火焰图分析的最大深度, 默认:5 (default 5)
  -flame-format string
        火焰图输出格式: json, folded, csv, 默认:folded (default "folded")
  -flame-min int
        火焰图分析的最小值(字节), 默认:1024(1KB) (default 1024)
  -flame-output string
        输出文件路径 (火焰图分析)
  -group-by-type
        按数据类型分组火焰图
  -input string
        输入 RDB 文件路径
  -mode string
        分析模式: bigkey, prefix, flame, both, all (default "all")
  -output string
        输出 CSV 文件路径 (大Key分析)
  -prefix-config string
        前缀配置文件路径 (JSON格式)
  -prefix-depth int
        前缀分析的最大深度, 默认:3 (default 3)
  -prefix-output string
        输出 CSV 文件路径 (前缀分析)
  -separator string
        键分隔符, 默认:':' (default ":")
  -skip-errors
        遇到错误时跳过而不是停止 (default true)
  -threshold int
        大 Key 阈值(单位：KB), 默认:3 (default 3)
  -topn int
        前缀分析的TopN数量, 默认:100 (default 100)
```

# 二、使用设定内的前缀进行前缀分析的方式

```shell
./build/frank -input /data/dump/dump.rdb -mode prefix -topn 0 -prefix-config ./configs/prefix.json -prefix-depth 0
```

# 三、使用设定内的前缀进行火焰图分析的方式

```shell
./build/frank -input /data/dump/dump.rdb -mode flame -topn 0 \
  -prefix-config ./configs/prefix.json \
  -flame-output /data/dump/flame_prefix_20251226_v1.folded \
  -prefix-depth 0 \
  -flame-depth 0  \
  -flame-min  1024 \
  -flame-format folded
```

# 四、使用 Unlinker 进行解除挂载

```shell
./unlinker -password 'Wejoinfx!@#135246' -pattern 'rebate:customerLogin*'
```

