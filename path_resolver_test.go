package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolvePathDirect(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "bt_test_resolve_*")
	if err != nil {
		t.Fatalf("Erro ao criar tempDir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testFile := filepath.Join(tempDir, "sample.txt")
	if err := os.WriteFile(testFile, []byte("conteudo"), 0644); err != nil {
		t.Fatalf("Erro ao escrever arquivo: %v", err)
	}

	// 1. Arquivo existente diretamente
	resolved, ok := resolvePath(testFile)
	if !ok || resolved != testFile {
		t.Errorf("Esperava resolver %s diretamente, obteve %s (ok=%v)", testFile, resolved, ok)
	}

	// 2. Pasta existente diretamente
	resolvedDir, ok := resolvePath(tempDir)
	if !ok || resolvedDir != tempDir {
		t.Errorf("Esperava resolver pasta %s diretamente, obteve %s (ok=%v)", tempDir, resolvedDir, ok)
	}

	// 3. Caminho vazio
	_, ok = resolvePath("")
	if ok {
		t.Errorf("Caminho vazio não deveria ser resolvido")
	}
}

func TestResolvePathWindowsSimulated(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Teste específico para Windows")
	}

	// Se existir o arquivo do usuário no disco Z:
	target := `E:\TRABALHO\2025\2025-11-29 ensaio externo Gabrieli Germano\captureone\selecao.db`
	expected := `Z:\2025\2025-11-29 ensaio externo Gabrieli Germano\captureone\selecao.db`

	if _, err := os.Stat(expected); err == nil {
		resolved, ok := resolvePath(target)
		if !ok {
			t.Errorf("Deveria ter resolvido %s para %s na unidade Z:", target, expected)
		}
		if resolved != expected {
			t.Errorf("Esperava %s, mas obteve %s", expected, resolved)
		}
	}
}
