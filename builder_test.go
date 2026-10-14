package mysql

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

func testModel() *DbModel {
	db := &DbModel{}
	db.pool = &sync.Pool{New: func() interface{} { return NewSQLBuilder(db, false) }}
	return db
}

func checkSQL(t *testing.T, sb *SQLBuilder, wantSQL string, wantParams ...interface{}) {
	t.Helper()
	got, params, err := sb.ShowSql()
	if err != nil {
		t.Fatalf("ShowSql: %v", err)
	}
	if got != wantSQL {
		t.Fatalf("sql\n got: %s\nwant: %s", got, wantSQL)
	}
	if len(params) != len(wantParams) || (len(params) > 0 && !reflect.DeepEqual(params, wantParams)) {
		t.Fatalf("params\n got: %#v\nwant: %#v", params, wantParams)
	}
}

func TestSelectParamOrderIndependentOfCallOrder(t *testing.T) {
	sb := testModel().Orm()
	defer sb.Free()
	sb.Table("a").Where("a.x=?", 1).Having("cnt>?", 3).GroupBy("a.id").JoinRaw("JOIN b ON b.id=a.id AND b.k=?", 2).Having("s<?", 4)
	checkSQL(t, sb, "SELECT * FROM a JOIN b ON b.id=a.id AND b.k=? WHERE a.x=? GROUP BY a.id HAVING cnt>? AND s<?", 2, 1, 3, 4)
}

func TestUpdateSetAppends(t *testing.T) {
	sb := testModel().Orm()
	defer sb.Free()
	sb.Table("u").QBU().Where("id=?", 9).Set("a=?", 1).Set("b=b+1").Sets(map[string]any{"d": 4, "c": 3})
	checkSQL(t, sb, "UPDATE u SET a=?,b=b+1,c=?,d=? WHERE id=?", 1, 3, 4, 9)
}

func TestWhereInEmptyAndPointers(t *testing.T) {
	m := testModel()
	sb := m.Orm()
	defer sb.Free()

	sb.Table("t").QBD().WhereIn("id")
	checkSQL(t, sb, "DELETE FROM t WHERE 1=0")
	sb.ReSet()

	sb.Table("t").QBD().WhereIn("id", []int64{})
	checkSQL(t, sb, "DELETE FROM t WHERE 1=0")
	sb.ReSet()

	sb.Table("t").Where("k=?", 1).WhereNotIn("id", []int{})
	checkSQL(t, sb, "SELECT * FROM t WHERE k=? AND 1=1", 1)
	sb.ReSet()

	ids := []int{1, 2}
	sb.Table("t").WhereIn("id", &ids)
	checkSQL(t, sb, "SELECT * FROM t WHERE id IN (?,?)", 1, 2)
	sb.ReSet()

	one := 5
	p := &one
	sb.Table("t").WhereIn("id", p)
	checkSQL(t, sb, "SELECT * FROM t WHERE id IN (?)", p)
}

func TestInsertDkuOrder(t *testing.T) {
	sb := testModel().Orm()
	defer sb.Free()
	sb.Table("t").QBI().OnDku("v=v+?", 7).AddOne(map[string]any{"b": 2, "a": 1})
	checkSQL(t, sb, "INSERT INTO t (`a`,`b`) VALUES (?,?)  ON DUPLICATE KEY UPDATE v=v+?", 1, 2, 7)

	sb.ReSet()
	sb.Table("t").QBI(MethodIgnoreInsert).OnDku("v=?", 1).AddOne(map[string]any{"a": 1})
	checkSQL(t, sb, "INSERT IGNORE INTO t (`a`) VALUES (?) ", 1)
}

func TestAddMoreMapsAlignedByKey(t *testing.T) {
	rows := []map[string]any{}
	for i := 0; i < 20; i++ {
		rows = append(rows, map[string]any{"a": i, "b": i * 10, "c": i * 100})
	}
	sb := testModel().Orm()
	defer sb.Free()
	sb.Table("t").QBI().AddMore(rows)
	_, params, err := sb.ShowSql()
	if err != nil {
		t.Fatal(err)
	}
	if sb._insert[:len("(`a`,`b`,`c`) VALUES (?,?,?),")] != "(`a`,`b`,`c`) VALUES (?,?,?)," {
		t.Fatalf("insert: %s", sb._insert)
	}
	for i := 0; i < 20; i++ {
		if params[i*3] != i || params[i*3+1] != i*10 || params[i*3+2] != i*100 {
			t.Fatalf("row %d misaligned: %v", i, params[i*3:i*3+3])
		}
	}
}

type tRow struct {
	ID      int64     `field:"id" auto:"1"`
	Name    string    `field:"name"`
	Balance int64     `field:"balance" empty:"1"`
	Score   int64     `field:"score"`
	At      time.Time `field:"at"`
	Day     time.Time `field:"day" type:"date"`
	secret  string    `field:"secret"`
	Other   string
}

func TestStructInsertUpdate(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	sb := testModel().Orm()
	defer sb.Free()

	sb.Table("t").QBI().AddOne(&tRow{Name: "n", At: at, secret: "x"})
	checkSQL(t, sb, "INSERT INTO t (`name`,`balance`,`at`) VALUES (?,?,?) ", "n", int64(0), at)
	sb.ReSet()

	sb.Table("t").QBU().Sets(tRow{Day: at}).Where("id=?", 1)
	checkSQL(t, sb, "UPDATE t SET balance=?,day=? WHERE id=?", int64(0), "2026-01-02", 1)
	sb.ReSet()

	sb.Table("t").QBI().AddMore([]*tRow{{Name: "a", At: at}, {Name: "b"}})
	checkSQL(t, sb, "INSERT INTO t (`name`,`balance`,`score`,`at`,`day`) VALUES (?,?,?,?,?),(?,?,?,?,?) ",
		"a", int64(0), int64(0), at, nil,
		"b", int64(0), int64(0), nil, nil)
}

func TestExecDoesNotReturnBuilderToPool(t *testing.T) {
	sb := testModel().Orm()
	if err := sb.Table("t").Where("id=?", 1).Find(&tRow{}); err == nil {
		t.Fatal("expected error without db")
	}
	if sb.inPool {
		t.Fatal("Find must not return the builder to the pool; the owner calls Free")
	}
	if sb._where != "" || sb._whereParam != nil {
		t.Fatal("builder state should be reset after Find")
	}
	if _, err := sb.Table("t").Set("a=?", 1).Update(); err == nil {
		t.Fatal("expected error without db")
	}
	if sb.inPool {
		t.Fatal("Update must not return the builder to the pool")
	}
	sb.Free()
	if !sb.inPool {
		t.Fatal("Free should return the builder to the pool")
	}
	sb.Free()
}

func TestInt64ToString(t *testing.T) {
	cases := map[int64]string{
		0:                    "0",
		-1:                   "-1",
		12345678901:          "12345678901",
		-9223372036854775808: "-9223372036854775808",
	}
	for n, want := range cases {
		if got := Int64ToString(n); got != want {
			t.Fatalf("%d -> %s", n, got)
		}
	}
}
