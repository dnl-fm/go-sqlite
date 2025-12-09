package query

import (
	"testing"
)

func TestBuild(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		params    map[string]any
		wantSQL   string
		wantArgs  int
		wantErr   bool
		errType   error
	}{
		{
			name:     "valid query with params",
			sql:      "SELECT * FROM users WHERE email = :email",
			params:   map[string]any{"email": "test@example.com"},
			wantSQL:  "SELECT * FROM users WHERE email = ?",
			wantArgs: 1,
			wantErr:  false,
		},
		{
			name:     "valid query with multiple params",
			sql:      "SELECT * FROM users WHERE email = :email AND active = :active",
			params:   map[string]any{"email": "test@example.com", "active": true},
			wantSQL:  "SELECT * FROM users WHERE email = ? AND active = ?",
			wantArgs: 2,
			wantErr:  false,
		},
		{
			name:     "valid query with repeated param",
			sql:      "SELECT * FROM users WHERE name = :name OR nickname = :name",
			params:   map[string]any{"name": "alice"},
			wantSQL:  "SELECT * FROM users WHERE name = ? OR nickname = ?",
			wantArgs: 2, // Same param used twice = 2 args
			wantErr:  false,
		},
		{
			name:    "empty SQL",
			sql:     "",
			params:  map[string]any{},
			wantErr: true,
			errType: ErrEmptySQL,
		},
		{
			name:    "missing param",
			sql:     "SELECT * FROM users WHERE id = :id",
			params:  map[string]any{},
			wantErr: true,
			errType: ErrMissingParam,
		},
		{
			name:    "extra param",
			sql:     "SELECT * FROM users WHERE id = :id",
			params:  map[string]any{"id": "123", "unused": "value"},
			wantErr: true,
			errType: ErrExtraParam,
		},
		{
			name:     "nil params with no placeholders",
			sql:      "SELECT * FROM users",
			params:   nil,
			wantSQL:  "SELECT * FROM users",
			wantArgs: 0,
			wantErr:  false,
		},
		{
			name:    "nil params with placeholders",
			sql:     "SELECT * FROM users WHERE id = :id",
			params:  nil,
			wantErr: true,
			errType: ErrMissingParam,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := Build(tt.sql, tt.params)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if q.SQL() != tt.wantSQL {
				t.Errorf("SQL() = %q, want %q", q.SQL(), tt.wantSQL)
			}

			if len(q.Args()) != tt.wantArgs {
				t.Errorf("Args() length = %d, want %d", len(q.Args()), tt.wantArgs)
			}

			if q.Original() != tt.sql {
				t.Errorf("Original() = %q, want %q", q.Original(), tt.sql)
			}
		})
	}
}

func TestBuild_ArgsOrder(t *testing.T) {
	q, err := Build(
		"INSERT INTO users (name, email) VALUES (:name, :email)",
		map[string]any{"name": "Alice", "email": "alice@test.com"},
	)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Args should be in order of appearance in SQL
	if q.SQL() != "INSERT INTO users (name, email) VALUES (?, ?)" {
		t.Errorf("SQL() = %q, unexpected", q.SQL())
	}

	args := q.Args()
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}

	// First arg should be name, second should be email
	if args[0] != "Alice" {
		t.Errorf("args[0] = %v, want 'Alice'", args[0])
	}
	if args[1] != "alice@test.com" {
		t.Errorf("args[1] = %v, want 'alice@test.com'", args[1])
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{
			name:    "valid simple query",
			sql:     "SELECT * FROM users",
			wantErr: false,
		},
		{
			name:    "empty SQL",
			sql:     "",
			wantErr: true,
		},
		{
			name:    "query with placeholders",
			sql:     "SELECT * FROM users WHERE id = :id",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := New(tt.sql)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if q.SQL() != tt.sql {
				t.Errorf("SQL() = %q, want %q", q.SQL(), tt.sql)
			}

			if len(q.Args()) != 0 {
				t.Errorf("Args() should be empty for New(), got %d args", len(q.Args()))
			}
		})
	}
}

func TestQuery_String(t *testing.T) {
	q, err := Build("SELECT * FROM users WHERE id = :id", map[string]any{"id": "123"})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if q.String() != q.SQL() {
		t.Errorf("String() should equal SQL()")
	}
}

func TestExtractParams(t *testing.T) {
	tests := []struct {
		sql    string
		expect []string
	}{
		{
			sql:    "SELECT * FROM users WHERE id = :id",
			expect: []string{"id"},
		},
		{
			sql:    "SELECT * FROM users WHERE email = :email AND active = :active",
			expect: []string{"active", "email"}, // sorted
		},
		{
			sql:    "SELECT * FROM users WHERE name = :name OR nickname = :name",
			expect: []string{"name"}, // deduplicated
		},
		{
			sql:    "SELECT * FROM users",
			expect: []string{},
		},
		{
			sql:    "SELECT * FROM users WHERE id = :_id AND name = :name123",
			expect: []string{"_id", "name123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			got := ExtractParams(tt.sql)

			if len(got) != len(tt.expect) {
				t.Errorf("ExtractParams() = %v, want %v", got, tt.expect)
				return
			}

			for i, v := range got {
				if v != tt.expect[i] {
					t.Errorf("ExtractParams()[%d] = %q, want %q", i, v, tt.expect[i])
				}
			}
		})
	}
}

func TestIsValidParamName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"id", true},
		{"user_id", true},
		{"_private", true},
		{"Name123", true},
		{"", false},
		{"123id", false},
		{"user-id", false},
		{"user.id", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidParamName(tt.name)
			if got != tt.valid {
				t.Errorf("IsValidParamName(%q) = %v, want %v", tt.name, got, tt.valid)
			}
		})
	}
}
