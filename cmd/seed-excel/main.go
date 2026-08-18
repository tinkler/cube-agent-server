// seed-excel: 读预定义格式的 .xlsx → 合并成一张商品表 → 写 data/erp.db
//
// 输入文件: testdata/盘点商品表_canonical.xlsx
//   - 由 ai skill `xlsx-canonicalize` 生成(7 列固定 schema:行号/货号/商品名称/盘点数量/盘点金额/主供应商/主供应商名称)
//   - skill 负责清洗脏行/合计行/重复表头/列错位
//   - 本程序只信任这个预定义格式,不做启发式过滤或后置 fix
//
// 用法: go run ./cmd/seed-excel
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"

	_ "modernc.org/sqlite"
	"github.com/xuri/excelize/v2"
)

const (
	srcPath = "testdata/盘点商品表_canonical.xlsx"
	dbPath  = "data/erp.db"
	// 预定义 7 列 schema(与 xlsx-canonicalize skill 的 products-7col 完全一致)
	colRowNo     = 0
	colBarcode   = 1
	colName      = 2
	colQty       = 3
	colAmount    = 4
	colSuppID    = 5
	colSuppName  = 6
)

type Row struct {
	Sheet      string
	RowNo      string
	Barcode    string
	Name       string
	Qty        string
	Amount     string
	MainSuppID string
	MainSuppNm string
}

func main() {
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		log.Fatalf("remove old db: %v", err)
	}
	if err := os.MkdirAll("data", 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE products (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			row_no TEXT,
			barcode TEXT,
			name TEXT,
			qty REAL,
			amount REAL,
			main_supp_id TEXT,
			main_supp_name TEXT,
			src_sheet TEXT
		)
	`)
	if err != nil {
		log.Fatalf("create table: %v", err)
	}
	_, err = db.Exec(`CREATE INDEX idx_products_barcode ON products(barcode)`)
	if err != nil {
		log.Fatalf("create index: %v", err)
	}
	_, err = db.Exec(`CREATE INDEX idx_products_name ON products(name)`)
	if err != nil {
		log.Fatalf("create index: %v", err)
	}

	f, err := excelize.OpenFile(srcPath)
	if err != nil {
		log.Fatalf("open xlsx: %v", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	fmt.Printf("sheets: %d\n", len(sheets))

	// 探测:第一个 sheet 的 row 1 应该是 schema header
	headRow, err := readRow(f, sheets[0], 1)
	if err != nil {
		log.Fatalf("read header: %v", err)
	}
	fmt.Printf("headers (sheet 0): %v\n", headRow)

	total := 0
	inserted := 0
	for si, sheetName := range sheets {
		rows, err := f.GetRows(sheetName)
		if err != nil {
			log.Printf("read sheet %q: %v", sheetName, err)
			continue
		}
		// row 0 = schema header,data 从 row 1 开始
		sheetInserted := 0
		for ri := 1; ri < len(rows); ri++ {
			row := rows[ri]
			if len(row) < 7 {
				continue
			}
			total++
			_, err := db.Exec(`INSERT INTO products
				(row_no, barcode, name, qty, amount, main_supp_id, main_supp_name, src_sheet)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				nullable(row[colRowNo]), nullable(row[colBarcode]), nullable(row[colName]),
				parseFloat(row[colQty]), parseFloat(row[colAmount]),
				nullable(row[colSuppID]), nullable(row[colSuppName]), sheetName)
			if err != nil {
				log.Printf("insert R%d in %s: %v", ri, sheetName, err)
				continue
			}
			inserted++
			sheetInserted++
		}
		fmt.Printf("  [%d] %s: %d rows (inserted %d)\n", si, sheetName, len(rows)-1, sheetInserted)
	}

	// 统计:供应商分布
	r, err := db.Query(`SELECT main_supp_name, COUNT(*) AS n FROM products GROUP BY main_supp_name ORDER BY n DESC LIMIT 10`)
	if err != nil {
		log.Fatalf("stats: %v", err)
	}
	defer r.Close()
	fmt.Println("\n=== top 10 suppliers ===")
	for r.Next() {
		var name sql.NullString
		var n int
		_ = r.Scan(&name, &n)
		v := "<null>"
		if name.Valid {
			v = name.String
		}
		fmt.Printf("  %-30s %d\n", v, n)
	}

	fmt.Printf("\nsummary: total=%d inserted=%d\n", total, inserted)
	fmt.Printf("db: %s\n", dbPath)
}

// readRow 读 sheet 第 r 行(1-indexed,与 Excel 行号一致)。
func readRow(f *excelize.File, sheet string, r int) ([]string, error) {
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, err
	}
	if r < 1 || r > len(rows) {
		return nil, fmt.Errorf("row %d out of range (sheet has %d rows)", r, len(rows))
	}
	return rows[r-1], nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func parseFloat(s string) any {
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return f
}
