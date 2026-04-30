package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"syscall"
	"unsafe"
)

var BuildVersion string = "dev"

func setConsoleTitle(title string) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleTitleProc := kernel32.NewProc("SetConsoleTitleW")
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	setConsoleTitleProc.Call(uintptr(unsafe.Pointer(titlePtr)))
}

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

type SearchResult struct {
	Matches      []Match
	ScannedFiles int
	ReportPath   string
}

func main() {
	setConsoleTitle("BuscaTextual " + BuildVersion)
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Busca Textual - Build:", BuildVersion)
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

	result, err := runSearch(config)
	if err != nil {
		exitWithMessage(fmt.Sprintf("Erro durante a busca: %v", err))
	}

	fmt.Println()
	fmt.Println("Busca concluida.")
	fmt.Printf("Arquivos analisados: %d\n", result.ScannedFiles)
	fmt.Printf("Ocorrencias encontradas: %d\n", len(result.Matches))
	fmt.Printf("Tempo total: %s\n", time.Since(start).Round(time.Millisecond))
	fmt.Printf("Relatorio salvo em: %s\n", result.ReportPath)
	showReport(result.ReportPath)
	waitForExit(reader)
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

func runSearch(config SearchConfig) (SearchResult, error) {
	workerCount := calculateWorkerCount(config.Mode)
	jobs := make(chan string, workerCount*4)
	results := make(chan []Match, workerCount)
	errorsCh := make(chan string, workerCount)
	reporter, err := createReport(config)
	if err != nil {
		return SearchResult{}, err
	}
	defer reporter.Close()

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
			if err := reporter.Append(batch); err != nil {
				errorsCh <- fmt.Sprintf("Falha ao salvar no relatorio: %v", err)
			}
			showFound(batch)
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
	if len(matches) == 0 {
		if err := reporter.WriteNoMatches(); err != nil {
			return SearchResult{}, err
		}
	}
	return SearchResult{
		Matches:      matches,
		ScannedFiles: totalScanned,
		ReportPath:   reporter.Path,
	}, walkErr
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

type ReportWriter struct {
	File     *os.File
	Writer   *bufio.Writer
	Path     string
	mu       sync.Mutex
	hasMatch bool
}

func createReport(config SearchConfig) (*ReportWriter, error) {
	reportDir := "resultados_busca"
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return nil, err
	}

	timestamp := strings.ReplaceAll(time.Now().Format("20060102_150405.000"), ".", "_")
	fileName := fmt.Sprintf("resultado_busca_%s.txt", timestamp)
	filePath, err := filepath.Abs(filepath.Join(reportDir, fileName))
	if err != nil {
		return nil, err
	}

	file, err := os.Create(filePath)
	if err != nil {
		return nil, err
	}

	writer := bufio.NewWriter(file)
	reporter := &ReportWriter{
		File:   file,
		Writer: writer,
		Path:   filePath,
	}

	header := []string{
		fmt.Sprintf("Busca iniciada em: %s", time.Now().Format("2006-01-02 15:04:05.000")),
		fmt.Sprintf("Pasta base: %s", config.BaseDir),
		fmt.Sprintf("Modo: %s", modeLabel(config.Mode)),
		fmt.Sprintf("Termos: %s", strings.Join(config.Terms, "; ")),
	}

	if config.SearchAllFiles {
		header = append(header, "Extensoes: todas")
	} else {
		header = append(header, fmt.Sprintf("Extensoes: %s", strings.Join(extensionList(config.ExtensionFilter), "; ")))
	}
	header = append(header, "")

	for _, line := range header {
		if _, err := writer.WriteString(line + "\n"); err != nil {
			file.Close()
			return nil, err
		}
	}
	if err := writer.Flush(); err != nil {
		file.Close()
		return nil, err
	}

	return reporter, nil
}

func (r *ReportWriter) Append(matches []Match) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, match := range matches {
		line := formatMatch(match)
		if _, err := r.Writer.WriteString(line + "\n"); err != nil {
			return err
		}
		r.hasMatch = true
	}
	return r.Writer.Flush()
}

func (r *ReportWriter) WriteNoMatches() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.hasMatch {
		return nil
	}
	if _, err := r.Writer.WriteString("Nenhuma ocorrencia encontrada.\n"); err != nil {
		return err
	}
	return r.Writer.Flush()
}

func (r *ReportWriter) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.Writer.Flush(); err != nil {
		_ = r.File.Close()
		return err
	}
	return r.File.Close()
}

func formatMatch(match Match) string {
	line := fmt.Sprintf("Arquivo: %s", match.Path)
	if match.Kind == "conteudo" {
		return fmt.Sprintf("%s | Linha: %d | Trecho: %s", line, match.Line, match.Text)
	}
	return fmt.Sprintf("%s | Correspondencia no nome do arquivo", line)
}

func modeLabel(mode SearchMode) string {
	switch mode {
	case ModeName:
		return "nome do arquivo"
	case ModeContent:
		return "conteudo do arquivo"
	case ModeBoth:
		return "nome e conteudo"
	default:
		return "desconhecido"
	}
}

func extensionList(filter map[string]struct{}) []string {
	list := make([]string, 0, len(filter))
	for ext := range filter {
		list = append(list, ext)
	}
	sort.Strings(list)
	return list
}

func showFound(batch []Match) {
	for _, match := range batch {
		fmt.Printf("\nEncontrado: %s\n", formatMatch(match))
	}
}

func showReport(reportPath string) {
	content, err := os.ReadFile(reportPath)
	if err != nil {
		fmt.Printf("Nao foi possivel exibir o relatorio: %v\n", err)
		return
	}

	fmt.Println()
	fmt.Println("Relatorio gerado:")
	fmt.Println(string(content))
}

func waitForExit(reader *bufio.Reader) {
	fmt.Print("Pressione Enter para sair...")
	_, _ = reader.ReadString('\n')
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
