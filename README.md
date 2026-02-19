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
	defer sb.Free() // 强烈建议：用完归还到 pool

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

#### 3) 写入（Insert/IgnoreInsert/ReplaceInsert）

```go
// 先调用 Sets / SetMap / SetStruct 等组装插入内容（具体方法见 builder.go）
id, err := db.Orm().
	Table("users").
	Sets(map[string]interface{}{"name": "alice"}).
	Insert(true) // true=返回LastInsertId; false=返回RowsAffected
_ = id
_ = err
```

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

### 破坏性变更说明（与旧版本相比）

- `NewDb` 已改为 `func NewDb(config SqlConf) (*DbModel, error)`，连接失败不再 `panic`
- `ConnMaxIdleTime/ConnMaxLifetime` 类型改为 `time.Duration`，避免单位误用
- `Get()` 遇到查询执行错误会返回 `err`（不再静默 `nil`）
