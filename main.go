package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type SearchMode int

const (
	ModeName SearchMode = iota + 1
	ModeContent
	ModeBoth
)

type Match struct {
	Path string
	Kind string
	Line int
	Text string
}

type SearchConfig struct {
	BaseDir         string
	Mode            SearchMode
	Terms           []string
	ExtensionFilter map[string]struct{}
	SearchAllFiles  bool
}

func main() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Busca Textual")
	baseDir := prompt(reader, "Informe o caminho da pasta para buscar: ")
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		exitWithMessage("Pasta nao informada.")
	}

	info, err := os.Stat(baseDir)
	if err != nil || !info.IsDir() {
		exitWithMessage("Pasta invalida ou inacessivel.")
	}

	mode := promptMode(reader)
	terms := promptTerms(reader)
	searchAllFiles, extensionFilter := promptExtensions(reader)
	if len(terms) == 0 {
		exitWithMessage("Nenhum termo de busca valido foi informado.")
	}

	fmt.Println()
	fmt.Println("Iniciando busca recursiva...")

	start := time.Now()
	config := SearchConfig{
		BaseDir:         baseDir,
		Mode:            mode,
		Terms:           terms,
		ExtensionFilter: extensionFilter,
		SearchAllFiles:  searchAllFiles,
	}

	matches, scannedFiles, err := runSearch(config)
	if err != nil {
		exitWithMessage(fmt.Sprintf("Erro durante a busca: %v", err))
	}

	reportPath, err := saveReport(matches)
	if err != nil {
		exitWithMessage(fmt.Sprintf("Nao foi possivel salvar o relatorio: %v", err))
	}

	fmt.Println()
	fmt.Println("Busca concluida.")
	fmt.Printf("Arquivos analisados: %d\n", scannedFiles)
	fmt.Printf("Ocorrencias encontradas: %d\n", len(matches))
	fmt.Printf("Tempo total: %s\n", time.Since(start).Round(time.Millisecond))
	fmt.Printf("Relatorio salvo em: %s\n", reportPath)
}

func prompt(reader *bufio.Reader, label string) string {
	fmt.Print(label)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

func promptMode(reader *bufio.Reader) SearchMode {
	for {
		fmt.Println()
		fmt.Println("Tipo de busca:")
		fmt.Println("1 - Nome do arquivo")
		fmt.Println("2 - Conteudo do arquivo")
		fmt.Println("3 - Ambas as opcoes")

		switch prompt(reader, "Escolha uma opcao (1/2/3): ") {
		case "1":
			return ModeName
		case "2":
			return ModeContent
		case "3":
			return ModeBoth
		default:
			fmt.Println("Opcao invalida.")
		}
	}
}

func promptTerms(reader *bufio.Reader) []string {
	raw := prompt(reader, "Informe os termos de busca separados por ';': ")
	parts := strings.Split(raw, ";")
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		term := strings.TrimSpace(part)
		if term != "" {
			terms = append(terms, term)
		}
	}
	return terms
}

func promptExtensions(reader *bufio.Reader) (bool, map[string]struct{}) {
	for {
		fmt.Println()
		fmt.Println("Filtro de extensao:")
		fmt.Println("1 - Buscar em todos os arquivos")
		fmt.Println("2 - Filtrar por extensoes")

		switch prompt(reader, "Escolha uma opcao (1/2): ") {
		case "1":
			return true, nil
		case "2":
			raw := prompt(reader, "Informe as extensoes separadas por ';' (ex.: .txt;.go;.json): ")
			filter := parseExtensions(raw)
			if len(filter) == 0 {
				fmt.Println("Nenhuma extensao valida foi informada.")
				continue
			}
			return false, filter
		default:
			fmt.Println("Opcao invalida.")
		}
	}
}

func parseExtensions(raw string) map[string]struct{} {
	parts := strings.Split(raw, ";")
	filter := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		ext := strings.TrimSpace(strings.ToLower(part))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		filter[ext] = struct{}{}
	}
	return filter
}

