package mysql

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/varunmbet/mysql/reflectx"
)

// var cacheDbObj *sync.Pool

const (
	MethodInsert        = 1 //"INSERT"
	MethodIgnoreInsert  = 2 //"INSERT IGNORE INTO"
	MethodReplaceInsert = 3 // "REPLACE INSERT"
	MethodUpdate        = 4 //"UPDATE"
	MethodDelete        = 5 //"DELETE"
	MethodSelect        = 6 //"SELECT"
)

var (
	// ErrTableEmpty table not set
	ErrTableEmpty = errors.New("table empty")
	// ErrInsertEmpty insert content not set
	ErrInsertEmpty  = errors.New("insert content empty")
	ErrInsertMethod = errors.New("insert Method err")
	// ErrUpdateEmpty update content not set
	ErrUpdateEmpty = errors.New("update content empty")
)

// SQLBuilder sql builder
type SQLBuilder struct {
	_select      string
	_insert      string
	_update      string
	_delete      string
	_table       string
	_join        string
	_where       string
	_groupBy     string
	_having      string
	_orderBy     string
	_limit       string
	_tbPre       string
	_forUpdate   bool
	_dkuStr      string
	_methodtype  int
	_param       []interface{}
	dbmodel      *DbModel
	unsafe       bool
	Mapper       *reflectx.Mapper
	driverName   string
	tx           *sql.Tx
	isAutoCommit bool //false=事物 true=正常提交

	_SQL   string //日志使用
	_Err   error  //日志使用
	_Debug bool   //是否调试
	Log    *sqllog
}

// NewSQLBuilder init sql builder
func NewSQLBuilder(dbmodel *DbModel, debug bool) *SQLBuilder {
	sb := &SQLBuilder{dbmodel: dbmodel, _Debug: debug, driverName: "mysql", unsafe: true, Mapper: mapper(), isAutoCommit: true}
	sb.Log = NewSqlLog(sb)
	return sb

}
func (sb *SQLBuilder) Init(dbmodel *DbModel) {
	sb.ReSet()
	sb.dbmodel = dbmodel
	sb.driverName = "mysql"
	sb.unsafe = true
	sb.Mapper = mapper()
}

// 是否 sync.pool,所以sb.tx = nil
func (sb *SQLBuilder) Free() {
	// 事务未正常结束时，自动回滚，避免连接/锁泄露
	// 注意：这里是 best-effort；调用方仍应显式 Commit/Rollback
	if !sb.isAutoCommit && sb.tx != nil {
		_ = sb.tx.Rollback()
		sb.isAutoCommit = true
	}
	sb.ReSet()
	sb.tx = nil
	if sb.dbmodel != nil {
		sb.dbmodel.PutSb(sb)
	}
}

// GetQuerySQL get sql
func (sb *SQLBuilder) GetQuerySQL() (string, error) {
	if sb._table == "" {
		return "", ErrTableEmpty
	}
	var buf strings.Builder

	buf.WriteString("SELECT ")
	if sb._select != "" {
		buf.WriteString(sb._select)
	} else {
		buf.WriteString("*")
	}
	buf.WriteString(" FROM ")
	buf.WriteString(sb._table)
	if sb._join != "" {
		buf.WriteString(" ")
		buf.WriteString(sb._join)
	}
	if sb._where != "" {
		buf.WriteString(" ")
		buf.WriteString(sb._where)
	}
	if sb._groupBy != "" {
		buf.WriteString(" ")
		buf.WriteString(sb._groupBy)
	}
	if sb._having != "" {
		buf.WriteString(" ")
		buf.WriteString(sb._having)
	}
	if sb._orderBy != "" {
		buf.WriteString(" ")
		buf.WriteString(sb._orderBy)
	}
	if sb._limit != "" {
		//buf.WriteString(" ")
		buf.WriteString(sb._limit)
	}

	if sb._forUpdate == true {
		buf.WriteString(" FOR UPDATE")
	}

	return buf.String(), nil
}

// GetInsertSQL get sql
func (sb *SQLBuilder) GetInsertSQL() (string, error) {
	if sb._table == "" {
		return "", ErrTableEmpty
	}
	if sb._insert == "" {
		return "", ErrInsertEmpty
	}

	var buf strings.Builder
	if sb._methodtype == 1 {
		buf.WriteString("INSERT INTO ")
	} else if sb._methodtype == 2 {
		buf.WriteString("INSERT IGNORE INTO ")
	} else if sb._methodtype == 3 {
		buf.WriteString("REPLACE INTO ")
	} else {
		return "", ErrInsertMethod
	}
	buf.WriteString(sb._table)
	buf.WriteString(" ")
	buf.WriteString(sb._insert)
	buf.WriteString(" ")

	if sb._methodtype == 1 {
		buf.WriteString(sb._dkuStr)
	}

	return buf.String(), nil
}

// GetUpdateSQL get sql
func (sb *SQLBuilder) GetUpdateSQL() (string, error) {
	if sb._table == "" {
		return "", ErrTableEmpty
	}

	if sb._update == "" {
		return "", ErrUpdateEmpty
	}

	var buf strings.Builder

	buf.WriteString("UPDATE ")
	buf.WriteString(sb._table)
	buf.WriteString(" ")
	buf.WriteString(sb._update)
	if sb._where != "" {
		buf.WriteString(" ")
		buf.WriteString(sb._where)
	}

	return buf.String(), nil
}

// GetDeleteSQL get sql
func (sb *SQLBuilder) GetDeleteSQL() (string, error) {
	if sb._table == "" {
		return "", ErrTableEmpty
	}

	var buf strings.Builder

	buf.WriteString("DELETE FROM ")
	buf.WriteString(sb._table)
	if sb._where != "" {
		buf.WriteString(" ")
		buf.WriteString(sb._where)
	}

	return buf.String(), nil
}

// GetQueryParams get params
func (sb *SQLBuilder) GetQueryParams() []interface{} {
	return sb._param
}

// Table set table
func (sb *SQLBuilder) Table(table string) *SQLBuilder {
	if sb.isAutoCommit {
		sb.Log.Start()
	}
	sb._table = table

	return sb
}

// Select set select cols
func (sb *SQLBuilder) Field(cols string) *SQLBuilder {
	sb._select = cols
	return sb
}

// 查询条件 Where("Uid = ? and user_name=?", 1, "admin")
func (sb *SQLBuilder) Where(queryString string, args ...interface{}) *SQLBuilder {
	if strings.Count(queryString, "?") != len(args) {
		err := fmt.Sprintf("查询条件[%s]与参数个数不对应", queryString)
		panic(err)
	}
	var buf strings.Builder
	buf.WriteString(sb._where) // append
	if buf.Len() == 0 {
		buf.WriteString("WHERE ")
		buf.WriteString(queryString)
	} else {
		buf.WriteString(" AND ")
		buf.WriteString(queryString)
	}
	sb._where = buf.String()
	if len(args) > 0 {
		sb._param = append(sb._param, args...)
	}
	return sb
}

// WhereIn set where in cond
func (sb *SQLBuilder) WhereIn(field string, values ...interface{}) *SQLBuilder {
	return sb.whereIn("AND", "IN", field, values...)
}

// OrWhereIn set or where in cond
func (sb *SQLBuilder) OrWhereIn(field string, values ...interface{}) *SQLBuilder {
	return sb.whereIn("OR", "IN", field, values...)
}

// WhereNotIn set where not in cond
func (sb *SQLBuilder) WhereNotIn(field string, values ...interface{}) *SQLBuilder {
	return sb.whereIn("AND", "NOT IN", field, values...)
}

// OrWhereNotIn set or where not in cond
func (sb *SQLBuilder) OrWhereNotIn(field string, values ...interface{}) *SQLBuilder {
	return sb.whereIn("OR", "NOT IN", field, values...)
}

