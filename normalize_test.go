package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNormalizeText(t *testing.T) {
	tests := []struct {
		input    string
		mode     string
		expected string
	}{
		{"Vitória", "ampla", "vitoria"},
		{"vitoria", "ampla", "vitoria"},
		{"VITÓRIA", "ampla", "vitoria"},
		{"São Paulo", "ampla", "sao paulo"},
		{"SAO PAULO", "ampla", "sao paulo"},
		{"AÇÃO E REAÇÃO", "ampla", "acao e reacao"},
		{"coração", "ampla", "coracao"},
		{"Parâmetro", "ampla", "parametro"},
		{"relatório_2026.pdf", "ampla", "relatorio_2026.pdf"},
		{"ascii_only_test", "ampla", "ascii_only_test"},
		{"ASCII_UPPER", "ampla", "ascii_upper"},
		// Modo Exato
		{"Vitória", "exata", "Vitória"},
		{"vitoria", "exata", "vitoria"},
		{"São Paulo", "exata", "São Paulo"},
	}

	for _, tt := range tests {
		got := NormalizeText(tt.input, tt.mode)
		if got != tt.expected {
			t.Errorf("NormalizeText(%q, %q) = %q; esperado %q", tt.input, tt.mode, got, tt.expected)
		}
	}
}

func TestMatchingModeDB(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test_matchmode.db")
	oldDB := db
	defer func() { db = oldDB }()

	var err error
	db, err = initTestDB(tmpDB)
	if err != nil {
		t.Fatalf("Erro ao inicializar DB de teste: %v", err)
	}
	defer db.Close()

	// Padrão deve ser "ampla"
	modeDefault := getMatchingMode()
	if modeDefault != "ampla" {
		t.Errorf("Modo padrão esperado 'ampla', obtido '%s'", modeDefault)
	}

	// Salvar modo "exata"
	if err := saveMatchingMode("exata"); err != nil {
		t.Fatalf("Erro ao salvar modo exata: %v", err)
	}
	if modeSaved := getMatchingMode(); modeSaved != "exata" {
		t.Errorf("Modo esperado 'exata', obtido '%s'", modeSaved)
	}

	// Salvar modo "ampla"
	if err := saveMatchingMode("ampla"); err != nil {
		t.Fatalf("Erro ao salvar modo ampla: %v", err)
	}
	if modeSaved := getMatchingMode(); modeSaved != "ampla" {
		t.Errorf("Modo esperado 'ampla', obtido '%s'", modeSaved)
	}

	// Erro em modo inválido
	if err := saveMatchingMode("invalido"); err == nil {
		t.Errorf("Esperava erro ao salvar modo inválido, mas obteve nil")
	}
}

func TestSearchInDBNormalized(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test_search_db.db")
	oldDB := db
	defer func() { db = oldDB }()

	var err error
	db, err = initTestDB(tmpDB)
	if err != nil {
		t.Fatalf("Erro ao inicializar DB de teste: %v", err)
	}
	defer db.Close()

	// Insere arquivos simulados
	tx, err := db.Begin(true)
	if err != nil {
		t.Fatalf("Erro ao abrir tx: %v", err)
	}
	b := tx.Bucket([]byte("Files"))
	_ = putIndexHelper(b, "C:\\Projetos\\Vitória_Relatório.txt", 100, time.Now().Format(time.RFC3339Nano))
	_ = putIndexHelper(b, "C:\\Projetos\\Sao_Paulo_Dados.csv", 200, time.Now().Format(time.RFC3339Nano))
	_ = tx.Commit()

	// Busca com termo sem acento "vitoria" no modo ampla
	matchesAmpla := searchFilenamesInDB([]string{"vitoria"}, nil, nil, "ampla")
	if len(matchesAmpla) != 1 {
		t.Fatalf("Modo ampla: esperava 1 match para 'vitoria', encontrou %d", len(matchesAmpla))
	}
	if !strings.Contains(matchesAmpla[0].Path, "Vitória") {
		t.Errorf("Path retornado inesperado: %s", matchesAmpla[0].Path)
	}

	// Busca com termo com acento "relatório" no modo ampla
	matchesRel := searchFilenamesInDB([]string{"relatorio"}, nil, nil, "ampla")
	if len(matchesRel) != 1 {
		t.Fatalf("Modo ampla: esperava 1 match para 'relatorio', encontrou %d", len(matchesRel))
	}

	// Busca com termo sem acento "vitoria" no modo exata -> Não deve encontrar "Vitória"
	matchesExata := searchFilenamesInDB([]string{"vitoria"}, nil, nil, "exata")
	if len(matchesExata) != 0 {
		t.Fatalf("Modo exata: esperava 0 matches para 'vitoria' em 'Vitória_Relatório.txt', encontrou %d", len(matchesExata))
	}

	// Busca exata exata -> Deve encontrar
	matchesExataMatch := searchFilenamesInDB([]string{"Vitória"}, nil, nil, "exata")
	if len(matchesExataMatch) != 1 {
		t.Fatalf("Modo exata: esperava 1 match para 'Vitória', encontrou %d", len(matchesExataMatch))
	}
}

