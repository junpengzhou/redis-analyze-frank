# 使用预定义前缀配置文件

go run main.go -input data.rdb -mode all -prefix-config configs/prefix.json

# 使用原有分隔符方式

go run main.go -input data.rdb -mode all

# 使用设定内的前缀进行分析的方式

```shell
./build/frank -input /data/dump/dump.rdb -mode prefix -topn 0 -prefix-config ./configs/prefix.json -prefix-depth 0
```
