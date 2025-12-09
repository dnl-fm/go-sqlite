package query

import (
	"testing"
)

func BenchmarkBuild_Simple(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := Build(
			"SELECT * FROM users WHERE id = :id",
			map[string]any{"id": "123"},
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_MultipleParams(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := Build(
			"SELECT * FROM users WHERE email = :email AND active = :active AND role = :role",
			map[string]any{
				"email":  "test@example.com",
				"active": true,
				"role":   "admin",
			},
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_ManyParams(b *testing.B) {
	b.ReportAllocs()

	params := map[string]any{
		"p1": "value1", "p2": "value2", "p3": "value3",
		"p4": "value4", "p5": "value5", "p6": "value6",
		"p7": "value7", "p8": "value8", "p9": "value9",
		"p10": "value10",
	}

	sql := `INSERT INTO table (c1, c2, c3, c4, c5, c6, c7, c8, c9, c10) 
	        VALUES (:p1, :p2, :p3, :p4, :p5, :p6, :p7, :p8, :p9, :p10)`

	for i := 0; i < b.N; i++ {
		_, err := Build(sql, params)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_RepeatedParams(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := Build(
			"SELECT * FROM users WHERE name = :search OR email LIKE :search OR bio LIKE :search",
			map[string]any{"search": "test"},
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNew(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := New("SELECT * FROM users")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExtractParams(b *testing.B) {
	b.ReportAllocs()

	sql := "SELECT * FROM users WHERE email = :email AND active = :active AND role = :role"

	for i := 0; i < b.N; i++ {
		ExtractParams(sql)
	}
}

func BenchmarkQuery_SQL(b *testing.B) {
	q, _ := Build(
		"SELECT * FROM users WHERE id = :id",
		map[string]any{"id": "123"},
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = q.SQL()
	}
}

func BenchmarkQuery_Args(b *testing.B) {
	q, _ := Build(
		"SELECT * FROM users WHERE id = :id AND active = :active",
		map[string]any{"id": "123", "active": true},
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = q.Args()
	}
}