func runSearch(config SearchConfig) ([]Match, int, error) {
	workerCount := calculateWorkerCount(config.Mode)
	jobs := make(chan string, workerCount*4)
	results := make(chan []Match, workerCount)
	errorsCh := make(chan string, workerCount)

	var scannedFiles atomic.Int64
	var workers sync.WaitGroup
	var collector sync.WaitGroup
	var matches []Match
	var matchMu sync.Mutex

	collector.Add(1)
	go func() {
		defer collector.Done()
		for batch := range results {
			if len(batch) == 0 {
				continue
			}
			matchMu.Lock()
			matches = append(matches, batch...)
			matchMu.Unlock()
		}
	}()

	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for path := range jobs {
				fileMatches, err := processFile(path, config.Mode, config.Terms)
				if err != nil {
					errorsCh <- fmt.Sprintf("Falha ao ler arquivo %s: %v", path, err)
					continue
				}
				if len(fileMatches) > 0 {
					results <- fileMatches
				}
			}
		}()
	}

	progressDone := make(chan struct{})
	go showProgress(&scannedFiles, workerCount, progressDone)

	walkErr := filepath.WalkDir(config.BaseDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			fmt.Printf("\nIgnorando caminho com erro: %s (%v)\n", path, walkErr)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !shouldProcessFile(path, config) {
			return nil
		}

		scannedFiles.Add(1)
		jobs <- path
		return nil
	})

	close(jobs)
	workers.Wait()
	close(results)
	close(errorsCh)
	collector.Wait()
	close(progressDone)

	for msg := range errorsCh {
		fmt.Printf("\n%s\n", msg)
	}

	totalScanned := int(scannedFiles.Load())
	fmt.Printf("\rAnalisando arquivos... %d | workers: %d\n", totalScanned, workerCount)
	return matches, totalScanned, walkErr
}

func shouldProcessFile(path string, config SearchConfig) bool {
	if config.SearchAllFiles {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := config.ExtensionFilter[ext]
	return ok
}

func processFile(path string, mode SearchMode, terms []string) ([]Match, error) {
	var matches []Match

	if mode == ModeName || mode == ModeBoth {
		nameMatches := searchInFileName(path, terms)
		matches = append(matches, nameMatches...)
	}

	if mode == ModeContent || mode == ModeBoth {
		contentMatches, err := searchInFile(path, terms)
		if err != nil {
			return matches, err
		}
		matches = append(matches, contentMatches...)
	}

	return matches, nil
}

func searchInFileName(path string, terms []string) []Match {
	fileName := strings.ToLower(filepath.Base(path))
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	for _, term := range terms {
		if strings.Contains(fileName, strings.ToLower(term)) {
			return []Match{{
				Path: absPath,
				Kind: "nome",
			}}
		}
	}

	return nil
}

func searchInFile(path string, terms []string) ([]Match, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Aumenta o buffer para lidar melhor com linhas grandes.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var matches []Match
	lineNumber := 0
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		lowerLine := strings.ToLower(line)

		for _, term := range terms {
			if strings.Contains(lowerLine, strings.ToLower(term)) {
				matches = append(matches, Match{
					Path: absPath,
					Kind: "conteudo",
					Line: lineNumber,
					Text: line,
				})
				break
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return matches, nil
}

func saveReport(matches []Match) (string, error) {
	fileName := fmt.Sprintf("resultado_busca_%s.txt", time.Now().Format("20060102_150405"))
	filePath, err := filepath.Abs(fileName)
	if err != nil {
		return "", err
	}

	file, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	if len(matches) == 0 {
		if _, err := writer.WriteString("Nenhuma ocorrencia encontrada.\n"); err != nil {
			return "", err
		}
		return filePath, nil
	}

	for _, match := range matches {
		line := fmt.Sprintf("Arquivo: %s", match.Path)
		if match.Kind == "conteudo" {
			line = fmt.Sprintf("%s | Linha: %d | Trecho: %s", line, match.Line, match.Text)
		} else {
			line = fmt.Sprintf("%s | Correspondencia no nome do arquivo", line)
		}

		if _, err := writer.WriteString(line + "\n"); err != nil {
			return "", err
		}
	}

	return filePath, nil
}

func calculateWorkerCount(mode SearchMode) int {
	cpuCount := runtime.NumCPU()
	if cpuCount < 1 {
		return 1
	}

	// Busca em conteudo costuma ser mais limitada pelo disco do que pela CPU.
	if mode == ModeContent || mode == ModeBoth {
		if cpuCount <= 2 {
			return cpuCount
		}
		if cpuCount > 6 {
			return 6
		}
		return cpuCount
	}

	if cpuCount > 12 {
		return 12
	}
	return cpuCount
}

func showProgress(scannedFiles *atomic.Int64, workerCount int, done <-chan struct{}) {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			fmt.Printf("\rAnalisando arquivos... %d | workers: %d", scannedFiles.Load(), workerCount)
		}
	}
}

func exitWithMessage(message string) {
	fmt.Println(message)
	os.Exit(1)
}
