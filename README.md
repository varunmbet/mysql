# mysql

一个轻量的 MySQL SQLBuilder（基于 `database/sql` + `github.com/go-sql-driver/mysql`），提供链式拼 SQL、读写分离、结构体扫描、以及可选 SQL 调试日志。

> 说明：本项目是 **library**（不是可执行程序），你需要在业务项目中 `import` 使用。

### 安装

```bash
go get github.com/varunmbet/mysql
```

### 快速开始

```go
package main

import (
	"fmt"
	"time"

	mysql "github.com/varunmbet/mysql"
)

func main() {
	conf := mysql.SqlConf{
		Debug:    true, // 打开SQL日志（会输出SQL与参数）
		Dbdriver: "mysql",
		Dbtype:   0, // 0=单库；非0=读写分离（Master写、Slave读）
		Master: mysql.SqlOneConf{
			Host:     "127.0.0.1",
			Port:     "3306",
			Username: "root",
			Password: "root",
			Database: "test",

			// 推荐：显式设置连接参数（字符集/时区/超时等）
			// 例如：charset=utf8mb4&parseTime=True&loc=Local&timeout=5s&readTimeout=5s&writeTimeout=5s
			Other: "charset=utf8mb4&parseTime=True&loc=Local",

			MaxIdleConns:    5,
			MaxOpenConns:    20,
			ConnMaxIdleTime: 5 * time.Minute,
			ConnMaxLifetime: 30 * time.Minute,
		},
	}

	db, err := mysql.NewDb(conf)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	sb := db.Orm()
	defer sb.Free() // 用完归还到 pool；只由 Orm() 的调用方调用

	var id int64
	err = sb.Table("users").Where("name=?", "alice").Get(&id)
	fmt.Println("id:", id, "err:", err)
}
```

### 配置说明（重要：Duration单位）

`SqlOneConf` 的这两个字段类型是 `time.Duration`，请用 `time.Second/time.Minute` 等组合：

- **ConnMaxIdleTime**：连接最大空闲时间
- **ConnMaxLifetime**：连接最大生命周期

示例：

```go
conf.Master.ConnMaxIdleTime = 5 * time.Minute
conf.Master.ConnMaxLifetime = 30 * time.Minute
```

### 常用用法

#### 1) 查询单行（Get）：区分“无数据”和“出错”

`Get()` 的行为：

- 执行成功且有数据：返回 `nil`
- 执行成功但没有数据：返回 `sql.ErrNoRows`
- 执行失败：返回具体错误（例如连接失败、SQL语法错误等）

```go
import (
	"database/sql"
	"errors"
)

var name string
err := db.Orm().Table("users").Where("id=?", 1).Get(&name)
if err == nil {
	// 有数据
} else if errors.Is(err, sql.ErrNoRows) {
	// 没有数据（正常分支）
} else {
	// 真正出错
}
```

#### 2) 查询 struct（Find）与列表（FindList/Select）

```go
type User struct {
	ID   int64  `field:"id"`
	Name string `field:"name"`
}

var u User
err := db.Orm().Table("users").Where("id=?", 1).Find(&u)

var list []User
err = db.Orm().Table("users").Where("id>?", 0).OrderBy("id desc").Limit(0, 10).FindList(&list)
```

数据量很大时建议用 `Select()` 自己迭代 `*sql.Rows`（记得 `rows.Close()`）：

```go
rows, err := db.Orm().Table("users").Where("id>?", 0).Select()
if err != nil { /* handle */ }
defer rows.Close()
for rows.Next() {
	// rows.Scan(...)
}
```

> 性能建议：分页场景请配合 `Limit(offset, num)` 使用；库内部会基于 `Limit` 对 `FindList` 的目标 slice 进行预扩容，减少扩容拷贝与 GC 压力。

#### 3) 写入（Insert/IgnoreInsert/ReplaceInsert）

```go
// AddOne 接受 map / struct（或指针）；AddMore 接受 []map / []struct / []*struct
id, err := db.Orm().
	Table("users").
	AddOne(map[string]interface{}{"name": "alice"}).
	Insert(true) // true=返回LastInsertId; false=返回RowsAffected
_ = id
_ = err

// 冲突时更新（只对 Insert 生效）
_, err = db.Orm().Table("users").
	AddOne(map[string]interface{}{"id": 1, "name": "alice"}).
	OnDku("name=VALUES(name)").
	Insert(false)
```