func (sb *SQLBuilder) whereIn(operator string, condition string, field string, values ...interface{}) *SQLBuilder {
	if len(values) > 0 {
		value := reflect.ValueOf(values[0])
		if value.Kind() == reflect.Ptr {
			elemType := value.Type().Elem()
			if elemType.Kind() == reflect.Slice {
				PtrValue := reflect.Indirect(value)
				sb.parseWhereInBySlice(operator, condition, field, PtrValue)
			}
		} else if value.Kind() == reflect.Slice { //map
			sb.parseWhereInBySlice(operator, condition, field, value)
		} else { //多个参数
			var buf strings.Builder
			buf.WriteString(sb._where) // append
			if buf.Len() == 0 {
				buf.WriteString("WHERE ")
			} else {
				buf.WriteString(" ")
				buf.WriteString(operator)
				buf.WriteString(" ")
			}
			buf.WriteString(field)
			if len(values) > 0 {
				plhs := GenPlaceholders(len(values))
				buf.WriteString(" ")
				buf.WriteString(condition)
				buf.WriteString(" ")
				buf.WriteString("(")
				buf.WriteString(plhs)
				buf.WriteString(")")
				sb._param = append(sb._param, values...)
			}
			sb._where = buf.String()
		}
		return sb
	} else {
		var buf strings.Builder
		buf.WriteString(sb._where) // append
		if buf.Len() == 0 {
			buf.WriteString("WHERE ")
		} else {
			buf.WriteString(" ")
			buf.WriteString(operator)
			buf.WriteString(" ")
		}
		buf.WriteString(field)
		sb._where = buf.String()
		return sb
	}

	//for _, value := range values {
	//	sb._param = append(sb._param, value)
	//}

}
func (sb *SQLBuilder) parseWhereInBySlice(operator string, condition string, field string, m reflect.Value) {
	knum := m.Len()
	if knum == 0 {
		panic("WhereIn 切片[] 长度必须大于0")
	}
	var bufstr strings.Builder
	bufstr.WriteString(sb._where) // append
	if bufstr.Len() == 0 {
		bufstr.WriteString("WHERE ")
	} else {
		bufstr.WriteString(" ")
		bufstr.WriteString(operator)
		bufstr.WriteString(" ")
	}
	bufstr.WriteString(field)
	if knum > 0 {
		plhs := GenPlaceholders(knum)
		bufstr.WriteString(" ")
		bufstr.WriteString(condition)
		bufstr.WriteString(" ")
		bufstr.WriteString("(")
		bufstr.WriteString(plhs)
		bufstr.WriteString(")")
		// 遍历切片并获取每个元素的值
		for i := 0; i < knum; i++ {
			element := m.Index(i).Interface()
			sb._param = append(sb._param, element)
		}
	}
	sb._where = bufstr.String()
}

// GroupBy set group by fields
func (sb *SQLBuilder) GroupBy(fields ...string) *SQLBuilder {
	var buf strings.Builder

	buf.WriteString("GROUP BY ")

	for k, field := range fields {

		buf.WriteString(field)

		if k != len(fields)-1 {
			buf.WriteString(",")
		}
	}

	sb._groupBy = buf.String()

	return sb
}

// 分组条件 Having("Gender>0") //必须配合group使用
func (sb *SQLBuilder) Having(havingstr string, args ...interface{}) *SQLBuilder {
	if sb._groupBy == "" { // group by not set
		return sb
	}
	if strings.Count(havingstr, "?") != len(args) {
		err := fmt.Sprintf("查询条件[%s]与参数个数不对应", args)
		panic(err)
	}
	var buf strings.Builder

	buf.WriteString(sb._having) // append

	if buf.Len() == 0 {
		buf.WriteString("HAVING ")
		buf.WriteString(havingstr)
	} else {
		buf.WriteString(havingstr)
	}

	sb._having = buf.String()
	if len(args) > 0 {
		sb._param = append(sb._param, args...)
	}
	return sb
}

// 排序字符 OrderBy("Uid")  OrderBy("Birth desc,Uid asc")
func (sb *SQLBuilder) OrderBy(operator string) *SQLBuilder {
	var buf strings.Builder
	buf.WriteString("ORDER BY ")
	buf.WriteString(operator)
	sb._orderBy = buf.String()
	return sb
}

// Limit set limit
func (sb *SQLBuilder) Limit(offset, num int) *SQLBuilder {
	var buf strings.Builder
	buf.WriteString(" LIMIT ")
	buf.WriteString(strconv.Itoa(offset))
	buf.WriteString(",")
	buf.WriteString(strconv.Itoa(num))
	//limit 0,1
	sb._limit = buf.String()
	return sb
}

// limitHint tries to extract the "num" from " LIMIT offset,num" for preallocation.
// Returns 0 when limit is not set or cannot be parsed.
func (sb *SQLBuilder) limitHint() int {
	lim := strings.TrimSpace(sb._limit)
	if lim == "" {
		return 0
	}
	lim = strings.TrimPrefix(lim, "LIMIT")
	lim = strings.TrimPrefix(lim, "limit")
	lim = strings.TrimSpace(lim)
	parts := strings.Split(lim, ",")
	if len(parts) != 2 {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// ForUpdate SQLSegments
func (sb *SQLBuilder) ForUpdate() *SQLBuilder {
	sb._forUpdate = true
	return sb
}

// JoinRaw join with raw sql
func (sb *SQLBuilder) JoinRaw(join string, args ...interface{}) *SQLBuilder {
	if strings.Count(join, "?") != len(args) {
		err := fmt.Sprintf("JoinRaw设置条件[%s]与参数个数不对应", join)
		panic(err)
	}
	var buf strings.Builder

	buf.WriteString(sb._join)
	if buf.Len() != 0 {
		buf.WriteString(" ")
	}
	buf.WriteString(join)

	sb._join = buf.String()
	if len(args) > 0 {
		sb._param = append(sb._param, args...)
	}
	return sb
}

// on duplicate key update设置 OnDku("Uid = ? and user_name=?", 1, "admin")
func (sb *SQLBuilder) OnDku(queryString string, args ...interface{}) *SQLBuilder {
	if strings.Count(queryString, "?") != len(args) {
		err := fmt.Sprintf("OnDku设置条件[%s]与参数个数不对应", queryString)
		panic(err)
	}
	var buf strings.Builder
	buf.WriteString(" ON DUPLICATE KEY UPDATE ")
	buf.WriteString(queryString)
	if len(args) > 0 {
		sb._param = append(sb._param, args...)
	}
	return sb
}

// 可以输入map或strcut或者他们的指针，map效率比struct高
func (sb *SQLBuilder) AddOne(s interface{}) *SQLBuilder {
	sb.insertScanOne(s)
	return sb
}

// 可以输入map或strcut或者他们的指针，map效率比struct高
func (sb *SQLBuilder) insertScanOne(dest interface{}) *SQLBuilder {
	value := reflect.ValueOf(dest)
	vtype := reflect.TypeOf(dest)
	if vtype.Kind() == reflect.Struct { //结构体,可设置字段时不为空
		rt := reflect.TypeOf(dest)
		sb.setInsetOneBystruct(rt, value)
	} else if vtype.Kind() == reflect.Ptr /*&& value.CanAddr()*/ {
		elemType := value.Type().Elem()
		if elemType.Kind() == reflect.Struct { //结构体指针
			PtrValue := reflect.Indirect(value)
			sb.setInsetOneBystruct(elemType, PtrValue)
		} else if elemType.Kind() == reflect.Map {
			PtrValue := reflect.Indirect(value)
			sb.setInsetOneByMap(PtrValue)
		}
	} else if vtype.Kind() == reflect.Map { //map
		sb.setInsetOneByMap(value)
	} else {

	}
	return sb
}
func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String: //v
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	}
	return false
}
func (sb *SQLBuilder) setInsetOneByMap(m reflect.Value) { //v
	knum := m.Len()
	if knum == 0 {
		panic("map 长度必须大于0")
	}
	var buf strings.Builder
	buf.WriteString("(")
	var valuebuf strings.Builder
	valuebuf.WriteString(") VALUES (")

	keys := m.MapKeys()
	for i, key := range keys {
		buf.WriteString("`")
		buf.WriteString(key.String())
		buf.WriteString("`")
		valuebuf.WriteString("?")
		if i != knum-1 {
			buf.WriteString(",")
			valuebuf.WriteString(",")
		}
		value := m.MapIndex(key).Interface().(string)
		sb._param = append(sb._param, value)
	}
	valuebuf.WriteString(")")
	sb._insert = buf.String() + valuebuf.String()

}

