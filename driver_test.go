/*
Copyright 2013 Tamás Gulácsi

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package goracle

import (
	"database/sql"
	"flag"
	"io"
	"io/ioutil"
	"strings"
	"testing"
	"time"

	"github.com/tgulacsi/go/loghlp/tsthlp"
	"gopkg.in/goracle.v1/oracle"
)

var fDsn = flag.String("dsn", "", "Oracle DSN")

func Test_open_cursors(t *testing.T) {
	conn := getConnection(t)
	defer conn.Close()
	var before, after int

	// This needs "GRANT SELECT ANY DICTIONARY TO test"
	// or at least "GRANT SELECT ON v_$mystat TO test".
	if err := conn.QueryRow("select value from v$mystat where statistic#=4").Scan(&before); err != nil {
		t.Skip(err)
	}
	rounds := 100
	for i := 0; i < rounds; i++ {
		func() {
			stmt, err := conn.Prepare("SELECT 1 FROM user_objects WHERE ROWNUM < 100")
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close()
			rows, err := stmt.Query()
			if err != nil {
				t.Errorf("ERROR SELECT: %v", err)
				return
			}
			defer rows.Close()
			j := 0
			for rows.Next() {
				j++
			}
			t.Logf("%d objects, error=%v", j, rows.Err())
		}()
	}
	if err := conn.QueryRow("select value from v$mystat where statistic#=4").Scan(&after); err != nil {
		t.Skip(err)
	}
	if after-before >= rounds {
		t.Errorf("ERROR before=%d after=%d, awaited less than %d increment!", before, after, rounds)
		return
	}
	t.Logf("before=%d after=%d", before, after)
}

func TestNull(t *testing.T) {
	conn := getConnection(t)
	defer conn.Close()
	var (
		err error
		str string
		dt  time.Time
	)
	qry := "SELECT '' FROM DUAL"
	row := conn.QueryRow(qry)
	if err = row.Scan(&str); err != nil {
		t.Fatalf("0. error with %q test: %s", qry, err)
	}
	t.Logf("0. %q result: %#v (%T)", qry, str, str)

	qry = "SELECT TO_DATE(NULL) FROM DUAL"
	row = conn.QueryRow(qry)
	if err = row.Scan(&dt); err != nil {
		t.Fatalf("1. error with %q test: %s", qry, err)
	}
	t.Logf("1. %q result: %#v (%T)", qry, dt, dt)
}

func TestSimple(t *testing.T) {
	conn := getConnection(t)
	defer conn.Close()

	var (
		err error
		dst interface{}
	)
	for i, qry := range []string{
		"SELECT ROWNUM FROM DUAL",
		"SELECT 1234567890 FROM DUAL",
		"SELECT LOG(10, 2) FROM DUAL",
		"SELECT 'árvíztűrő tükörfúrógép' FROM DUAL",
		"SELECT HEXTORAW('00') FROM DUAL",
		"SELECT TO_DATE('2006-05-04 15:07:08', 'YYYY-MM-DD HH24:MI:SS') FROM DUAL",
		"SELECT NULL FROM DUAL",
		"SELECT TO_CLOB('árvíztűrő tükörfúrógép') FROM DUAL",
	} {
		row := conn.QueryRow(qry)
		if err = row.Scan(&dst); err != nil {
			t.Fatalf("%d. error with %q test: %s", i, qry, err)
		}
		t.Logf("%d. %q result: %#v", i, qry, dst)
		if strings.Index(qry, " TO_CLOB(") >= 0 {
			var b []byte
			var e error
			if true {
				r := dst.(io.Reader)
				b, e = ioutil.ReadAll(r)
			} else {
				clob := dst.(*oracle.ExternalLobVar)
				b, e = clob.ReadAll()
			}
			if e != nil {
				t.Errorf("error reading clob (%v): %s", dst, e)
			} else {
				t.Logf("clob=%s", b)
			}
		}
	}

	qry := "SELECT rn, CHR(rn) FROM (SELECT ROWNUM rn FROM all_objects WHERE ROWNUM < 256)"
	rows, err := conn.Query(qry)
	if err != nil {
		t.Errorf("error with multirow test, query %q: %s", qry, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Errorf("error getting columns for %q: %s", qry, err)
	}
	t.Logf("columns for %q: %v", qry, cols)
	var (
		num int
		str string
	)
	for rows.Next() {
		if err = rows.Scan(&num, &str); err != nil {
			t.Errorf("error scanning row: %s", err)
		}
		//t.Logf("%d=%q", num, str)
	}
}

func TestNumber(t *testing.T) {
	conn := getConnection(t)
	defer conn.Close()

	oldDebug := IsDebug
	IsDebug = true
	if !oldDebug {
		defer func() { IsDebug = false }()
	}

	var (
		err, errF error
		into      int64
		intoF     float64
	)
	for i, tst := range []struct {
		in   string
		want int64
	}{
		{"1", 1},
		{"1234567890", 1234567890},
	} {
		into, intoF, errF = 0, 0, nil
		qry := "SELECT " + tst.in + " FROM DUAL"
		row := conn.QueryRow(qry)
		if err = row.Scan(&into); err != nil {
			row = conn.QueryRow(qry)
			if errF = row.Scan(&intoF); errF != nil {
				t.Errorf("%d. error with %q testF: %s", i, qry, err)
				continue
			}
			t.Logf("%d. %q result: %#v", i, qry, intoF)
			if intoF != float64(tst.want) {
				t.Errorf("%d. got %#v want %#v", i, intoF, float64(tst.want))
			}
			continue
		}
		t.Logf("%d. %q result: %#v", i, qry, into)
		if into != tst.want {
			t.Errorf("%d. got %#v want %#v", i, into, tst.want)
		}
	}
}

func TestSelectBind(t *testing.T) {
	conn := getConnection(t)
	defer conn.Close()

	tbl := `(SELECT 1 id FROM DUAL
             UNION ALL SELECT 2 FROM DUAL
             UNION ALL SELECT 1234567890123 FROM DUAL)`

	qry := "SELECT * FROM " + tbl
	rows, err := conn.Query(qry)
	if err != nil {
		t.Errorf("get all rows: %v", err)
		return
	}

	var id int64
	i := 1
	for rows.Next() {
		if err = rows.Scan(&id); err != nil {
			t.Errorf("%d. error: %v", i, err)
		}
		t.Logf("%d. %d", i, id)
		i++
	}
	if err = rows.Err(); err != nil {
		t.Errorf("rows error: %v", err)
	}

	qry = "SELECT id FROM " + tbl + " WHERE id = :1"
	if err = conn.QueryRow(qry, 1234567890123).Scan(&id); err != nil {
		t.Errorf("bind: %v", err)
		return
	}
	t.Logf("bind: %d", id)
}

// TestClob
func TestClob(t *testing.T) {
	conn := getConnection(t)
	defer conn.Close()
	text := "abcdefghijkl"
	stmt, err := conn.Prepare("SELECT TO_CLOB('" + text + "') FROM DUAL")
	if err != nil {
		t.Errorf("error preparing query1: %v", err)
		t.FailNow()
	}
	defer stmt.Close()

	var clob *oracle.ExternalLobVar
	if err = stmt.QueryRow().Scan(&clob); err != nil {
		t.Errorf("Error scanning clob: %v", err)
	}
	defer clob.Close()
	t.Logf("clob: %v", clob)

	got, err := clob.ReadAll()
	if err != nil {
		t.Errorf("error reading clob: %v", err)
		t.FailNow()
	}
	if string(got) != text {
		t.Errorf("clob: got %q, awaited %q", got, text)
	}
}

func TestPrepared(t *testing.T) {
	conn := getConnection(t)
	defer conn.Close()
	stmt, err := conn.Prepare("SELECT ? FROM DUAL")
	if err != nil {
		t.Errorf("error preparing query: %v", err)
		t.FailNow()
	}
	defer stmt.Close()
	rows, err := stmt.Query("a")
	if err != nil {
		t.Errorf("error executing query: %s", err)
		t.FailNow()
	}
	defer rows.Close()
}

func TestNULL(t *testing.T) {
	conn := getConnection(t)
	defer conn.Close()

	rows, err := conn.Query(`
		SELECT dt
		  FROM (SELECT TO_DATE('', 'YYYY-MM-DD') dt FROM DUAL
				UNION ALL SELECT SYSDATE FROM DUAL
				UNION ALL SELECT NULL FROM DUAL)`)
	if err != nil {
		t.Errorf("error executing the query: %v", err)
		t.FailNow()
	}
	var dt time.Time
	i := 0
	for rows.Next() {
		if err = rows.Scan(&dt); err != nil {
			t.Errorf("error fetching row %d: %v", i+1, err)
			break
		}
		if i == 1 && dt.IsZero() {
			t.Errorf("second row is zero: %#v", dt)
		}
		if i != 1 && !dt.IsZero() {
			t.Errorf("other row is not zero: %#v", dt)
		}
		i++
	}
}

var testDB *sql.DB

func getConnection(t *testing.T) *sql.DB {
	var err error
	if testDB != nil && testDB.Ping() == nil {
		return testDB
	}
	flag.Parse()
	Log.SetHandler(tsthlp.TestHandler(t))
	oracle.Log.SetHandler(tsthlp.TestHandler(t))
	if testDB, err = sql.Open("goracle", *fDsn); err != nil {
		t.Fatalf("error connecting to %q: %s", *fDsn, err)
	}
	return testDB
}
