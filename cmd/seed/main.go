// Seed script: 建 demo SQLite 数据库,插入测试数据
// 用法: go run ./cmd/seed
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	// 删旧库
	_ = os.Remove("./data/demo.db")
	_ = os.MkdirAll("./data", 0o755)

	db, err := sql.Open("sqlite", "./data/demo.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// 1. 建表
	stmts := []string{
		`CREATE TABLE orders (
			id INTEGER PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			customer_id INTEGER,
			amount REAL,
			status TEXT,
			created_at TEXT
		)`,
		`CREATE TABLE products (
			id INTEGER PRIMARY KEY,
			name TEXT,
			category TEXT
		)`,
		`CREATE TABLE order_items (
			id INTEGER PRIMARY KEY,
			order_id INTEGER,
			product_id INTEGER,
			quantity INTEGER,
			unit_price REAL
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			log.Fatalf("create table: %v", err)
		}
	}

	// 2. 插数据
	insertOrder, _ := db.Prepare(`INSERT INTO orders(id, tenant_id, customer_id, amount, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`)
	orders := []struct {
		ID         int
		Tenant     string
		CustomerID int
		Amount     float64
		Status     string
		CreatedAt  string
	}{
		{1, "acme", 1, 100.0, "paid", "2026-08-10 10:00:00"},
		{2, "acme", 2, 200.0, "paid", "2026-08-10 11:00:00"},
		{3, "acme", 1, 50.0, "pending", "2026-08-11 09:00:00"},
		{4, "acme", 3, 300.0, "shipped", "2026-08-12 14:00:00"},
		{5, "acme", 2, 75.0, "cancelled", "2026-08-13 16:00:00"},
		{6, "acme", 4, 150.0, "paid", "2026-08-15 12:00:00"},
		{7, "acme", 1, 80.0, "created", "2026-08-15 13:00:00"},
		{8, "acme", 5, 220.0, "done", "2026-08-15 14:00:00"},
		{9, "globex", 6, 90.0, "paid", "2026-08-15 15:00:00"},
		{10, "acme", 2, 60.0, "paid", "2026-08-15 16:00:00"},
	}
	for _, o := range orders {
		if _, err := insertOrder.Exec(o.ID, o.Tenant, o.CustomerID, o.Amount, o.Status, o.CreatedAt); err != nil {
			log.Fatalf("insert order: %v", err)
		}
	}
	insertOrder.Close()

	insertProd, _ := db.Prepare(`INSERT INTO products(id, name, category) VALUES (?, ?, ?)`)
	products := []struct {
		id       int
		name     string
		category string
	}{
		{1, "可乐", "饮料"},
		{2, "薯片", "零食"},
		{3, "洗衣液", "日化"},
		{4, "面包", "烘焙"},
	}
	for _, p := range products {
		if _, err := insertProd.Exec(p.id, p.name, p.category); err != nil {
			log.Fatalf("insert product: %v", err)
		}
	}
	insertProd.Close()

	fmt.Println("seed done: ./data/demo.db")
	fmt.Println("  - 10 orders (1 tenant=globex, 9 tenant=acme)")
	fmt.Println("  - 4 products")
}