// 获取struct的标签并设置SQLBuilder.FieldMap及SQLBuilder.TableName
func (sb *SQLBuilder) setInsetOneBystruct(rt reflect.Type, rv reflect.Value) {
	var buf strings.Builder
	buf.WriteString("(")
	var valuebuf strings.Builder
	valuebuf.WriteString(") VALUES (")

	fieldNum := rt.NumField()

	hasValueNum := 0 //是否有设置值

	for i := 0; i < fieldNum; i++ {
		field := rt.Field(i)
		val := rv.Field(i).Interface()
		tagField := field.Tag.Get("field")
		//tagKey := field.Tag.Get("key")
		tagTable := field.Tag.Get("table")
		tagType := field.Tag.Get("type")
		tagIsAuto := strings.Trim(field.Tag.Get("auto"), " ") //是否为数据库自动字段
		tagallowempty := field.Tag.Get("empty")
		if tagField != "" && tagIsAuto != "1" { //不属于数据库字段不添加到字段值列表,没设置值的不添加 //字段赋值,非自增主键
			if tagallowempty != "1" && rv.Field(i).IsZero() {
				continue //字段不能为空 但值却为空则跳过
			}

			if tagallowempty == "1" && rv.Field(i).IsZero() {
				continue //字段可用为空且值也为空则跳过
			}

			if field.Type.Kind() == reflect.Struct && field.Type.String() == "time.Time" { //time类型
				fieldTime := val.(time.Time)
				if !fieldTime.IsZero() {
					if tagType == "date" { //date方式
						sb._param = append(sb._param, fieldTime.Format("2006-01-02"))
					} else if (tagType == "datetime") || (tagType == "timestamp") { //datetime timestamp方式
						//sb._param = append(sb._param, fieldTime.Format(time.RFC3339Nano))
						sb._param = append(sb._param, fieldTime.Format("2006-01-02T15:04:05.999999999"))
					} else if tagType == "int" { //uinx时间戳方式
						sb._param = append(sb._param, fieldTime.Unix())
					}
					hasValueNum++
					//构造字段表map
					if hasValueNum != 1 {
						buf.WriteString(",")
						valuebuf.WriteString(",")
					}
					var fmField string = tagField
					buf.WriteString("`")
					buf.WriteString(fmField)
					buf.WriteString("`")
					valuebuf.WriteString("?")
				}
			} else {
				hasValueNum++
				//构造字段表map
				if hasValueNum != 1 {
					buf.WriteString(",")
					valuebuf.WriteString(",")
				}
				var fmField string = tagField
				buf.WriteString("`")
				buf.WriteString(fmField)
				buf.WriteString("`")
				valuebuf.WriteString("?")
				sb._param = append(sb._param, val)
			}

			//添加到主键列表
			//if tagKey == "pk" {
			//sb.primaryKeys.Set(fmField, val)
			//}

		}

		//如果设置table标签用则采用table的值，如果已经执行过Table方法了，此标签失效
		if tagTable != "" && sb._table == "" {
			sb._table = tagTable
		}
	}
	valuebuf.WriteString(")")
	if hasValueNum > 0 {
		sb._insert = buf.String() + valuebuf.String()
	}
}

// 必须输入[]struct []map或者是指针 两者效率差不多
// 结构体的，time类型要补全
func (sb *SQLBuilder) AddMore(s interface{}, addtype ...int) *SQLBuilder {
	sb.insertScanMore(s)
	return sb
}

func (sb *SQLBuilder) insertScanMore(dest interface{}) *SQLBuilder {
	value := reflect.ValueOf(dest)
	//vtype := reflect.TypeOf(dest)
	//valuekind := value.Kind()
	if value.Kind() == reflect.Ptr && value.Type().Elem().Kind() == reflect.Slice {
		elemType := value.Type().Elem()
		if elemType.Elem().Kind() == reflect.Struct { //结构体指针
			PtrType := elemType.Elem().Elem()
			PtrValue := reflect.Indirect(value)
			sb.setInsetMoreBystruct(PtrType, PtrValue)
		} else if elemType.Elem().Kind() == reflect.Map {
			PtrValue := reflect.Indirect(value)
			sb.setInsetMoreByMap(PtrValue)
		}
	} else if value.Kind() == reflect.Slice { //结构体,可设置字段时不为空
		elemType := value.Type().Elem()
		if elemType.Kind() == reflect.Struct {
			//PtrValue := reflect.Indirect(value)
			sb.setInsetMoreBystruct(elemType, value)
		} else if elemType.Kind() == reflect.Map {
			//PtrValue := reflect.Indirect(value)
			sb.setInsetMoreByMap(value)
		}
	}
	return sb
}

func (sb *SQLBuilder) setInsetMoreBystruct(rt reflect.Type, rv reflect.Value) {
	knum := rv.Len()
	if knum == 0 {
		panic("Slice 长度必须大于0")
	}
	var buf strings.Builder
	buf.WriteString("(")
	var valuebuf strings.Builder
	valuebuf.WriteString(") VALUES (")
	for i := 0; i < rv.Len(); i++ {
		// 使用反射获取结构体的值信息
		structValue := rv.Index(i)
		var HasInsetNum = 0
		// 遍历结构体的字段
		for j := 0; j < structValue.NumField(); j++ {
			// 获取字段的键名和值
			//fieldName := rt.Field(i).Name
			fieldValue := structValue.Field(j).Interface()
			field := rt.Field(j)
			tagField := field.Tag.Get("field")
			tagKey := field.Tag.Get("key")
			//tagTable := field.Tag.Get("table")
			tagType := field.Tag.Get("type")
			tagIsAuto := field.Tag.Get("auto")      //是否为数据库自动字段
			if tagField != "" && tagIsAuto != "1" { //不属于数据库字段不添加到字段值列表,没设置值的不添加  为保证字段相同，去掉&& !structValue.Field(j).IsZero()
				//构造字段表map
				//var fmField string = tagField
				HasInsetNum++

				if i == 0 {
					if HasInsetNum != 1 {
						buf.WriteString(",")
					}
					buf.WriteString("`")
					buf.WriteString(tagField)
					buf.WriteString("`")
				}
				if HasInsetNum != 1 {
					valuebuf.WriteString(",")
				}
				valuebuf.WriteString("?")
				//if j != structValue.NumField()-1 {
				//	valuebuf.WriteString(",")
				//}
				if field.Type.Kind() == reflect.Struct && field.Type.String() == "time.Time" { //time类型
					fieldTime := fieldValue.(time.Time)
					if !fieldTime.IsZero() {
						if tagType == "date" { //date方式
							sb._param = append(sb._param, fieldTime.Format("2006-01-02"))
						} else if tagType == "datetime" { //datetime timestamp方式
							sb._param = append(sb._param, fieldTime.Format("2006-01-02T15:04:05.999999999"))
						} else if tagType == "int" { //uinx时间戳方式
							sb._param = append(sb._param, fieldTime.Unix())
						}
					}
				} else {
					sb._param = append(sb._param, fieldValue)
				}
				//添加到主键列表
				if tagKey == "pk" {
					//m.primaryKeys.Set(fmField, val)
				}

			}
		}
		if i == knum-1 {
			valuebuf.WriteString(")")
		} else {
			valuebuf.WriteString("),(")
		}
	}
	sb._insert = buf.String() + valuebuf.String()
}

func (sb *SQLBuilder) setInsetMoreByMap(m reflect.Value) {
	knum := m.Len()
	if knum == 0 {
		panic("map 长度必须大于0")
	}
	var buf strings.Builder
	buf.WriteString("(")
	var valuebuf strings.Builder
	valuebuf.WriteString(") VALUES (")

	// 遍历切片中的每个map
	firstmaplen := m.Index(0).Len() //获取每个切片的长度
	for i := 0; i < knum; i++ {
		// 获取切片中的每个map的值
		mapValue := m.Index(i)
		maplen := mapValue.Len() //获取每个切片的长度
		if maplen == 0 || firstmaplen != maplen {
			panic("切片中的每个map的长度必须大于0且等同")
		}
		keys := mapValue.MapKeys()
		for ik, key := range keys {
			if i == 0 { //第一个切片
				buf.WriteString(key.String())
				if ik < maplen-1 {
					buf.WriteString(",")
				}
			}
			valuebuf.WriteString("?")
			if ik != maplen-1 {
				valuebuf.WriteString(",")
			}
			value := mapValue.MapIndex(key).Interface().(string)
			sb._param = append(sb._param, value)
		}
		if i == knum-1 {
			valuebuf.WriteString(")")
		} else {
			valuebuf.WriteString("),(")
		}
	}
	sb._insert = buf.String() + valuebuf.String()
}

// set("user=?,id=?",username,id)
func (sb *SQLBuilder) Set(updatestr string, values ...interface{}) *SQLBuilder {

	if updatestr != "" {
		if sb._where != "" {
			panic(errors.New("where语句必须再set之后出现"))
		}
		if sb._update == "" {
			sb._update = "SET " + updatestr
		} else {
			var buf strings.Builder
			buf.WriteString(",")
			buf.WriteString(updatestr)
			sb._update = buf.String()
		}
		if len(values) > 0 {
			sb._param = append(sb._param, values...)
		}

	}
	return sb
}

// 可以输入map或strcut或者他们的指针，struct效率比map高
func (sb *SQLBuilder) Sets(dest interface{}) *SQLBuilder {
	if sb._where != "" {
		panic(errors.New("where语句必须再set之后出现"))
	}
	value := reflect.ValueOf(dest)
	vtype := reflect.TypeOf(dest)
	if vtype.Kind() == reflect.Struct { //结构体,可设置字段时不为空
		rt := reflect.TypeOf(dest)
		sb.setUpdateBystruct(rt, value)
	} else if vtype.Kind() == reflect.Ptr {
		elemType := value.Type().Elem()
		if elemType.Kind() == reflect.Struct { //结构体指针
			PtrValue := reflect.Indirect(value)
			sb.setUpdateBystruct(elemType, PtrValue)
		} else if elemType.Kind() == reflect.Map {
			PtrValue := reflect.Indirect(value)
			sb.setUpdateByMap(PtrValue)
		}
	} else if vtype.Kind() == reflect.Map { //map
		sb.setUpdateByMap(value)
	}
	return sb
}

