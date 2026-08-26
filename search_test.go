package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSearchWithBinariesAndLongLines(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "buscatextual_search_test_*")
	if err != nil {
		t.Fatalf("Erro ao criar temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 1. Arquivo de texto comum com termo no conteudo
	textFilePath := filepath.Join(tempDir, "documento.txt")
	if err := os.WriteFile(textFilePath, []byte("Linha 1: introducao\nLinha 2: termo_secreto_busca aqui\nLinha 3: fim"), 0644); err != nil {
		t.Fatalf("Erro ao escrever arquivo de texto: %v", err)
	}

	// 2. Arquivo binario com byte nulo
	binFilePath := filepath.Join(tempDir, "arquivo_termo_secreto_busca.bin")
	binContent := append([]byte{0x00, 0x01, 0x02, 0xFF}, []byte("termo_secreto_busca")...)
	if err := os.WriteFile(binFilePath, binContent, 0644); err != nil {
		t.Fatalf("Erro ao escrever arquivo binario: %v", err)
	}

	// 3. Arquivo com linha muito longa (> 2MB)
	longLinePath := filepath.Join(tempDir, "longline.txt")
	longContent := strings.Repeat("A", 2*1024*1024) + " termo_secreto_busca " + strings.Repeat("B", 100) + "\n"
	if err := os.WriteFile(longLinePath, []byte(longContent), 0644); err != nil {
		t.Fatalf("Erro ao escrever arquivo de linha longa: %v", err)
	}

	// 4. Criacao de 30 arquivos dummy para testar concorrencia
	for i := 0; i < 30; i++ {
		p := filepath.Join(tempDir, fmt.Sprintf("extra_%d.log", i))
		_ = os.WriteFile(p, []byte(fmt.Sprintf("conteudo %d sem nada relevante\n", i)), 0644)
	}

	// Executa busca no disco (ModeBoth)
	config := SearchConfig{
		BaseDir:      tempDir,
		Mode:         ModeBoth,
		Terms:        []string{"termo_secreto_busca"},
		ReportFormat: "csv",
		MatchingMode: "ampla",
	}

	result, err := runSearch(config)
	if err != nil {
		t.Fatalf("runSearch falhou: %v", err)
	}

	if result.ScannedFiles < 33 {
		t.Errorf("Esperava pelo menos 33 arquivos analisados, obteve %d", result.ScannedFiles)
	}

	// Deve encontrar:
	// - documento.txt (conteudo)
	// - arquivo_termo_secreto_busca.bin (nome, ignorando conteudo por ser binario)
	// - longline.txt (conteudo na linha longa)
	var foundText, foundBinName, foundLongLine bool
	for _, m := range result.Matches {
		if strings.HasSuffix(m.Path, "documento.txt") && m.Kind == "conteudo" {
			foundText = true
		}
		if strings.HasSuffix(m.Path, "arquivo_termo_secreto_busca.bin") && m.Kind == "nome" {
			foundBinName = true
		}
		if strings.HasSuffix(m.Path, "longline.txt") && m.Kind == "conteudo" {
			foundLongLine = true
		}
	}

	if !foundText {
		t.Errorf("Nao encontrou documento.txt no conteudo")
	}
	if !foundBinName {
		t.Errorf("Nao encontrou correspondencia de nome em arquivo binario")
	}
	if !foundLongLine {
		t.Errorf("Nao encontrou correspondencia em linha longa sem quebrar o parser")
	}

	// Limpa arquivo de relatorio gerado
	if result.ReportPath != "" {
		_ = os.Remove(result.ReportPath)
	}
}

func TestCalculateWorkerCountWithDiskBenchmark(t *testing.T) {
	// Testa quando nao ha benchmark
	w1 := calculateWorkerCount(ModeContent, "C:\\teste_pasta")
	if w1 <= 0 {
		t.Errorf("Worker count deve ser positivo, obteve %d", w1)
	}

	w2 := calculateWorkerCount(ModeName, "C:\\teste_pasta")
	if w2 <= 0 {
		t.Errorf("Worker count deve ser positivo, obteve %d", w2)
	}
}

func TestRunSearchStressDeadlockImmunity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "buscatextual_stress_*")
	if err != nil {
		t.Fatalf("Erro ao criar temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Cria 100 arquivos em multiplos subdiretorios
	for dirIdx := 0; dirIdx < 5; dirIdx++ {
		sub := filepath.Join(tempDir, fmt.Sprintf("subdir_%d", dirIdx))
		_ = os.MkdirAll(sub, 0755)
		for fIdx := 0; fIdx < 20; fIdx++ {
			filePath := filepath.Join(sub, fmt.Sprintf("file_%d.txt", fIdx))
			content := fmt.Sprintf("Linha teste no arquivo %d\n", fIdx)
			if fIdx == 10 {
				content += "alvo_especifico_stress\n"
			}
			_ = os.WriteFile(filePath, []byte(content), 0644)
		}
	}

	config := SearchConfig{
		BaseDir:      tempDir,
		Mode:         ModeBoth,
		Terms:        []string{"alvo_especifico_stress"},
		ReportFormat: "json",
		MatchingMode: "ampla",
	}

	result, err := runSearch(config)
	if err != nil {
		t.Fatalf("runSearch falhou no teste de estresse: %v", err)
	}

	if len(result.Matches) != 5 {
		t.Errorf("Esperava 5 ocorrencias encontradas, obteve %d", len(result.Matches))
	}

	if result.ReportPath != "" {
		_ = os.Remove(result.ReportPath)
	}
}