struct 标签：`field:"列名"`，`auto:"1"` 表示自增等数据库自动字段（不写入），`table:"表名"` 在未调用 `Table()` 时生效，
`type:"date|datetime|timestamp|int"` 控制 `time.Time` 的写入格式（不设置则交给驱动处理）。
单行写入/更新默认跳过零值字段；加 `empty:"1"` 的字段零值也会写入（`time.Time` 零值写 NULL）。
`AddMore` 结构体按全部数据库字段逐行写入（零值也写），map 以第一行的键为列，其余行必须有相同的键。

#### 4) 更新/删除（Update/Delete）

```go
rows, err := db.Orm().
	Table("users").
	Sets(map[string]interface{}{"name": "bob"}).
	Where("id=?", 1).
	Update()
_ = rows
_ = err

rows, err = db.Orm().Table("users").Where("id=?", 1).Delete()
_ = rows
_ = err
```

### 事务使用（Begin/Commit/Rollback）

#### 推荐：使用 `db.Transaction(...)`（更安全）

库内置了事务包装器，自动处理：

- `Begin()`
- `panic` / 返回 `error` -> `Rollback()`
- 成功 -> `Commit()`
- 最终 `Free()` 归还 builder（并且如果事务意外未结束，`Free()` 会 best-effort 自动回滚避免泄露）

```go
err := db.Transaction(func(sb *mysql.SQLBuilder) error {
	// sb 已处于事务中
	_, err := sb.Table("users").Sets(map[string]interface{}{"name": "tx"}).Where("id=?", 1).Update()
	if err != nil {
		return err
	}
	// 继续更多写操作...
	return nil
})
```

#### 传统用法：手动 Begin/Commit/Rollback

```go
sb := db.Orm()
defer sb.Free() // 注意：事务场景也建议最终 Free()

_, err := sb.Begin()
if err != nil { /* handle */ }

_, err = sb.Table("users").Sets(map[string]interface{}{"name": "tx"}).Where("id=?", 1).Update()
if err != nil {
	_, _ = sb.Rollback()
	return
}

_, err = sb.Commit()
if err != nil {
	_, _ = sb.Rollback()
	return
}
```

> 注意：
>
> - 推荐用 `db.Transaction(...)`，可以显著减少漏回滚/漏释放导致的连接泄露与锁占用问题
> - 现在 `Free()` 在检测到事务未结束时会 **best-effort 自动回滚**，但仍建议你显式 `Commit/Rollback`

### Debug/SQL日志

`SqlConf.Debug=true` 会输出 SQL 与参数到 stdout（请注意敏感信息）。

### 连接生命周期

推荐在程序退出时调用：

```go
defer db.Close()
```

### builder 的归还规则

- `Free()` 只由取得 builder 的一方调用：`db.Orm()` 的调用方，或 `db.Transaction`（内部已处理）
- `Get/Find/FindList/Select/QueryRow/Query/Insert/Update/Delete` 等执行完只清空构建状态，**不会**自行归还 pool，
  同一个 builder 可以继续构建下一条语句
- 不调用 `Free()` 的写法（如 `db.Orm().Table(...).Find(...)`）依然正确，只是该对象不会被复用，由 GC 回收

### 条件与参数

- `Where / WhereIn / JoinRaw / Having / Set / Sets / OnDku` 的参数按子句分别保存，生成 SQL 时按语句中的位置拼接，链式调用顺序不影响参数对应
- `Set / Sets` 可多次调用、可混用，合并为一个 `SET` 子句；`Having` 多次调用以 `AND` 连接
- `WhereIn("id", ...)` 接受多个值、切片或切片指针；值为空时生成 `1=0`（`NotIn` 为 `1=1`），不会退化成只剩字段名的条件

### 破坏性变更说明（与旧版本相比）

- `NewDb` 已改为 `func NewDb(config SqlConf) (*DbModel, error)`，连接失败不再 `panic`
- `ConnMaxIdleTime/ConnMaxLifetime` 类型改为 `time.Duration`，避免单位误用
- `Get()` 遇到查询执行错误会返回 `err`（不再静默 `nil`）
- 执行方法不再自动 `Free()`（见“builder 的归还规则”）。旧版在调用方也 `defer sb.Free()` 时会把同一对象两次放回 pool，
  第二次可能回滚别的 goroutine 正在进行的事务
- `WhereIn` 传空值：旧版生成 `WHERE id`（`Delete` 时会删掉所有 id 非 0 的行），现在为恒假条件
- `Having` 不再要求先调用 `GroupBy`；`Set` 的 `?` 个数与参数不一致时 panic（与 `Where` 一致）
- struct 的 `empty:"1"` 标签生效（零值会写入）；未设置 `type` 的 `time.Time` 字段现在会正确传参
- map 写入/更新的列按键名排序，SQL 文本稳定
- `text` 包日志不再修改标准库 `log` 的全局输出、不再在日志文件打不开时 `os.Exit`