func (sb *SQLBuilder) setUpdateByMap(m reflect.Value) {
	knum := m.Len()
	if knum == 0 {
		panic("map 长度必须大于0")
	}
	var buf strings.Builder
	buf.WriteString("Set ")
	keys := m.MapKeys()
	for i, key := range keys {
		buf.WriteString(key.String())
		buf.WriteString("=?")
		if i != knum-1 {
			buf.WriteString(",")
		}
		value := m.MapIndex(key).Interface().(string)
		sb._param = append(sb._param, value)
	}
	if sb._update == "" {
		sb._update = buf.String()
	} else {
		sb._update = "," + buf.String()
	}
}

func (sb *SQLBuilder) setUpdateBystruct(rt reflect.Type, rv reflect.Value) {
	var buf strings.Builder
	buf.WriteString("Set ")
	fieldNum := rt.NumField()

	hasValueNum := 0 //是否有设置值

	for i := 0; i < fieldNum; i++ {
		field := rt.Field(i)
		val := rv.Field(i).Interface()
		tagField := field.Tag.Get("field")
		tagTable := field.Tag.Get("table")
		tagType := field.Tag.Get("type")
		//tagkey := field.Tag.Get("key")
		tagallowempty := field.Tag.Get("empty")
		tagIsAuto := strings.Trim(field.Tag.Get("auto"), " ") //是否为数据库自动字段
		if tagField != "" && tagIsAuto != "1" {               //不属于数据库字段不添加到字段值列表,没设置值的不添加&& !rv.Field(i).IsZero()
			if tagallowempty != "1" && rv.Field(i).IsZero() {
				continue //字段不能为空 但值却为空则跳过
			}

			if tagallowempty == "1" && rv.Field(i).IsZero() {
				continue //字段可用为空且值也为空则跳过
			}

			if field.Type.Kind() == reflect.Struct && field.Type.String() == "time.Time" { //time类型
				fieldTime := val.(time.Time)
				if !fieldTime.IsZero() {
					if tagType == "date" { //date方式
						sb._param = append(sb._param, fieldTime.Format("2006-01-02"))
					} else if (tagType == "datetime") || (tagType == "timestamp") { //datetime timestamp方式
						sb._param = append(sb._param, fieldTime.Format("2006-01-02T15:04:05.999999999"))
					} else if tagType == "int" { //uinx时间戳方式
						sb._param = append(sb._param, fieldTime.Unix())
					}
					hasValueNum++
					//构造字段表map
					var fmField string = tagField
					if hasValueNum != 1 {
						buf.WriteString(",")
					}

					buf.WriteString(fmField)
					buf.WriteString("=?")
				}
			} else {
				hasValueNum++
				//构造字段表map
				var fmField string = tagField
				if hasValueNum != 1 {
					buf.WriteString(",")
				}

				buf.WriteString(fmField)
				buf.WriteString("=?")
				sb._param = append(sb._param, val)
			}
		}
		//如果设置table标签用则采用table的值，如果已经执行过Table方法了，此标签失效
		if tagTable != "" && sb._table == "" {
			sb._table = tagTable
		}
	}
	if hasValueNum > 0 {
		if sb._update == "" {
			sb._update = buf.String()
		} else {
			sb._update = "," + buf.String()
		}
	}
	//if hasValueNum != fieldNum {
	//sb._update = sb._update[:len(sb._update)-1]
	//}
}

func (sb *SQLBuilder) ReSet() *SQLBuilder {
	sb._select = ""
	sb._insert = ""
	sb._update = ""
	sb._delete = ""
	sb._table = ""
	sb._join = ""
	sb._where = ""
	sb._groupBy = ""
	sb._having = ""
	sb._orderBy = ""
	sb._limit = ""
	sb._tbPre = ""
	sb._forUpdate = false
	sb._dkuStr = ""
	sb._methodtype = 6          //select
	sb._param = sb._param[:0:0] //清空参数列表
	if sb.isAutoCommit {        //非事物
		sb.tx = nil
	}
	sb._SQL = ""
	sb._Err = nil
	return sb
}

// ////////////////////////////////////////下面开始数据库操作、、、、、、、、、、、、、、、、、、
func (sb *SQLBuilder) QBI(typ ...int) *SQLBuilder {
	if len(typ) > 0 {
		if typ[0] > 3 {
			sb._methodtype = 1
		} else {
			sb._methodtype = typ[0]
		}
	} else {
		sb._methodtype = 1
	}
	return sb
}

func (sb *SQLBuilder) QBS() *SQLBuilder {
	sb._methodtype = 6
	return sb
}

func (sb *SQLBuilder) QBU() *SQLBuilder {
	sb._methodtype = 4
	return sb
}

func (sb *SQLBuilder) QBD() *SQLBuilder {
	sb._methodtype = 5
	return sb
}

func (sb *SQLBuilder) ShowSql() (string, []interface{}, error) {
	//sb.dbmodel.PutSb(sb)
	var sql string
	var err error
	switch sb._methodtype {
	case 1, 2, 3:
		sql, err = sb.GetInsertSQL()
	case 4:
		sql, err = sb.GetUpdateSQL()
	case 5:
		sql, err = sb.GetDeleteSQL()
	case 6:
		sql, err = sb.GetQuerySQL()
	}
	sb._SQL = sql
	return sql, sb._param, err
}

func (sb *SQLBuilder) getSql() (string, []interface{}, error) {
	var sql string
	var err error
	switch sb._methodtype {
	case 1, 2, 3:
		sql, err = sb.GetInsertSQL()
	case 4:
		sql, err = sb.GetUpdateSQL()
	case 5:
		sql, err = sb.GetDeleteSQL()
	case 6:
		sql, err = sb.GetQuerySQL()
	}
	sb._SQL = sql
	return sql, sb._param, err
}

// 查询单行，这里error只提示数据库没有实例,没法判断有没数据，必须调用row.scan来获取err，err == sql.ErrNoRows
func (sb *SQLBuilder) QueryRow(sqlstr string, args ...interface{}) (*sql.Row, error) {
	if sb.isAutoCommit {
		defer sb.Free()
	}
	defer sb.Log.ShowLog()
	return sb._QueryRow(sqlstr, args...)

}

// 查询单行，这里error只提示数据库没有实例
func (sb *SQLBuilder) _QueryRow(sqlstr string, args ...interface{}) (row *sql.Row, err error) {
	defer func() { // 捕捉panic并转成error，避免静默失败
		if r := recover(); r != nil {
			err = fmt.Errorf("QueryRow panic: %v\n%s", r, debug.Stack())
			sb._Err = err
		}
	}()
	if !sb.isAutoCommit && sb.tx != nil { //事物
		row = sb.tx.QueryRow(sqlstr, args...)
		return row, nil
	} else {
		if sb.dbmodel == nil || sb.dbmodel.reader == nil { //如果数据库实例未能获取到，返回空列表
			return nil, errors.New("从数据库未能获取到实例")
		}
		row = sb.dbmodel.reader.QueryRow(sqlstr, args...)
		return row, nil
	}
}

// 数据库查询操作（select） 冲数据库,主要一定要close释放
func (sb *SQLBuilder) Query(sqlstr string, args ...interface{}) (*sql.Rows, error) {
	if sb.isAutoCommit {
		defer sb.Free()
	}
	defer sb.Log.ShowLog()
	return sb._Query(sqlstr, args...)
}

func (sb *SQLBuilder) _Query(sqlstr string, args ...interface{}) (rows *sql.Rows, err error) {
	defer func() { // 捕捉panic并转成error，避免静默失败
		if r := recover(); r != nil {
			err = fmt.Errorf("Query panic: %v\n%s", r, debug.Stack())
			sb._Err = err
		}
	}()
	if !sb.isAutoCommit && sb.tx != nil { //事物
		return sb.tx.Query(sqlstr, args...)
	} else {
		if sb.dbmodel == nil || sb.dbmodel.reader == nil { //如果数据库实例未能获取到，返回空列表
			return nil, errors.New("从数据库未能获取到实例")
		}
		return sb.dbmodel.reader.Query(sqlstr, args...)
	}
	/*stmp, err := sb.dbReader.Prepare(sqlstr)
	if err != nil {
		return nil, err
	}
	defer stmp.Close()
	return stmp.Query(args...)*/
}

