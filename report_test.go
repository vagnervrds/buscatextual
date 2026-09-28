package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

func TestSortModeDB(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test_sortmode.db")
	oldDB := db
	defer func() { db = oldDB }()

	var err error
	db, err = initTestDB(tmpDB)
	if err != nil {
		t.Fatalf("Erro ao inicializar DB de teste: %v", err)
	}
	defer db.Close()

	// Padrão deve ser SortByFolder (1)
	sortDefault := getSortMode()
	if sortDefault != SortByFolder {
		t.Errorf("SortMode padrão esperado SortByFolder (1), obtido %d", sortDefault)
	}

	// Testar salvar e recuperar cada modo válido
	validModes := []SortMode{
		SortBySizeDesc,
		SortBySizeAsc,
		SortByDateDesc,
		SortByDateAsc,
		SortByFolder,
	}

	for _, m := range validModes {
		if err := saveSortMode(m); err != nil {
			t.Fatalf("Erro ao salvar sortMode %d: %v", m, err)
		}
		if saved := getSortMode(); saved != m {
			t.Errorf("SortMode esperado %d, obtido %d", m, saved)
		}
	}

	// Testar valor inválido
	if err := saveSortMode(0); err == nil {
		t.Errorf("Esperava erro ao salvar modo de ordenacao 0, mas obteve nil")
	}
	if err := saveSortMode(6); err == nil {
		t.Errorf("Esperava erro ao salvar modo de ordenacao 6, mas obteve nil")
	}
}

func TestPromptSortMode(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test_prompt_sort.db")
	oldDB := db
	defer func() { db = oldDB }()

	var err error
	db, err = initTestDB(tmpDB)
	if err != nil {
		t.Fatalf("Erro ao inicializar DB de teste: %v", err)
	}
	defer db.Close()

	// 1. Simular escolha da opção "4" (SortByDateDesc)
	r1 := bufio.NewReader(strings.NewReader("4\n"))
	chosen1 := promptSortMode(r1)
	if chosen1 != SortByDateDesc {
		t.Errorf("Esperava SortByDateDesc (4), obtido %d", chosen1)
	}
	if saved := getSortMode(); saved != SortByDateDesc {
		t.Errorf("Deveria ter persistido SortByDateDesc (4) no DB, mas obteve %d", saved)
	}

	// 2. Simular pressionar Enter (deve manter o padrão atual, que agora é SortByDateDesc)
	r2 := bufio.NewReader(strings.NewReader("\n"))
	chosen2 := promptSortMode(r2)
	if chosen2 != SortByDateDesc {
		t.Errorf("Ao pressionar Enter, esperava manter padrão SortByDateDesc (4), obtido %d", chosen2)
	}

	// 3. Simular escolha da opção "2" (SortBySizeDesc)
	r3 := bufio.NewReader(strings.NewReader("2\n"))
	chosen3 := promptSortMode(r3)
	if chosen3 != SortBySizeDesc {
		t.Errorf("Esperava SortBySizeDesc (2), obtido %d", chosen3)
	}
	if saved := getSortMode(); saved != SortBySizeDesc {
		t.Errorf("Deveria ter persistido SortBySizeDesc (2) no DB, mas obteve %d", saved)
	}

	// 4. Enter novamente - agora deve ser SortBySizeDesc
	r4 := bufio.NewReader(strings.NewReader("\n"))
	chosen4 := promptSortMode(r4)
	if chosen4 != SortBySizeDesc {
		t.Errorf("Ao pressionar Enter, esperava manter padrão SortBySizeDesc (2), obtido %d", chosen4)
	}
}

func TestReportFormatDB(t *testing.T) {
	// Cria arquivo de DB temporário para o teste
	tmpDB := filepath.Join(t.TempDir(), "test_buscatextual.db")
	oldDB := db
	defer func() { db = oldDB }()

	var err error
	db, err = initTestDB(tmpDB)
	if err != nil {
		t.Fatalf("Erro ao inicializar DB de teste: %v", err)
	}
	defer db.Close()

	// Formato padrão deve ser "csv"
	fmtDefault := getReportFormat()
	if fmtDefault != "csv" {
		t.Errorf("Formato padrão esperado 'csv', obtido '%s'", fmtDefault)
	}

	// Salvar JSON
	if err := saveReportFormat("json"); err != nil {
		t.Fatalf("Erro ao salvar formato json: %v", err)
	}
	if fmtSaved := getReportFormat(); fmtSaved != "json" {
		t.Errorf("Formato esperado 'json', obtido '%s'", fmtSaved)
	}

	// Salvar TOML
	if err := saveReportFormat("toml"); err != nil {
		t.Fatalf("Erro ao salvar formato toml: %v", err)
	}
	if fmtSaved := getReportFormat(); fmtSaved != "toml" {
		t.Errorf("Formato esperado 'toml', obtido '%s'", fmtSaved)
	}

	// Salvar CSV
	if err := saveReportFormat("csv"); err != nil {
		t.Fatalf("Erro ao salvar formato csv: %v", err)
	}
	if fmtSaved := getReportFormat(); fmtSaved != "csv" {
		t.Errorf("Formato esperado 'csv', obtido '%s'", fmtSaved)
	}
}

func TestReportGeneration(t *testing.T) {
	config := SearchConfig{
		BaseDir:        "C:\\Teste",
		Mode:           ModeBoth,
		Terms:          []string{"teste"},
		PositiveFilter: []string{"txt"},
		NegativeFilter: []string{"tmp"},
		TargetType:     TargetFiles,
		SortMode:       SortByFolder,
	}

	matches := []Match{
		{
			Path:    "C:\\Teste\\arquivo1.txt",
			Kind:    "conteudo",
			Line:    42,
			Text:    "linha com teste",
			Size:    1024,
			ModTime: time.Now(),
		},
		{
			Path:    "C:\\Teste\\teste.txt",
			Kind:    "nome",
			Size:    2048,
			ModTime: time.Now(),
		},
	}

	formats := []string{"csv", "json", "toml"}

	for _, fmtStr := range formats {
		cfg := config
		cfg.ReportFormat = fmtStr

		reporter, err := createReport(cfg)
		if err != nil {
			t.Fatalf("Erro ao criar relatório %s: %v", fmtStr, err)
		}

		if err := reporter.Append(matches); err != nil {
			t.Fatalf("Erro ao adicionar matches no relatório %s: %v", fmtStr, err)
		}

		if err := reporter.Close(); err != nil {
			t.Fatalf("Erro ao fechar relatório %s: %v", fmtStr, err)
		}

		content, err := os.ReadFile(reporter.Path)
		if err != nil {
			t.Fatalf("Erro ao ler relatório gerado %s: %v", reporter.Path, err)
		}

		strContent := string(content)
		if !strings.HasSuffix(reporter.Path, "."+fmtStr) {
			t.Errorf("Extensão do arquivo incorreta: %s", reporter.Path)
		}

		if len(strContent) == 0 {
			t.Errorf("Conteúdo do relatório %s veio vazio", fmtStr)
		}

		// Limpa o arquivo de relatório criado no teste
		_ = os.Remove(reporter.Path)
	}
}

func initTestDB(dbPath string) (*bbolt.DB, error) {
	d, err := bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 500 * time.Millisecond})
	if err != nil {
		return nil, err
	}
	err = d.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte("Files")); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte("DiskConfig")); err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("AppConfig"))
		return err
	})
	return d, err
}
