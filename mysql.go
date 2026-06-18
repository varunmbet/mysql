package mysql

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"

	Text "github.com/varunmbet/mysql/text"
)

type (
	DbModel struct {
		pool   *sync.Pool
		reader *sql.DB
		writer *sql.DB
	}

	SqlConf struct {
		Debug    bool
		Dbdriver string
		Dbtype   int
		Master   SqlOneConf
		Slave    SqlOneConf
	}
	SqlOneConf struct {
		Host            string
		Port            string
		Username        string
		Password        string
		Database        string
		Other           string
		Tablepre        string
		MaxIdleConns    int
		MaxOpenConns    int
		ConnMaxLifetime time.Duration
		ConnMaxIdleTime time.Duration
	}
)

func NewDb(config SqlConf) (*DbModel, error) {
	db := &DbModel{}
	var err error

	// 设置主数据库
	db.writer, err = Init(config.Dbdriver, config.Master)
	if err != nil {
		Text.Log("debug").Debug(err.Error())
		errstr := fmt.Sprintf("无法连接到主数据库[Ip:%s;port:%s] 错误[%v]", config.Master.Host, config.Master.Port, err.Error())
		return nil, errors.New(errstr)
	}

	// 设置从数据库
	if config.Dbtype == 0 { //默认单机模型
		db.reader = db.writer
	} else {
		db.reader, err = Init(config.Dbdriver, config.Slave)
		if err != nil {
			Text.Log("debug").Debug(err.Error())
			errstr := fmt.Sprintf("无法连接到从数据库[Ip:%s;port:%s] 错误[%v]", config.Slave.Host, config.Slave.Port, err.Error())
			_ = db.writer.Close()
			return nil, errors.New(errstr)
		}
	}

	db.pool = &sync.Pool{
		New: func() interface{} {
			return NewSQLBuilder(db, config.Debug)
		},
	}
	return db, nil
}

// 初始化数据库
func Init(driver string, conf SqlOneConf) (db *sql.DB, err error) {
	defer func() { // 捕捉panic并转成error，避免静默失败
		if r := recover(); r != nil {
			err = fmt.Errorf("db init panic (driver=%s): %v", driver, r)
		}
	}()
	linkstr := Text.SpliceString(conf.Username, ":", conf.Password,
		"@tcp(", conf.Host, ":", conf.Port, ")/", conf.Database,
		"?", conf.Other)
	db, err = sql.Open(driver, linkstr)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(conf.MaxOpenConns) //设置最大打开的连接数，默认值为0表示不限制,可以避免并发太高导致连接mysql出现too many connections的错误
	db.SetMaxIdleConns(conf.MaxIdleConns) //设置闲置的连接数,当开启的一个连接使用完成后可以放在池里等候下一次使用
	db.SetConnMaxIdleTime(conf.ConnMaxIdleTime)
	db.SetConnMaxLifetime(conf.ConnMaxLifetime)
	err = db.Ping()
	return db, err
}

// 数据库连接判断，连接未中断返回nil
func (m *DbModel) Ping() error {
	if m == nil || m.reader == nil || m.writer == nil {
		return errors.New("db model not initialized")
	}
	err := m.reader.Ping()
	if err == nil {
		err = m.writer.Ping()
	}
	return err
}

// Close closes underlying db connections.
func (m *DbModel) Close() error {
	if m == nil {
		return nil
	}
	var err1, err2 error
	if m.reader != nil && m.reader != m.writer {
		err1 = m.reader.Close()
	}
	if m.writer != nil {
		err2 = m.writer.Close()
	}
	if err1 != nil {
		return err1
	}
	return err2
}

// GetDb 返回底层写库 *sql.DB，用于 DDL（建表/迁移）等高级用法。
func (m *DbModel) GetDb() *sql.DB {
	if m == nil {
		return nil
	}
	return m.writer
}

func (m *DbModel) Orm() *SQLBuilder {
	sb := m.pool.Get().(*SQLBuilder)
	sb.inPool = false // 取出后标记“在用”，配合 Free 的幂等保护避免重复入池
	return sb
}

func (m *DbModel) PutSb(sb *SQLBuilder) {
	//sb.ReSet()
	m.pool.Put(sb)
	//sbpool.Put(sb)
}