// 数据库修改操作（insert/update/delete） 主数据库
func (sb *SQLBuilder) Execute(sqlstr string, args ...interface{}) (res sql.Result, err error) {
	defer func() { // 捕捉panic并转成error，避免静默失败
		if r := recover(); r != nil {
			err = fmt.Errorf("Execute panic: %v\n%s", r, debug.Stack())
			sb._Err = err
		}
	}()
	if !sb.isAutoCommit && sb.tx != nil { //事物
		stmt, err := sb.tx.Prepare(sqlstr)
		if err != nil {
			return nil, err
		}
		defer stmt.Close()
		return stmt.Exec(args...)

	} else {
		if sb.dbmodel == nil || sb.dbmodel.writer == nil {
			return nil, errors.New("主数据库未能获取到实例")
		}
		stmt, err := sb.dbmodel.writer.Prepare(sqlstr)
		if err != nil {
			return nil, err
		}
		defer stmt.Close()
		return stmt.Exec(args...)
	}
}

// 数据库修改操作（insert/update/delete） 主数据库
func (sb *SQLBuilder) Exec(sqlstr string, args ...interface{}) (res sql.Result, err error) {
	defer func() { // 捕捉panic并转成error，避免静默失败
		if r := recover(); r != nil {
			err = fmt.Errorf("Exec panic: %v\n%s", r, debug.Stack())
			sb._Err = err
		}
	}()
	if !sb.isAutoCommit && sb.tx != nil { //事物
		return sb.tx.Exec(sqlstr, args...)
	} else {
		if sb.dbmodel == nil || sb.dbmodel.writer == nil {
			return nil, errors.New("主数据库未能获取到实例")
		}
		return sb.dbmodel.writer.Exec(sqlstr, args...)
	}
}

// 把返回值直接赋值到多个参数,参数不能小于1个,和字段对应,必须指针
func (sb *SQLBuilder) Get(dest ...interface{}) error {
	if len(dest) > 0 {
		if sb.isAutoCommit {
			defer sb.Free()
		} else {
			defer sb.ReSet()
		}
		defer sb.Log.ShowLog()
		sb._methodtype = 6
		sb.Limit(0, 1)
		sqlstr, params, err := sb.getSql()
		if err != nil {
			return err
		}
		//defer sb.ReSet()
		rows, err := sb._Query(sqlstr, params...)
		if err != nil {
			return err
		}
		r := &Row{rows: rows, err: err, unsafe: sb.unsafe, Mapper: sb.Mapper}
		return r.Scan(dest...)
	} else {
		return errors.New("GET函数必须带有一个或多个参数，和字段对应")
	}
}

// 这个速度快 用指针 没有数据会返回错误sql.ErrNoRows
func (sb *SQLBuilder) Find(dest interface{}) error {
	if sb.isAutoCommit {
		defer sb.Free()
	} else {
		defer sb.ReSet()
	}
	defer sb.Log.ShowLog()
	value := reflect.ValueOf(dest)
	//vtype := reflect.TypeOf(dest)
	if value.Kind() == reflect.Ptr && value.Type().Elem().Kind() == reflect.Struct {
		sb._methodtype = 6
		sb.Limit(0, 1)
		sqlstr, params, err := sb.getSql()
		if err != nil {
			return err
		}
		//defer sb.ReSet()
		rows, err := sb._Query(sqlstr, params...)
		if err != nil {
			return err
		}
		r := &Row{rows: rows, err: err, unsafe: sb.unsafe, Mapper: sb.Mapper}
		return r.scanAny(dest, false)
	} else {
		return errors.New("参数必须为struct的指针")
	}
}

// 这个速度快 用指针,要判断有没数据必须 判断dest ！=nil 或者 len（dest）>0
func (sb *SQLBuilder) FindList(dest interface{}) error {
	if sb.isAutoCommit {
		defer sb.Free()
	} else {
		defer sb.ReSet()
	}
	defer sb.Log.ShowLog()
	value := reflect.ValueOf(dest)
	vtype := reflect.TypeOf(dest)
	//rt := rv.Type()
	if vtype.Kind() == reflect.Ptr && value.Type().Elem().Kind() == reflect.Slice {
		// 预扩容：当使用 Limit 时，可显著减少 slice 扩容拷贝成本
		if hint := sb.limitHint(); hint > 0 {
			direct := reflect.Indirect(value)
			if direct.Kind() == reflect.Slice && direct.Cap() < hint {
				direct.Set(reflect.MakeSlice(direct.Type(), 0, hint))
			}
		}
		sb._methodtype = 6
		sqlstr, params, err := sb.getSql()
		if err != nil {
			return err
		}
		//defer sb.ReSet()
		rows, err := sb._Query(sqlstr, params...)
		if err != nil {
			return err
		}
		r := &Rows{Rows: rows, unsafe: sb.unsafe, Mapper: sb.Mapper}
		defer r.Close()
		return scanAll(r, dest, false)
	} else {
		return errors.New("参数必须为[]struct的指针")
	}
}

// 数据量大时必须用select，用其他的和原生差距一倍以上  内存小，必须close
func (sb *SQLBuilder) Select() (*sql.Rows, error) {
	if sb.isAutoCommit {
		defer sb.Free()
	} else {
		defer sb.ReSet()
	}
	defer sb.Log.ShowLog()
	sb._methodtype = 6
	sqlstr, params, err := sb.getSql()
	if err != nil {
		return nil, err
	}
	//defer sb.ReSet()
	return sb._Query(sqlstr, params...)
}

func (sb *SQLBuilder) Delete() (int64, error) {
	if sb.isAutoCommit {
		defer sb.Free()
	} else {
		defer sb.ReSet()
	}
	defer sb.Log.ShowLog()
	sb._methodtype = 5
	sqlstr, params, err := sb.getSql()
	if err != nil {
		return 0, err
	}
	//defer sb.ReSet()
	res, err := sb.Execute(sqlstr, params...)
	if err != nil {
		return 0, err
	} else {
		rows, err := res.RowsAffected()
		return rows, err
	}
}

// typ参数，1=insert，2=IGNORE INTO，3=REPLACE INSERT
func (sb *SQLBuilder) Insert(lastinsertid bool) (int64, error) {
	if sb.isAutoCommit {
		defer sb.Free()
	} else {
		defer sb.ReSet()
	}
	defer sb.Log.ShowLog()
	sb._methodtype = 1
	sqlstr, params, err := sb.getSql()
	if err != nil {
		return 0, err
	}
	//defer sb.ReSet()
	res, err := sb.Exec(sqlstr, params...)
	if err != nil {
		return 0, err
	} else {
		if lastinsertid {
			return res.LastInsertId()
		} else {
			return res.RowsAffected()
		}
	}
}

func (sb *SQLBuilder) ReplaceInsert(lastinsertid bool) (int64, error) {
	if sb.isAutoCommit {
		defer sb.Free()
	} else {
		defer sb.ReSet()
	}
	defer sb.Log.ShowLog()
	sb._methodtype = 3
	sqlstr, params, err := sb.getSql()
	if err != nil {
		return 0, err
	}
	//defer sb.ReSet()
	res, err := sb.Exec(sqlstr, params...)
	if err != nil {
		return 0, err
	} else {
		if lastinsertid {
			return res.LastInsertId()
		} else {
			return res.RowsAffected()
		}
	}
}

func (sb *SQLBuilder) IgnoreInsert(lastinsertid bool) (int64, error) {
	if sb.isAutoCommit {
		defer sb.Free()
	} else {
		defer sb.ReSet()
	}
	defer sb.Log.ShowLog()
	sb._methodtype = 2
	sqlstr, params, err := sb.getSql()
	if err != nil {
		return 0, err
	}
	//defer sb.ReSet()
	res, err := sb.Exec(sqlstr, params...)
	if err != nil {
		return 0, err
	} else {
		if lastinsertid {
			return res.LastInsertId()
		} else {
			return res.RowsAffected()
		}
	}
}

// 要注意set和where的顺序，避免参数混乱
func (sb *SQLBuilder) Update() (int64, error) {
	if sb.isAutoCommit {
		defer sb.Free()
	} else {
		defer sb.ReSet()
	}
	//starttime := time.Now()
	defer sb.Log.ShowLog()
	sb._methodtype = 4
	sqlstr, params, err := sb.getSql()
	if err != nil {
		return 0, err
	}
	//defer sb.ReSet()
	res, err := sb.Exec(sqlstr, params...)
	//endtime := time.Now()
	//fmt.Println("cost time=%v", endtime.Sub(starttime))
	if err != nil {
		return 0, err
	} else {
		rows, err := res.RowsAffected()
		return rows, err
	}
}

