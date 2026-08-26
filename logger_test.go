package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestErrorLoggerBasic(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "buscatextual_log_test_*")
	if err != nil {
		t.Fatalf("falha ao criar temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger, err := newErrorLogger(tempDir, defaultMaxFileSize, defaultMaxRotatedFiles)
	if err != nil {
		t.Fatalf("falha ao instanciar logger: %v", err)
	}

	testErr := fmt.Errorf("falha de teste simulada no disco")
	logger.log("Erro de Leitura de Arquivo", testErr, []byte("goroutine 1 [running]:\nmain.TestErrorLoggerBasic()\n\td:/test.go:20"))
	logger.Close()

	activeLog := filepath.Join(tempDir, "log.log")
	content, err := os.ReadFile(activeLog)
	if err != nil {
		t.Fatalf("falha ao ler log.log: %v", err)
	}

	strContent := string(content)
	if !strings.Contains(strContent, "[ERROR] Erro de Leitura de Arquivo") {
		t.Errorf("conteudo nao possui mensagem de erro esperada: %s", strContent)
	}
	if !strings.Contains(strContent, "falha de teste simulada no disco") {
		t.Errorf("conteudo nao possui o erro: %s", strContent)
	}
	if !strings.Contains(strContent, "Stack Trace Completo:") || !strings.Contains(strContent, "main.TestErrorLoggerBasic") {
		t.Errorf("conteudo nao possui stack trace: %s", strContent)
	}
}

func TestErrorLoggerRotation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "buscatextual_log_rot_test_*")
	if err != nil {
		t.Fatalf("falha ao criar temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Tamanho max de 200 bytes para forçar rotação rápida
	logger, err := newErrorLogger(tempDir, 200, 3)
	if err != nil {
		t.Fatalf("falha ao instanciar logger: %v", err)
	}

	// Escreve mensagem grande para forçar rotação
	logger.log("Erro Rotacao 1", fmt.Errorf("erro grande para ultrapassar limite"), []byte("stack trace extenso com muitas linhas de informacao 1234567890"))
	logger.log("Erro Rotacao 2", fmt.Errorf("erro 2"), []byte("stack trace 2"))
	logger.Close()

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("falha ao ler tempDir: %v", err)
	}

	var rotatedCount int
	rotatedPattern := regexp.MustCompile(`^log_\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}-\d{3}(_\d+)?\.log$`)

	for _, entry := range entries {
		name := entry.Name()
		if name == "log.log" {
			continue
		}
		if rotatedPattern.MatchString(name) {
			rotatedCount++
		} else {
			t.Errorf("arquivo rotacionado fora do padrao esperado: %s", name)
		}
	}

	if rotatedCount < 1 {
		t.Errorf("esperava pelo menos 1 arquivo rotacionado, obteve %d", rotatedCount)
	}
}

func TestErrorLoggerMaxRotatedRetention(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "buscatextual_log_ret_test_*")
	if err != nil {
		t.Fatalf("falha ao criar temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	maxRotated := 3
	logger, err := newErrorLogger(tempDir, 100, maxRotated)
	if err != nil {
		t.Fatalf("falha ao instanciar logger: %v", err)
	}

	// Força múltiplas rotações (> 5 rotações)
	for i := 0; i < 8; i++ {
		logger.log(fmt.Sprintf("Erro Ciclo %d", i), fmt.Errorf("erro %d", i), []byte("stack trace detalhado para forcar rotacao de arquivo"))
		time.Sleep(10 * time.Millisecond) // Pequeno sleep para garantir timestamps distintos
	}
	logger.Close()

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("falha ao ler tempDir: %v", err)
	}

	var rotatedCount int
	for _, entry := range entries {
		name := entry.Name()
		if name != "log.log" && strings.HasPrefix(name, "log_") && strings.HasSuffix(name, ".log") {
			rotatedCount++
		}
	}

	if rotatedCount > maxRotated {
		t.Errorf("quantidade de arquivos rotacionados (%d) excedeu o limite configurado (%d)", rotatedCount, maxRotated)
	}
}

func TestErrorLoggerConcurrentZeroBlock(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "buscatextual_log_conc_test_*")
	if err != nil {
		t.Fatalf("falha ao criar temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger, err := newErrorLogger(tempDir, defaultMaxFileSize, 3)
	if err != nil {
		t.Fatalf("falha ao instanciar logger: %v", err)
	}

	var wg sync.WaitGroup
	workers := 50
	logsPerWorker := 100

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < logsPerWorker; j++ {
				logger.log(fmt.Sprintf("Worker %d Log %d", workerID, j), fmt.Errorf("erro concorrente"), []byte("stack trace"))
			}
		}(i)
	}

	wg.Wait()
	logger.Close()

	activeLog := filepath.Join(tempDir, "log.log")
	info, err := os.Stat(activeLog)
	if err != nil {
		t.Fatalf("falha ao obter stat de log.log: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("arquivo de log deveria conter dados gravados")
	}
}
