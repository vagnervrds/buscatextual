package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// resolvePath tenta localizar um arquivo ou pasta no sistema operacional.
// Se o caminho não existir diretamente (por exemplo, unidade não mapeada ou desconectada como E:),
// ele tenta resolver em outras unidades ativas (C:, D:, U:, Z:, etc.) mantendo o caminho relativo
// ou considerando o compartilhamento de rede montado como raiz.
func resolvePath(targetPath string) (string, bool) {
	if strings.TrimSpace(targetPath) == "" {
		return "", false
	}
	clean := filepath.Clean(targetPath)

	// 1. Verifica se o caminho ou a pasta pai existe diretamente
	if _, err := os.Stat(clean); err == nil {
		return clean, true
	}
	if _, err := os.Stat(filepath.Dir(clean)); err == nil {
		return clean, true
	}

	if runtime.GOOS != "windows" {
		return clean, false
	}

	// 2. Extrai volume e caminho relativo (ex: "E:" e "TRABALHO\2025\...")
	vol := filepath.VolumeName(clean)
	if vol == "" {
		return clean, false
	}

	rel := strings.TrimPrefix(clean, vol)
	rel = strings.TrimPrefix(rel, `\`)
	rel = strings.TrimPrefix(rel, `/`)
	if rel == "" {
		return clean, false
	}

	// Prepara variantes do caminho relativo:
	// Exemplo:
	// rel = "TRABALHO\2025\pasta\arquivo.txt"
	// Variante 1: "TRABALHO\2025\pasta\arquivo.txt"
	// Variante 2: "2025\pasta\arquivo.txt" (caso o compartilhamento de rede seja a pasta TRABALHO montada na raiz da unidade, ex: Z:\)
	var relVariants []string
	relVariants = append(relVariants, rel)

	parts := strings.Split(rel, `\`)
	if len(parts) > 1 {
		relVariants = append(relVariants, strings.Join(parts[1:], `\`))
	}
	if len(parts) > 2 {
		relVariants = append(relVariants, strings.Join(parts[2:], `\`))
	}

	// 3. Monta lista de outras unidades a testar
	var drivesToCheck []string
	for r := 'A'; r <= 'Z'; r++ {
		driveLetter := string(r) + ":"
		if strings.EqualFold(driveLetter, vol) {
			continue // Unidade original já falhou no teste direto
		}
		drivesToCheck = append(drivesToCheck, driveLetter+`\`)
	}

	// 4. Executa a checagem concorrente nas unidades com timeout para evitar travamentos
	// em compartilhamentos de rede desconectados ou lentos
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()

	resultChan := make(chan string, 1)
	var wg sync.WaitGroup

	for _, root := range drivesToCheck {
		wg.Add(1)
		go func(driveRoot string) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				return
			default:
			}

			// Checa cada variante para esta unidade
			for _, variant := range relVariants {
				candidate := filepath.Join(driveRoot, variant)

				// Testa arquivo
				if _, err := os.Stat(candidate); err == nil {
					select {
					case resultChan <- candidate:
						cancel()
					default:
					}
					return
				}

				// Testa diretório pai
				if _, err := os.Stat(filepath.Dir(candidate)); err == nil {
					select {
					case resultChan <- candidate:
						cancel()
					default:
					}
					return
				}
			}
		}(root)
	}

	// Goroutine para fechar o canal quando todos terminarem caso nada seja encontrado
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	foundPath, ok := <-resultChan
	if ok && foundPath != "" {
		return foundPath, true
	}

	return clean, false
}