/*
// Run 开启一个事务并且自动提交或者回滚
// 如果txFunc返回error或者panic，事务会回滚
func (sb *SQLBuilder) Run(txFunc func() (interface{}, error)) (interface{}, error) {
	tx, err := sb.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction failed. reason: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			// panic(p) // re-throw panic
		} else if err != nil {
			tx.Rollback() // err is non-nil; don't change it
		} else {
			err = tx.Commit() // err is nil; if Commit returns error update err
		}
	}()

	vin, err := txFunc()
	return vin, err
}
*/
// Begin() 后要手工调用free函数
func (sb *SQLBuilder) Begin() (*SQLBuilder, error) {
	var err error
	if !sb.isAutoCommit && sb.tx != nil {
		return sb, errors.New("事务已开启，禁止重复 begin")
	}
	if sb.dbmodel == nil || sb.dbmodel.writer == nil {
		return sb, errors.New("主数据库未能获取到实例")
	}
	sb.tx, err = sb.dbmodel.writer.Begin()
	if err != nil {
		return sb, err
	}
	sb.isAutoCommit = false
	return sb, err
}

func (sb *SQLBuilder) Rollback() (*SQLBuilder, error) {
	if !sb.isAutoCommit && sb.tx != nil {
		defer sb.Log.ShowLog()
		err := sb.tx.Rollback()
		sb.isAutoCommit = true
		sb.tx = nil
		return sb, err
	}
	return sb, errors.New("数据库事物必须先调用begin函数")
}

func (sb *SQLBuilder) Commit() (*SQLBuilder, error) {
	if !sb.isAutoCommit && sb.tx != nil {
		defer sb.Log.ShowLog()
		err := sb.tx.Commit()
		sb.isAutoCommit = true
		sb.tx = nil
		return sb, err
	}
	return sb, errors.New("数据库事物必须先调用begin函数")
}

// InTx reports whether the builder is currently in an active transaction.
func (sb *SQLBuilder) InTx() bool {
	return sb != nil && !sb.isAutoCommit && sb.tx != nil
}

// GenPlaceholders generate placeholders
func GenPlaceholders(n int) string {
	var buf strings.Builder

	for i := 0; i < n-1; i++ {
		buf.WriteString("?,")
	}

	if n > 0 {
		buf.WriteString("?")
	}

	return buf.String()
}