func TestSearchInFileNormalized(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "arquivo_teste.txt")

	content := "Linha 1: Introdução ao sistema de Busca\nLinha 2: Cidade de Vitória no Espírito Santo\nLinha 3: Processamento e Ação concluídos\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Erro ao criar arquivo de teste: %v", err)
	}

	// Modo Ampla: termo "vitoria" encontra "Vitória"
	matchesAmpla, err := searchInFile(filePath, []string{"vitoria"}, "ampla")
	if err != nil {
		t.Fatalf("Erro ao buscar no arquivo: %v", err)
	}
	if len(matchesAmpla) != 1 {
		t.Fatalf("Esperava 1 match para 'vitoria', obteve %d", len(matchesAmpla))
	}
	if matchesAmpla[0].Line != 2 {
		t.Errorf("Esperava linha 2, obteve %d", matchesAmpla[0].Line)
	}

	// Modo Ampla: termo "introducao" encontra "Introdução"
	matchesIntro, err := searchInFile(filePath, []string{"introducao"}, "ampla")
	if err != nil {
		t.Fatalf("Erro ao buscar no arquivo: %v", err)
	}
	if len(matchesIntro) != 1 {
		t.Fatalf("Esperava 1 match para 'introducao', obteve %d", len(matchesIntro))
	}

	// Modo Exata: termo "vitoria" não deve encontrar "Vitória"
	matchesExata, err := searchInFile(filePath, []string{"vitoria"}, "exata")
	if err != nil {
		t.Fatalf("Erro ao buscar no arquivo: %v", err)
	}
	if len(matchesExata) != 0 {
		t.Fatalf("Modo exata não deveria encontrar 'vitoria' em 'Vitória', obteve %d", len(matchesExata))
	}
}

func TestNormalizeTextConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	inputs := []string{
		"Vitória Lopes",
		"São Paulo - Relatório Financeiro",
		"Ação e Reação em Acentuação",
		"Parâmetro de Configuração",
		"coração e emoção",
		"relatório_2026.pdf",
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				str := inputs[(id+j)%len(inputs)]
				norm := NormalizeText(str, "ampla")
				if strings.ContainsAny(norm, "áéíóúãõâêîôûçÁÉÍÓÚÃÕÂÊÎÔÛÇ") {
					t.Errorf("Caracter acentuado encontrado em texto normalizado: %s", norm)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestHighlightTermsNormalized(t *testing.T) {
	text := "Bem-vindo a Vitória capital"
	highlighted := highlightTerms(text, []string{"vitoria"}, "ampla")

	// Deve conter as cores ANSI envolvendo exatamente "Vitória"
	expectedSub := "\033[1;31mVitória\033[0m"
	if !strings.Contains(highlighted, expectedSub) {
		t.Errorf("Destaque incorreto. Esperava conter %q, obteve %q", expectedSub, highlighted)
	}
}

func BenchmarkNormalizeTextASCII(b *testing.B) {
	str := "c:\\users\\admin\\pycharmprojects\\pythonproject\\buscatextual\\main.go"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NormalizeText(str, "ampla")
	}
}

func BenchmarkNormalizeTextUnicode(b *testing.B) {
	str := "c:\\Relatórios_Vitória\\Ação_São_Paulo_Informações.txt"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NormalizeText(str, "ampla")
	}
}
