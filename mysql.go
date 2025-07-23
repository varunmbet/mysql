package mysql

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"

	Text "github.com/varunmbet/mysql/text"
)

var dbReader *sql.DB
var dbWriter *sql.DB

type (
	DbModel struct {
		pool *sync.Pool
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
		ConnMaxLifetime int
		ConnMaxIdleTime int
	}
)

func NewDb(config SqlConf) *DbModel {
	db := &DbModel{}
	var err error
	//设置主数据库
	dbWriter, err = Init(config.Dbdriver, config.Master)
	if err != nil {
		Text.Log("debug").Debug(err.Error())
		errstr := fmt.Sprintf("无法连接到主数据库[Ip:%s;port:%s] 错误[%v]\n", config.Master.Host, config.Master.Port, err.Error())
		log.Panic(errstr)
	}
	//设置从数据库
	if config.Dbtype == 0 { //默认单机模型
		dbReader = dbWriter
	} else {
		dbReader, err = Init(config.Dbdriver, config.Slave)
		if err != nil {
			Text.Log("debug").Debug(err.Error())
			errstr := fmt.Sprintf("无法连接到从数据库[Ip:%s;port:%s] 错误[%v]\n", config.Slave.Host, config.Slave.Port, err.Error())
			log.Panic(errstr)
		}
	}
	db.pool = &sync.Pool{
		New: func() interface{} {
			//db.count++
			//fmt.Printf("create a new dbbuile pool,count=%d\n", db.count)
			return NewSQLBuilder(db, config.Debug)
		},
	}
	return db
}

// 初始化数据库
func Init(driver string, conf SqlOneConf) (db *sql.DB, err error) {
	defer func() { //捕捉panic错误避免崩溃
		if r := recover(); r != nil {
			fmt.Printf("[ERROR]发生错误：数据库[%s]服务链接失败！请检查数据库！\r\n", driver)
			//Text.Log("db_error").Error(fmt.Sprintf("db init ping error:%s", r))
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
	db.SetConnMaxIdleTime(time.Duration(conf.ConnMaxIdleTime))
	db.SetConnMaxLifetime(time.Duration(conf.ConnMaxLifetime))
	err = db.Ping()
	return db, err
}

// 数据库连接判断，连接未中断返回nil
func (m *DbModel) Ping() error {
	err := dbReader.Ping()
	if err == nil {
		err = dbWriter.Ping()
	}
	return err
}

func (m *DbModel) Orm() *SQLBuilder {
	//defer m.pool.Put(sb)
	return m.pool.Get().(*SQLBuilder) //sbpool.Get().(*SQLBuilder) //NewSQLBuilder(m) //m.pool.Get().(*SQLBuilder)
	//defer m.pool.Put(sb)
	//return sb
	//m.sb.ReSet()
	//return m.sb
}

func (m *DbModel) PutSb(sb *SQLBuilder) {
	//sb.ReSet()
	m.pool.Put(sb)
	//sbpool.Put(sb)
}