/*
// /////////////////////////////////////下面的是测试用，性能比较差

func (sb *SQLBuilder) FindListNew(dest interface{}) error {
	rv := reflect.Indirect(reflect.ValueOf(dest))
	rt := rv.Type().Elem()
	rvNew := reflect.New(rt)
	//defer sb.dbmodel.PutSb(sb)
	sqlstr, params, err := sb.getSql()
	if err != nil {
		return err
	}
	rows, err := sb._Query(sqlstr, params...)
	if err != nil {
		return err
	}
	defer rows.Close()
	columns, err := rows.Columns() //得到字段名
	if err != nil {
		return err
	}
	values := make([]sql.RawBytes, len(columns))
	scanArgs := make([]interface{}, len(values))

	for i := range values {
		scanArgs[i] = &values[i]
	}
	//ret := make([]map[string]string, 0) //返回的结果集
	for rows.Next() {
		err = rows.Scan(scanArgs...)
		if err != nil {
			return err
		}
		var val string
		vmap := make(map[string]string, len(scanArgs))
		for i, col := range values {
			//			fmt.Printf("type:%v\r\n", reflect.ValueOf(col).Kind())
			if col == nil {
				val = "NULL"
			} else {
				val = string(col)
			}
			vmap[columns[i]] = val
		}
		err := sb.ConvertMapToModel(vmap, rvNew.Interface()) //将单条写进struct对象
		if err != nil {
			return err
		}
		rv.Set(reflect.Append(rv, reflect.Indirect(reflect.ValueOf(rvNew.Interface()))))
		//ret = append(ret, vmap)
	}
	return nil
}

func (sb *SQLBuilder) FindListNew1(dest interface{}) error {
	rv := reflect.Indirect(reflect.ValueOf(dest))
	rt := rv.Type().Elem()
	rvNew := reflect.New(rt)
	//defer sb.dbmodel.PutSb(sb)
	sqlstr, params, err := sb.getSql()
	if err != nil {
		return err
	}
	rows, err := sb._Query(sqlstr, params...)
	if err != nil {
		return err
	}
	defer rows.Close()
	columns, err := rows.Columns() //得到字段名
	if err != nil {
		return err
	}
	values := make([]sql.RawBytes, len(columns))
	scanArgs := make([]interface{}, len(values))

	for i := range values {
		scanArgs[i] = &values[i]
	}
	//ret := make([]map[string]string, 0) //返回的结果集
	for rows.Next() {
		err = rows.Scan(scanArgs...)
		if err != nil {
			return err
		}
		var dbValue string
		for i, col := range values {
			//			fmt.Printf("type:%v\r\n", reflect.ValueOf(col).Kind())
			if col == nil {
				dbValue = "NULL"
			} else {
				dbValue = string(col)
			}
			colname := Text.FirstCharToUpper(columns[i])
			fieldValue := reflect.Indirect(rvNew).FieldByName(colname)
			if !fieldValue.IsValid() {
				continue
			}
			field, err := reflect.Indirect(rvNew).Type().FieldByName(colname)
			if err == false {
				continue
			}
			tagType := field.Tag.Get("type")
			//tagTimeParse := field.Tag.Get("type") //时间解析函数
			if field.Type.Kind() == reflect.Struct && field.Type.String() != "time.Time" {
				return errors.New("不支持除Time类型外的其他类型的转换")
			}
			if col != nil {
				var newValue interface{}
				switch field.Type.Kind() {
				case reflect.String:
					newValue = dbValue
				case reflect.Bool:
					newValue = dbValue == "1"
				case reflect.Int:
					x, err := strconv.Atoi(dbValue)
					if err != nil {
						return errors.New(fmt.Sprintf("字段[%s]不能转换为Int,错误：%s\n", dbValue, err.Error()))
					}
					newValue = x
				case reflect.Int8:
					x, err := strconv.ParseInt(dbValue, 10, 8)
					if err != nil {
						return errors.New(fmt.Sprintf("字段[%s]不能转换为Int8,错误：%s\n", dbValue, err.Error()))
					}
					newValue = int8(x)
				case reflect.Int16:
					x, err := strconv.ParseInt(dbValue, 10, 16)
					if err != nil {
						return errors.New(fmt.Sprintf("字段[%s]不能转换为Int16,错误：%s\n", dbValue, err.Error()))
					}
					newValue = int16(x)
				case reflect.Int32:
					x, err := strconv.ParseInt(dbValue, 10, 32)
					if err != nil {
						return errors.New(fmt.Sprintf("字段[%s]不能转换为Int32,错误：%s\n", dbValue, err.Error()))
					}
					newValue = int32(x)
				case reflect.Int64:
					x, err := strconv.ParseInt(dbValue, 10, 64)
					if err != nil {
						return errors.New(fmt.Sprintf("字段[%s]不能转换为Int64,错误：%s\n", dbValue, err.Error()))
					}
					newValue = x
				case reflect.Float32:
					x, err := strconv.ParseFloat(dbValue, 32)
					if err != nil {
						return errors.New(fmt.Sprintf("字段[%s]不能转换为Float32,错误：%s\n", dbValue, err.Error()))
					}
					newValue = float32(x)
				case reflect.Float64:
					x, err := strconv.ParseFloat(dbValue, 64)
					if err != nil {
						return errors.New(fmt.Sprintf("字段[%s]不能转换为Float64,错误：%s\n", dbValue, err.Error()))
					}
					newValue = x
				case reflect.Uint8:
					x, err := strconv.ParseUint(dbValue, 10, 8)
					if err != nil {
						return errors.New(fmt.Sprintf("字段[%s]不能转换为Uint,错误：%s\n", dbValue, err.Error()))
					}
					newValue = uint8(x)
				case reflect.Uint16:
					x, err := strconv.ParseUint(dbValue, 10, 16)
					if err != nil {
						return errors.New(fmt.Sprintf("字段[%s]不能转换为Uint,错误：%s\n", dbValue, err.Error()))
					}
					newValue = uint16(x)
				case reflect.Uint32:
					x, err := strconv.ParseUint(dbValue, 10, 32)
					if err != nil {
						return errors.New(fmt.Sprintf("字段[%s]不能转换为Uint,错误：%s\n", dbValue, err.Error()))
					}
					newValue = uint32(x)
				case reflect.Uint64:
					x, err := strconv.ParseUint(dbValue, 10, 64)
					if err != nil {
						return errors.New(fmt.Sprintf("字段[%s]不能转换为Uint,错误：%s\n", dbValue, err.Error()))
					}
					newValue = x
				case reflect.Struct: //解析时间

					var x time.Time
					var err error
					switch tagType {
					case "int":
						intTime, err1 := strconv.ParseInt(dbValue, 10, 64)
						if err1 == nil {
							x = time.Unix(intTime, 0)
						} else {
							return errors.New("时间格式不支持: " + dbValue)
						}
					default:
						x, err = ParseTime(dbValue)
						if err != nil {
							intTime, err := strconv.ParseInt(dbValue, 10, 64)
							if err == nil {
								x = time.Unix(intTime, 0)
							} else {
								return errors.New("时间格式不支持: " + dbValue)
							}

						}

					}
					newValue = x

				default:
					return errors.New("未发现可以支持的类型: " + reflect.TypeOf(newValue).String())
				}
				fieldValue.Set(reflect.ValueOf(newValue)) //将字段写入struct
			}

		}

		rv.Set(reflect.Append(rv, reflect.Indirect(reflect.ValueOf(rvNew.Interface()))))
		//ret = append(ret, vmap)
	}
	return nil
}

func (sb *SQLBuilder) FindListNew2(dest interface{}) error {
	rv := reflect.Indirect(reflect.ValueOf(dest))
	rt := rv.Type().Elem()
	rvNew := reflect.New(rt)
	//defer sb.dbmodel.PutSb(sb)
	sqlstr, params, err := sb.getSql()
	if err != nil {
		return err
	}
	rows, err := sb._Query(sqlstr, params...)
	if err != nil {
		return err
	}
	defer rows.Close()
	columns, err := rows.Columns() //得到字段名
	if err != nil {
		return err
	}
	values := make([]sql.RawBytes, len(columns))
	scanArgs := make([]interface{}, len(values))

	for i := range values {
		scanArgs[i] = &values[i]
	}

	//ret := make([]map[string]string, 0) //返回的结果集
	for rows.Next() {
		scan_dest := []interface{}{}
		for i := 0; i < len(columns); i++ {
			columns_name := Text.FirstCharToUpper(columns[i])
			_, err := reflect.Indirect(rvNew).Type().FieldByName(columns_name)
			if err == false {
				scan_dest = append(scan_dest, scanArgs[i])
			} else {
				fieldValue := reflect.Indirect(rvNew).FieldByName(columns_name)
				if !fieldValue.IsValid() {
					scan_dest = append(scan_dest, scanArgs[i])
				} else {
					valuePtr := fieldValue.Addr().Interface() //获取字段地址并转换为interface{}类型
					scan_dest = append(scan_dest, valuePtr)   //将字段地址添加到scanArgs切片中
				}

			}
		}
		err = rows.Scan(scan_dest...)
		if err != nil {
			return err
		}
		rv.Set(reflect.Append(rv, reflect.Indirect(reflect.ValueOf(rvNew.Interface()))))
		//ret = append(ret, vmap)
	}
	return nil
}

func MapToStruct(m map[string]string, to interface{}) error {
	model := reflect.Indirect(reflect.ValueOf(to))
	modelType := model.Type()
	if model.Kind() != reflect.Struct {
		return errors.New("expected a pointer to a struct")
	}
	for i := 0; i < model.NumField(); i++ {
		fieldValue := model.Field(i)
		field := modelType.Field(i)
		var sKey string
		fieldTag := field.Tag.Get("field")
		if fieldTag != "" {
			sKey = fieldTag
		} else {
			sKey = field.Name
		}
		tagType := field.Tag.Get("type")
		//tagTimeParse := field.Tag.Get("type") //时间解析函数
		if field.Type.Kind() == reflect.Struct && field.Type.String() != "time.Time" {
			return errors.New("不支持除Time类型外的其他类型的转换")
		}
		if dbValue, ok := m[sKey]; ok {
			//值转换
			var newValue interface{}
			switch field.Type.Kind() {
			case reflect.String:
				newValue = dbValue
			case reflect.Bool:
				newValue = dbValue == "1"
			case reflect.Int:
				x, err := strconv.Atoi(dbValue)
				if err != nil {
					return errors.New(fmt.Sprintf("字段[%s]不能转换为Int,错误：%s\n", dbValue, err.Error()))
				}
				newValue = x
			case reflect.Int8:
				x, err := strconv.ParseInt(dbValue, 10, 8)
				if err != nil {
					return errors.New(fmt.Sprintf("字段[%s]不能转换为Int8,错误：%s\n", dbValue, err.Error()))
				}
				newValue = int8(x)
			case reflect.Int16:
				x, err := strconv.ParseInt(dbValue, 10, 16)
				if err != nil {
					return errors.New(fmt.Sprintf("字段[%s]不能转换为Int16,错误：%s\n", dbValue, err.Error()))
				}
				newValue = int16(x)
			case reflect.Int32:
				x, err := strconv.ParseInt(dbValue, 10, 32)
				if err != nil {
					return errors.New(fmt.Sprintf("字段[%s]不能转换为Int32,错误：%s\n", dbValue, err.Error()))
				}
				newValue = int32(x)
			case reflect.Int64:
				x, err := strconv.ParseInt(dbValue, 10, 64)
				if err != nil {
					return errors.New(fmt.Sprintf("字段[%s]不能转换为Int64,错误：%s\n", dbValue, err.Error()))
				}
				newValue = x
			case reflect.Float32:
				x, err := strconv.ParseFloat(dbValue, 32)
				if err != nil {
					return errors.New(fmt.Sprintf("字段[%s]不能转换为Float32,错误：%s\n", dbValue, err.Error()))
				}
				newValue = float32(x)
			case reflect.Float64:
				x, err := strconv.ParseFloat(dbValue, 64)
				if err != nil {
					return errors.New(fmt.Sprintf("字段[%s]不能转换为Float64,错误：%s\n", dbValue, err.Error()))
				}
				newValue = x
			case reflect.Uint8:
				x, err := strconv.ParseUint(dbValue, 10, 8)
				if err != nil {
					return errors.New(fmt.Sprintf("字段[%s]不能转换为Uint,错误：%s\n", dbValue, err.Error()))
				}
				newValue = uint8(x)
			case reflect.Uint16:
				x, err := strconv.ParseUint(dbValue, 10, 16)
				if err != nil {
					return errors.New(fmt.Sprintf("字段[%s]不能转换为Uint,错误：%s\n", dbValue, err.Error()))
				}
				newValue = uint16(x)
			case reflect.Uint32:
				x, err := strconv.ParseUint(dbValue, 10, 32)
				if err != nil {
					return errors.New(fmt.Sprintf("字段[%s]不能转换为Uint,错误：%s\n", dbValue, err.Error()))
				}
				newValue = uint32(x)
			case reflect.Uint64:
				x, err := strconv.ParseUint(dbValue, 10, 64)
				if err != nil {
					return errors.New(fmt.Sprintf("字段[%s]不能转换为Uint,错误：%s\n", dbValue, err.Error()))
				}
				newValue = x
			case reflect.Struct: //解析时间

				var x time.Time
				var err error
				switch tagType {
				case "int":
					intTime, err1 := strconv.ParseInt(dbValue, 10, 64)
					if err1 == nil {
						x = time.Unix(intTime, 0)
					} else {
						return errors.New("时间格式不支持: " + dbValue)
					}
				default:
					x, err = ParseTime(dbValue)
					if err != nil {
						intTime, err := strconv.ParseInt(dbValue, 10, 64)
						if err == nil {
							x = time.Unix(intTime, 0)
						} else {
							return errors.New("时间格式不支持: " + dbValue)
						}

					}

				}
				newValue = x

			default:
				return errors.New("未发现可以支持的类型: " + reflect.TypeOf(newValue).String())
			}
			//fmt.Printf("%v:%v;type:%v\r\n", field.Name, newValue, reflect.TypeOf(newValue).String())
			fieldValue.Set(reflect.ValueOf(newValue)) //将字段写入struct
		}
	}
	return nil
}

const (
	dateFormat         = "2006-01-02"
	timeFormat         = "15:04:05.999999999"
	timetzFormat1      = "15:04:05.999999999-07:00:00"
	timetzFormat2      = "15:04:05.999999999-07:00"
	timetzFormat3      = "15:04:05.999999999-07"
	timestampFormat    = "2006-01-02 15:04:05.999999999"
	timestamptzFormat1 = "2006-01-02 15:04:05.999999999-07:00:00"
	timestamptzFormat2 = "2006-01-02 15:04:05.999999999-07:00"
	timestamptzFormat3 = "2006-01-02 15:04:05.999999999-07"
)

func ParseTime(s string) (time.Time, error) {
	l := len(s)
	if l >= len("2006-01-02 15:04:05") {
		switch s[10] {
		case ' ':
			if c := s[l-6]; c == '+' || c == '-' {
				return time.Parse(timestamptzFormat2, s)
			}
			if c := s[l-3]; c == '+' || c == '-' {
				return time.Parse(timestamptzFormat3, s)
			}
			if c := s[l-9]; c == '+' || c == '-' {
				return time.Parse(timestamptzFormat1, s)
			}
			return time.ParseInLocation(timestampFormat, s, time.Local)
		case 'T':
			return time.Parse(time.RFC3339Nano, s)
		}
	}
	if l >= len("15:04:05-07") {
		if c := s[l-6]; c == '+' || c == '-' {
			return time.Parse(timetzFormat2, s)
		}
		if c := s[l-3]; c == '+' || c == '-' {
			return time.Parse(timetzFormat3, s)
		}
		if c := s[l-9]; c == '+' || c == '-' {
			return time.Parse(timetzFormat1, s)
		}
	}
	if l < len("15:04:05") {
		return time.Time{}, fmt.Errorf("bun: can't parse time=%q", s)
	}
	if s[2] == ':' {
		return time.ParseInLocation(timeFormat, s, time.Local)
	}
	return time.ParseInLocation(dateFormat, s, time.Local)
}

func ToStruct(rows *sql.Rows, to interface{}) error {

	v := reflect.ValueOf(to)

	if v.Elem().Type().Kind() != reflect.Struct {

		return errors.New("Expect a struct")

	}

	scan_dest := []interface{}{}

	column_names, _ := rows.Columns()

	addr_by_column_name := map[string]interface{}{}

	for i := 0; i < v.Elem().NumField(); i++ {

		one_value := v.Elem().Field(i)

		column_name := v.Elem().Type().Field(i).Tag.Get("sql")

		if column_name == "" {
			column_name = one_value.Type().Name()

		}

		addr_by_column_name[column_name] = one_value.Addr().Interface()

	}

	for _, column_name := range column_names {

		scan_dest = append(scan_dest, addr_by_column_name[column_name])

	}

	return rows.Scan(scan_dest...)

}

func ScanStruct(rows *sql.Rows, dest interface{}) error {
	columns, err := rows.Columns() //获取查询结果集的列名
	if err != nil {
		return err
	}

	values := make([]interface{}, len(columns)) //创建与列数相同长度的切片，存放每行记录对应列的值
	for i := range values {
		var v interface{} = new(interface{}) //定义空接口类型变量v
		values[i] = &v                       //将指针传入values切片
	}

	scanArgs := []interface{}{&dest} //将目标结构体作为参数传入Scan函数
	for _, columnName := range columns {
		fieldValue := reflect.Indirect(reflect.ValueOf(&dest).Elem()).FieldByNameFunc(func(fieldName string) bool {
			return strings.EqualFold(columnName, fieldName) || strings.HasPrefix(strings.ToLower(columnName), strings.ToLower(fieldName)+".")
		})

		if !fieldValue.IsValid() {
			continue
		}

		valuePtr := fieldValue.Addr().Interface() //获取字段地址并转换为interface{}类型
		scanArgs = append(scanArgs, valuePtr)     //将字段地址添加到scanArgs切片中
	}

	if err := rows.Scan(scanArgs...); err != nil {
		return err
	}

	return nil
}

// 将数据库查询出的数据映射到struct
func (sb *SQLBuilder) ConvertMapToModel(s map[string]string, mStruct interface{}) error {
	model := reflect.Indirect(reflect.ValueOf(mStruct))
	modelType := model.Type()
	if model.Kind() != reflect.Struct {
		return errors.New("expected a pointer to a struct")
	}
	for i := 0; i < model.NumField(); i++ {
		fieldValue := model.Field(i)
		field := modelType.Field(i)
		err := sb.convertToModelElem(fieldValue, field, s)
		if err != nil {
			continue
		}
	}

	return nil
}

// 转换为Struct对象的元素（单个元素值设置）
func (sb *SQLBuilder) convertToModelElem(fieldValue reflect.Value, field reflect.StructField, s map[string]string) error {
	defer func() { //捕捉panic错误避免崩溃
		if r := recover(); r != nil {
			Text.Log("db_error").Error(fmt.Sprintf("convert to model element error:%s", r))
		}
	}()
	var sKey string
	fieldTag := field.Tag.Get("field")
	if fieldTag != "" {
		sKey = fieldTag
	} else {
		sKey = field.Name
	}
	tagType := field.Tag.Get("type")
	//tagTimeParse := field.Tag.Get("type") //时间解析函数
	if field.Type.Kind() == reflect.Struct && field.Type.String() != "time.Time" {
		return errors.New("不支持除Time类型外的其他类型的转换")
	}
	if dbValue, ok := s[sKey]; ok {
		//值转换
		var newValue interface{}
		switch field.Type.Kind() {
		case reflect.String:
			newValue = dbValue
		case reflect.Bool:
			newValue = dbValue == "1"
		case reflect.Int:
			x, err := strconv.Atoi(dbValue)
			if err != nil {
				return errors.New(fmt.Sprintf("字段[%s]不能转换为Int,错误：%s\n", dbValue, err.Error()))
			}
			newValue = x
		case reflect.Int8:
			x, err := strconv.ParseInt(dbValue, 10, 8)
			if err != nil {
				return errors.New(fmt.Sprintf("字段[%s]不能转换为Int8,错误：%s\n", dbValue, err.Error()))
			}
			newValue = int8(x)
		case reflect.Int16:
			x, err := strconv.ParseInt(dbValue, 10, 16)
			if err != nil {
				return errors.New(fmt.Sprintf("字段[%s]不能转换为Int16,错误：%s\n", dbValue, err.Error()))
			}
			newValue = int16(x)
		case reflect.Int32:
			x, err := strconv.ParseInt(dbValue, 10, 32)
			if err != nil {
				return errors.New(fmt.Sprintf("字段[%s]不能转换为Int32,错误：%s\n", dbValue, err.Error()))
			}
			newValue = int32(x)
		case reflect.Int64:
			x, err := strconv.ParseInt(dbValue, 10, 64)
			if err != nil {
				return errors.New(fmt.Sprintf("字段[%s]不能转换为Int64,错误：%s\n", dbValue, err.Error()))
			}
			newValue = x
		case reflect.Float32:
			x, err := strconv.ParseFloat(dbValue, 32)
			if err != nil {
				return errors.New(fmt.Sprintf("字段[%s]不能转换为Float32,错误：%s\n", dbValue, err.Error()))
			}
			newValue = float32(x)
		case reflect.Float64:
			x, err := strconv.ParseFloat(dbValue, 64)
			if err != nil {
				return errors.New(fmt.Sprintf("字段[%s]不能转换为Float64,错误：%s\n", dbValue, err.Error()))
			}
			newValue = x
		case reflect.Uint8:
			x, err := strconv.ParseUint(dbValue, 10, 8)
			if err != nil {
				return errors.New(fmt.Sprintf("字段[%s]不能转换为Uint,错误：%s\n", dbValue, err.Error()))
			}
			newValue = uint8(x)
		case reflect.Uint16:
			x, err := strconv.ParseUint(dbValue, 10, 16)
			if err != nil {
				return errors.New(fmt.Sprintf("字段[%s]不能转换为Uint,错误：%s\n", dbValue, err.Error()))
			}
			newValue = uint16(x)
		case reflect.Uint32:
			x, err := strconv.ParseUint(dbValue, 10, 32)
			if err != nil {
				return errors.New(fmt.Sprintf("字段[%s]不能转换为Uint,错误：%s\n", dbValue, err.Error()))
			}
			newValue = uint32(x)
		case reflect.Uint64:
			x, err := strconv.ParseUint(dbValue, 10, 64)
			if err != nil {
				return errors.New(fmt.Sprintf("字段[%s]不能转换为Uint,错误：%s\n", dbValue, err.Error()))
			}
			newValue = x
		case reflect.Struct: //解析时间

			var x time.Time
			var err error
			switch tagType {
			case "int":
				intTime, err1 := strconv.ParseInt(dbValue, 10, 64)
				if err1 == nil {
					x = time.Unix(intTime, 0)
				} else {
					return errors.New("时间格式不支持: " + dbValue)
				}
			default:
				x, err = ParseTime(dbValue)
				if err != nil {
					intTime, err := strconv.ParseInt(dbValue, 10, 64)
					if err == nil {
						x = time.Unix(intTime, 0)
					} else {
						return errors.New("时间格式不支持: " + dbValue)
					}

				}

			}
			newValue = x

		default:
			return errors.New("未发现可以支持的类型: " + reflect.TypeOf(newValue).String())
		}
		//fmt.Printf("%v:%v;type:%v\r\n", field.Name, newValue, reflect.TypeOf(newValue).String())
		fieldValue.Set(reflect.ValueOf(newValue)) //将字段写入struct
		return nil
	} else {
		return errors.New("未找到此字段值")
	}
}
*/
