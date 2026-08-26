package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDecodeJSONWithBOM(t *testing.T) {
	// JSON com UTF-8 BOM (0xEF, 0xBB, 0xBF)
	rawWithBOM := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"build": 29}`)...)

	cleanBytes := bytes.TrimPrefix(rawWithBOM, []byte{0xEF, 0xBB, 0xBF})

	var info RemoteBuildInfo
	if err := json.Unmarshal(cleanBytes, &info); err != nil {
		t.Fatalf("Erro ao decodificar JSON com BOM: %v", err)
	}

	if info.Build != 29 {
		t.Errorf("Build esperado 29, obteve %d", info.Build)
	}
}
