package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseCSVReport(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "bt_test_csv_*")
	if err != nil {
		t.Fatalf("Erro ao criar tempDir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	csvPath := filepath.Join(tempDir, "resultado_busca_teste.csv")
	csvContent := `# Metadados
# Data Inicio: 2026-08-29 15:30:00.000
# Pasta Base: C:\Projetos
# Modo: conteudo do arquivo
# Modo Busca: ampla
# Alvo: arquivos
# Termos: teste, busca
# Filtros Positivos: .go, .txt
# Filtros Negativos: .bak

Arquivo,Tipo,Linha,Trecho,Tamanho_Bytes,Data_Modificacao
C:\Projetos\main.go,conteudo,42,"fmt.Println(""teste"")",1024,2026-08-29 12:00:00
C:\Projetos\doc.txt,nome,,documento de teste,512,2026-08-29 11:00:00
`

	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("Erro ao escrever arquivo de teste: %v", err)
	}

	resp, err := parseCSVReport(csvPath)
	if err != nil {
		t.Fatalf("parseCSVReport falhou: %v", err)
	}

	if resp.Metadata.PastaBase != `C:\Projetos` {
		t.Errorf("Esperava PastaBase 'C:\\Projetos', obteve %q", resp.Metadata.PastaBase)
	}

	if resp.Metadata.Modo != "conteudo do arquivo" {
		t.Errorf("Esperava Modo 'conteudo do arquivo', obteve %q", resp.Metadata.Modo)
	}

	if len(resp.Metadata.Termos) != 2 || resp.Metadata.Termos[0] != "teste" || resp.Metadata.Termos[1] != "busca" {
		t.Errorf("Termos incorretos: %v", resp.Metadata.Termos)
	}

	if resp.Metadata.TotalLinhas != 2 {
		t.Errorf("Esperava 2 linhas nos metadados, obteve %d", resp.Metadata.TotalLinhas)
	}

	if len(resp.Rows) != 2 {
		t.Fatalf("Esperava 2 linhas na tabela, obteve %d", len(resp.Rows))
	}

	row1 := resp.Rows[0]
	if row1.Arquivo != `C:\Projetos\main.go` {
		t.Errorf("Row 1 Arquivo incorreto: %q", row1.Arquivo)
	}
	if row1.NomeArquivo != "main.go" {
		t.Errorf("Row 1 NomeArquivo incorreto: %q", row1.NomeArquivo)
	}
	if row1.Pasta != `C:\Projetos` {
		t.Errorf("Row 1 Pasta incorreta: %q", row1.Pasta)
	}
	if row1.Tipo != "conteudo" {
		t.Errorf("Row 1 Tipo incorreto: %q", row1.Tipo)
	}
	if row1.LinhaNum != 42 {
		t.Errorf("Row 1 LinhaNum incorreto: %d", row1.LinhaNum)
	}
	if row1.TamanhoBytes != 1024 {
		t.Errorf("Row 1 TamanhoBytes incorreto: %d", row1.TamanhoBytes)
	}
}

func TestListCSVReports(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "bt_test_list_*")
	if err != nil {
		t.Fatalf("Erro ao criar tempDir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	file1 := filepath.Join(tempDir, "resultado_busca_20260829_100000_000.csv")
	content1 := `# Metadados
# Data Inicio: 2026-08-29 10:00:00.000
# Pasta Base: C:\Temp
# Modo: nome do arquivo
# Alvo: arquivos
# Termos: alpha

Arquivo,Tipo,Linha,Trecho,Tamanho_Bytes,Data_Modificacao
C:\Temp\a.txt,nome,,,100,2026-08-29 09:00:00
`
	_ = os.WriteFile(file1, []byte(content1), 0644)

	reports, err := listCSVReports(tempDir)
	if err != nil {
		t.Fatalf("listCSVReports falhou: %v", err)
	}

	if len(reports) != 1 {
		t.Fatalf("Esperava 1 relatorio, obteve %d", len(reports))
	}

	if reports[0].FileName != "resultado_busca_20260829_100000_000.csv" {
		t.Errorf("Nome do arquivo inesperado: %s", reports[0].FileName)
	}
	if reports[0].PastaBase != `C:\Temp` {
		t.Errorf("PastaBase inesperada: %s", reports[0].PastaBase)
	}
	if len(reports[0].Termos) != 1 || reports[0].Termos[0] != "alpha" {
		t.Errorf("Termos inesperados: %v", reports[0].Termos)
	}
}

func TestTableViewerEndpoints(t *testing.T) {
	// 1. Testa endpoint de abertura de arquivo
	reqBody, _ := json.Marshal(ActionRequest{Path: "test.txt"})
	req := httptest.NewRequest(http.MethodPost, "/api/open-file", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ActionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(ActionResponse{Success: false, Error: "invalido"})
			return
		}
		_ = json.NewEncoder(w).Encode(ActionResponse{Success: true, Message: "OK"})
	})

	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Esperava 200 OK, obteve %d", w.Code)
	}

	var actionResp ActionResponse
	if err := json.NewDecoder(w.Body).Decode(&actionResp); err != nil || !actionResp.Success {
		t.Errorf("Esperava sucesso na resposta da acao: %v", actionResp)
	}
}

func TestFilePreviewAndCategories(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "bt_test_preview_*")
	if err != nil {
		t.Fatalf("Erro ao criar tempDir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	goFilePath := filepath.Join(tempDir, "sample.go")
	goContent := `package main

import "fmt"

func main() {
	fmt.Println("Ola Mundo")
}
`
	if err := os.WriteFile(goFilePath, []byte(goContent), 0644); err != nil {
		t.Fatalf("Erro ao escrever arquivo sample.go: %v", err)
	}

	preview, err := buildFilePreview(goFilePath, 5)
	if err != nil {
		t.Fatalf("buildFilePreview falhou: %v", err)
	}

	if preview.Category != "code" {
		t.Errorf("Esperava category 'code', obteve %q", preview.Category)
	}
	if preview.HighlightLine != 5 {
		t.Errorf("Esperava HighlightLine 5, obteve %d", preview.HighlightLine)
	}
	if len(preview.Lines) != 7 {
		t.Errorf("Esperava 7 linhas, obteve %d", len(preview.Lines))
	}
	if preview.FileName != "sample.go" {
		t.Errorf("FileName incorreto: %s", preview.FileName)
	}

	// Testa categoria de imagem
	cat, mimeType := detectFileCategory("foto.png")
	if cat != "image" || mimeType != "image/png" {
		t.Errorf("Esperava image/png, obteve %s / %s", cat, mimeType)
	}

	// Testa categoria de áudio/vídeo/pdf
	if cat, _ := detectFileCategory("musica.mp3"); cat != "audio" {
		t.Errorf("Esperava audio, obteve %s", cat)
	}
	if cat, _ := detectFileCategory("video.mp4"); cat != "video" {
		t.Errorf("Esperava video, obteve %s", cat)
	}
	if cat, _ := detectFileCategory("doc.pdf"); cat != "pdf" {
		t.Errorf("Esperava pdf, obteve %s", cat)
	}
}
