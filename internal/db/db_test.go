package db_test

import (
	"path/filepath"
	"testing"

	"github.com/FelipeSrutkoske/Skatt/internal/db"
	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

func TestOpenAppliesMigrationsAndSeedsPeople(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "finance.db")

	conn, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open falhou: %v", err)
	}
	defer conn.Close()

	var fk int
	if err := conn.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys falhou: %v", err)
	}
	if fk != 1 {
		t.Fatalf("esperado foreign_keys=1, obtido %d", fk)
	}

	expectedTables := []string{
		"people",
		"statements",
		"transactions",
		"transaction_splits",
		"assignment_rules",
		"payments",
		"receipts",
		"settings",
	}
	for _, tbl := range expectedTables {
		var name string
		err := conn.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl).Scan(&name)
		if err != nil {
			t.Fatalf("tabela %s nao encontrada: %v", tbl, err)
		}
	}

	var peopleCount int
	if err := conn.QueryRow("SELECT COUNT(*) FROM people").Scan(&peopleCount); err != nil {
		t.Fatalf("contar people falhou: %v", err)
	}
	if peopleCount != 2 {
		t.Fatalf("esperado 2 pessoas iniciais, obtido %d", peopleCount)
	}

	var selfName string
	if err := conn.QueryRow("SELECT name FROM people WHERE is_self = 1").Scan(&selfName); err != nil {
		t.Fatalf("buscar is_self falhou: %v", err)
	}
	if selfName != "Eu" {
		t.Fatalf("esperado Eu como is_self, obtido %q", selfName)
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("fechar conexao falhou: %v", err)
	}

	conn2, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("reabrir banco falhou: %v", err)
	}
	defer conn2.Close()

	if err := conn2.QueryRow("SELECT COUNT(*) FROM people").Scan(&peopleCount); err != nil {
		t.Fatalf("recontar people falhou: %v", err)
	}
	if peopleCount != 2 {
		t.Fatalf("seed nao idempotente: esperado 2, obtido %d", peopleCount)
	}
}

func TestParseBRLCentsAndFormatBRL(t *testing.T) {
	cases := []struct {
		raw      string
		expected int64
	}{
		{"1.234,56", 123456},
		{"-R$ 45,90", -4590},
		{"R$ -45,90", -4590},
		{"120.50", 12050},
		{"0,00", 0},
		{"50", 5000},
		{"-19.90", -1990},
	}

	for _, tc := range cases {
		got, err := domain.ParseBRLCents(tc.raw)
		if err != nil {
			t.Fatalf("ParseBRLCents(%q) retornou erro: %v", tc.raw, err)
		}
		if got != tc.expected {
			t.Fatalf("ParseBRLCents(%q) = %d, esperado %d", tc.raw, got, tc.expected)
		}
	}

	if _, err := domain.ParseBRLCents("abc"); err == nil {
		t.Fatalf("esperado erro para valor invalido")
	}

	if got := domain.FormatBRL(123456); got != "R$ 1.234,56" {
		t.Fatalf("FormatBRL(123456) = %q, esperado R$ 1.234,56", got)
	}
	if got := domain.FormatBRL(-4590); got != "-R$ 45,90" {
		t.Fatalf("FormatBRL(-4590) = %q, esperado -R$ 45,90", got)
	}
}
