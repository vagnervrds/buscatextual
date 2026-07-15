package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var BuildVersion string = "dev"

// Constantes de cores ANSI
const (
	Reset   = "\033[0m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"
	Bold    = "\033[1m"

	// Cores do Tema
	ThemeCyan   = "\033[36m"
	ThemeYellow = "\033[33m"
	ThemeGreen  = "\033[32m"
)

func initConsole() {
	setConsoleTitle("BuscaTextual " + BuildVersion)
	enableANSI()
}

func setConsoleTitle(title string) {
	if runtime.GOOS != "windows" {
		return
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleTitleProc := kernel32.NewProc("SetConsoleTitleW")
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	setConsoleTitleProc.Call(uintptr(unsafe.Pointer(titlePtr)))
}

func enableANSI() {
	if runtime.GOOS != "windows" {
		return
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode := kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode := kernel32.NewProc("SetConsoleMode")

	stdout, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err != nil {
		return
	}

	var mode uint32
	r, _, _ := procGetConsoleMode.Call(uintptr(stdout), uintptr(unsafe.Pointer(&mode)))
	if r != 0 {
		// ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x0004
		mode |= 0x0004
		procSetConsoleMode.Call(uintptr(stdout), uintptr(mode))
	}
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

type interval struct {
	start int
	end   int
}

// highlightTerms destaca em vermelho negrito os termos buscados no texto
func highlightTerms(text string, terms []string) string {
	if len(terms) == 0 {
		return text
	}
	var intervals []interval
	lowerText := strings.ToLower(text)
	for _, term := range terms {
		termLower := strings.ToLower(term)
		if termLower == "" {
			continue
		}
		pos := 0
		for {
			idx := strings.Index(lowerText[pos:], termLower)
			if idx == -1 {
				break
			}
			start := pos + idx
			end := start + len(termLower)
			intervals = append(intervals, interval{start: start, end: end})
			pos = start + len(termLower)
		}
	}

	if len(intervals) == 0 {
		return text
	}

	// Ordena os intervalos pelo início
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].start == intervals[j].start {
			return intervals[i].end > intervals[j].end
		}
		return intervals[i].start < intervals[j].start
	})

	// Mescla intervalos que se sobrepõem
	var merged []interval
	for _, current := range intervals {
		if len(merged) == 0 {
			merged = append(merged, current)
			continue
		}
		last := &merged[len(merged)-1]
		if current.start <= last.end {
			if current.end > last.end {
				last.end = current.end
			}
		} else {
			merged = append(merged, current)
		}
	}

	// Reconstrói a string com cores ANSI
	var sb strings.Builder
	lastIdx := 0
	for _, inter := range merged {
		sb.WriteString(text[lastIdx:inter.start])
		sb.WriteString("\033[1;31m") // Bold Red
		sb.WriteString(text[inter.start:inter.end])
		sb.WriteString("\033[0m")
		lastIdx = inter.end
	}
	sb.WriteString(text[lastIdx:])
	return sb.String()
}

func main() {
	initConsole()
	reader := bufio.NewReader(os.Stdin)

	if err := initDB(); err != nil {
		fmt.Printf(Red+"Aviso: Nao foi possivel abrir o banco de dados 'buscatextual.db': %v\n"+Reset, err)
	} else {
		defer closeDB()
	}

	for {
		fmt.Println()
		fmt.Println(Bold + ThemeCyan + "==================================================" + Reset)
		fmt.Printf("   "+Bold+ThemeCyan+"Busca Textual"+Reset+" - Versao/Build: "+Bold+ThemeGreen+"%s\n"+Reset, BuildVersion)
		fmt.Println(Bold + ThemeCyan + "==================================================" + Reset)
		fmt.Println(Bold + " Menu Principal:" + Reset)
		fmt.Printf("  "+ThemeYellow+"1"+Reset+" - Busca Rapida (%sBanco de Dados%s - Somente Nomes)\n", Bold, Reset)
		fmt.Printf("  "+ThemeYellow+"2"+Reset+" - Buscar (%sDisco%s - Nome e Conteudo)\n", Bold, Reset)
		fmt.Printf("  "+ThemeYellow+"3"+Reset+" - Indexar Pasta (Atualizar Banco)\n")
		fmt.Printf("  "+ThemeYellow+"4"+Reset+" - Sair\n")
		fmt.Println(Bold + ThemeCyan + "--------------------------------------------------" + Reset)

		opcao := prompt(reader, Bold+"Escolha uma opcao: "+Reset)

		switch opcao {
		case "1":
			if realizarBuscaRapida(reader) {
				return
			}
		case "2":
			if realizarBusca(reader) {
				return
			}
		case "3":
			realizarIndexacao(reader)
		case "4":
			fmt.Println(Bold + ThemeGreen + "\nObrigado por usar o BuscaTextual! Ate logo." + Reset)
			return
		default:
			fmt.Println(Red + "Opcao invalida. Tente novamente." + Reset)
		}
	}
}

func realizarBusca(reader *bufio.Reader) bool {
	baseDir := prompt(reader, "\n"+Bold+"Informe o caminho da pasta para buscar: "+Reset)
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		fmt.Println(Red + "Pasta nao informada." + Reset)
		return false
	}

	info, err := os.Stat(baseDir)
	if err != nil || !info.IsDir() {
		fmt.Println(Red + "Pasta invalida ou inacessivel." + Reset)
		return false
	}

	mode := promptMode(reader)
	terms := promptTerms(reader)
	searchAllFiles, extensionFilter := promptExtensions(reader)
	if len(terms) == 0 {
		fmt.Println(Red + "Nenhum termo de busca valido foi informado." + Reset)
		return false
	}

	fmt.Println()
	fmt.Println(Bold + ThemeYellow + "Iniciando busca no disco e atualizando banco assincronamente..." + Reset)

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
		fmt.Printf(Red+"Erro durante a busca: %v\n"+Reset, err)
		return false
	}

	fmt.Println()
	fmt.Println(Bold + ThemeGreen + "Busca concluida." + Reset)
	fmt.Printf("Arquivos analisados: %s%d%s\n", ThemeCyan, result.ScannedFiles, Reset)
	fmt.Printf("Ocorrencias encontradas: %s%d%s\n", ThemeGreen, len(result.Matches), Reset)
	fmt.Printf("Tempo total: %s%s%s\n", ThemeYellow, time.Since(start).Round(time.Millisecond), Reset)
	fmt.Printf("Relatorio salvo em: %s%s%s\n", ThemeCyan, result.ReportPath, Reset)
	showReport(result.ReportPath)

	return !postSearchMenu(reader, result.ReportPath, result.Matches)
}

func realizarBuscaRapida(reader *bufio.Reader) bool {
	terms := promptTerms(reader)
	if len(terms) == 0 {
		fmt.Println(Red + "Nenhum termo informado." + Reset)
		return false
	}

	fmt.Println(Bold + ThemeYellow + "Buscando no banco de dados..." + Reset)
	start := time.Now()
	matches := searchFilenamesInDB(terms)

	fmt.Printf("\nBusca concluida em %s%s%s.\n", ThemeYellow, time.Since(start).Round(time.Millisecond), Reset)
	fmt.Printf("Ocorrencias encontradas no banco: %s%d%s\n", ThemeGreen, len(matches), Reset)

	if len(matches) > 0 {
		config := SearchConfig{BaseDir: "Banco de Dados", Terms: terms, Mode: ModeName, SearchAllFiles: true}
		reporter, _ := createReport(config)
		if reporter != nil {
			_ = reporter.Append(matches)
			reporter.Close()
			fmt.Printf("Relatorio salvo em: %s%s%s\n", ThemeCyan, reporter.Path, Reset)
			return !postSearchMenu(reader, reporter.Path, matches)
		}
	} else {
		fmt.Println(Yellow + "Nenhuma ocorrencia encontrada." + Reset)
	}
	return false
}

// getDiskID retorna a letra da unidade no Windows (ex: "D:") ou "/" como fallback
func getDiskID(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	vol := filepath.VolumeName(abs)
	if vol != "" {
		return strings.ToUpper(vol)
	}
	return "/"
}

// WalkState mantém o estado da busca concorrente de arquivos
type WalkState struct {
	mu         sync.Mutex
	cond       *sync.Cond
	pending    int
	queue      []string
	fileChan   chan<- FileMeta
	dirsCount  int64
	filesCount int64
}

func newWalkState(baseDir string, fileChan chan<- FileMeta) *WalkState {
	ws := &WalkState{
		queue:    []string{baseDir},
		pending:  1,
		fileChan: fileChan,
	}
	ws.cond = sync.NewCond(&ws.mu)
	return ws
}

func (ws *WalkState) push(dir string) {
	ws.mu.Lock()
	ws.queue = append(ws.queue, dir)
	ws.pending++
	ws.mu.Unlock()
	ws.cond.Signal()
}

func (ws *WalkState) pop() (string, bool) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	for len(ws.queue) == 0 && ws.pending > 0 {
		ws.cond.Wait()
	}
	if len(ws.queue) == 0 && ws.pending == 0 {
		return "", false
	}
	dir := ws.queue[0]
	ws.queue = ws.queue[1:]
	return dir, true
}

func (ws *WalkState) done() {
	ws.mu.Lock()
	ws.pending--
	ws.mu.Unlock()
	ws.cond.Broadcast()
}

type BenchPhase struct {
	workers    int32
	startDirs  int64
	startFiles int64
	startTime  time.Time
	throughput float64
}

func realizarIndexacao(reader *bufio.Reader) {
	baseDir := prompt(reader, "\n"+Bold+"Informe o caminho da pasta para indexar: "+Reset)
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		fmt.Println(Red + "Pasta nao informada." + Reset)
		return
	}

	info, err := os.Stat(baseDir)
	if err != nil || !info.IsDir() {
		fmt.Println(Red + "Pasta invalida ou inacessivel." + Reset)
		return
	}

	absPath, err := filepath.Abs(baseDir)
	if err != nil {
		absPath = baseDir
	}

	diskID := getDiskID(absPath)
	fmt.Printf("\nIdentificando volume do disco: %s%s%s\n", Bold+ThemeCyan, diskID, Reset)

	optimalThreads := getDiskOptimalThreads(diskID)

	var isBenchmarking bool
	var initialThreads int32 = 2
	if optimalThreads > 0 {
		fmt.Printf("Perfil de desempenho encontrado para o disco: %s%d threads%s\n", Bold+ThemeGreen, optimalThreads, Reset)
		initialThreads = int32(optimalThreads)
		isBenchmarking = false
	} else {
		fmt.Printf("Nenhum perfil encontrado para o disco. %sIniciando autotuning de threads (2 -> 4 -> 8 -> 16)%s\n", ThemeYellow, Reset)
		initialThreads = 2
		isBenchmarking = true
	}

	fmt.Println(Bold + "Iniciando indexacao concorrente..." + Reset)
	start := time.Now()

	fileChan := make(chan FileMeta, 50000)

	// Goroutine de gravação em lote no BoltDB
	dbDone := make(chan int)
	go func() {
		count := 0
		batchSize := 5000
		tx, err := db.Begin(true)
		if err != nil {
			fmt.Printf("\n%sErro ao abrir transacao no banco: %v%s\n", Red, err, Reset)
			dbDone <- 0
			return
		}
		b := tx.Bucket([]byte("Files"))

		for meta := range fileChan {
			_ = putIndexHelper(b, meta.Path, meta.Size, meta.ModTime)
			count++

			if count%batchSize == 0 {
				_ = tx.Commit()
				tx, err = db.Begin(true)
				if err != nil {
					fmt.Printf("\n%sErro ao continuar transacao: %v%s\n", Red, err, Reset)
					dbDone <- count
					return
				}
				b = tx.Bucket([]byte("Files"))
			}
		}

		_ = tx.Commit()
		dbDone <- count
	}()

	state := newWalkState(absPath, fileChan)

	var activeWorkers int32 = 0
	var targetWorkers int32 = initialThreads

	var dirsProcessed int64 = 0
	var filesScanned int64 = 0

	// Fases do benchmark
	phases := []BenchPhase{
		{workers: 2, startDirs: 0},
		{workers: 4, startDirs: 50},
		{workers: 8, startDirs: 100},
		{workers: 16, startDirs: 150},
	}
	currentPhaseIdx := 0
	var benchMu sync.Mutex

	var workerFunc func()
	workerFunc = func() {
		for {
			// Controle de escalabilidade para baixo
			if atomic.LoadInt32(&activeWorkers) > atomic.LoadInt32(&targetWorkers) {
				atomic.AddInt32(&activeWorkers, -1)
				return
			}

			dir, ok := state.pop()
			if !ok {
				atomic.AddInt32(&activeWorkers, -1)
				return
			}

			entries, err := os.ReadDir(dir)
			if err == nil {
				for _, entry := range entries {
					if entry.IsDir() {
						state.push(filepath.Join(dir, entry.Name()))
					} else {
						info, err := entry.Info()
						if err == nil {
							atomic.AddInt64(&filesScanned, 1)
							fileChan <- FileMeta{
								Path:    filepath.Join(dir, entry.Name()),
								Size:    info.Size(),
								ModTime: info.ModTime().Format(time.RFC3339Nano),
							}
						}
					}
				}
			}

			state.done()

			// Incrementa diretórios processados e atualiza o benchmark
			dirsNow := atomic.AddInt64(&dirsProcessed, 1)

			if isBenchmarking {
				benchMu.Lock()
				if currentPhaseIdx < len(phases) {
					phase := &phases[currentPhaseIdx]

					// Marca início da fase
					if dirsNow == phase.startDirs+1 {
						phase.startTime = time.Now()
						phase.startFiles = atomic.LoadInt64(&filesScanned)
					}

					// Gatilho de fim da fase
					nextDirsTrigger := phase.startDirs + 50
					if dirsNow == nextDirsTrigger {
						duration := time.Since(phase.startTime)
						filesDiff := atomic.LoadInt64(&filesScanned) - phase.startFiles

						if duration > 0 {
							phase.throughput = float64(filesDiff) / duration.Seconds()
						} else {
							phase.throughput = 0
						}

						fmt.Printf("\n[Autotuning] Medida Fase %d (%d threads): %s%.1f arquivos/s%s\n",
							currentPhaseIdx+1, phase.workers, ThemeCyan, phase.throughput, Reset)

						currentPhaseIdx++
						if currentPhaseIdx < len(phases) {
							nextPhase := &phases[currentPhaseIdx]
							atomic.StoreInt32(&targetWorkers, nextPhase.workers)

							diff := nextPhase.workers - atomic.LoadInt32(&activeWorkers)
							for i := int32(0); i < diff; i++ {
								atomic.AddInt32(&activeWorkers, 1)
								go workerFunc()
							}
						} else {
							// Fim de todas as fases de benchmark
							isBenchmarking = false
							bestWorkers := int32(2)
							var maxThroughput float64 = -1
							for _, p := range phases {
								if p.throughput > maxThroughput {
									maxThroughput = p.throughput
									bestWorkers = p.workers
								}
							}

							fmt.Printf("\n[Autotuning Concluido] Melhor desempenho detectado: %s%d threads%s (%.1f arq/s). Gravando perfil...\n",
								Bold+ThemeGreen, bestWorkers, Reset, maxThroughput)

							_ = saveDiskOptimalThreads(diskID, int(bestWorkers))
							atomic.StoreInt32(&targetWorkers, bestWorkers)
						}
					}
				}
				benchMu.Unlock()
			}
		}
	}

	// Inicia os primeiros workers do crawl
	for i := int32(0); i < initialThreads; i++ {
		atomic.AddInt32(&activeWorkers, 1)
		go workerFunc()
	}

	// Feedback de progresso
	progressDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-progressDone:
				return
			case <-ticker.C:
				t := atomic.LoadInt32(&targetWorkers)
				d := atomic.LoadInt64(&dirsProcessed)
				f := atomic.LoadInt64(&filesScanned)
				fmt.Printf("\rCrawl: %s%d%s pastas | %s%d%s arquivos encontrados | Threads: %s%d%s ...",
					ThemeCyan, d, Reset, ThemeGreen, f, Reset, ThemeYellow, t, Reset)
			}
		}
	}()

	// Aguarda crawl
	for atomic.LoadInt32(&activeWorkers) > 0 {
		time.Sleep(10 * time.Millisecond)
	}

	close(fileChan)
	close(progressDone)

	totalFilesWritten := <-dbDone
	totalTime := time.Since(start).Round(time.Millisecond)

	fmt.Printf("\n\n%sIndexacao concluida!%s\n", Bold+ThemeGreen, Reset)
	fmt.Printf("Arquivos salvos no banco: %s%d%s\n", ThemeCyan, totalFilesWritten, Reset)
	fmt.Printf("Tempo total: %s%s%s\n", ThemeYellow, totalTime, Reset)
}

func postSearchMenu(reader *bufio.Reader, reportPath string, matches []Match) bool {
	for {
		fmt.Println()
		fmt.Println(Bold + "O que deseja fazer agora?" + Reset)
		fmt.Printf("  "+ThemeYellow+"1"+Reset+" - Abrir arquivo de relatorio\n")
		fmt.Printf("  "+ThemeYellow+"2"+Reset+" - Abrir pastas dos resultados passo a passo\n")
		fmt.Printf("  "+ThemeYellow+"3"+Reset+" - Voltar ao menu principal\n")
		fmt.Printf("  "+ThemeYellow+"4"+Reset+" - Sair\n")

		switch prompt(reader, Bold+"Escolha uma opcao (1/2/3/4): "+Reset) {
		case "1":
			openFile(reportPath)
		case "2":
			quitToMainMenu := abrirPastasPassoAPasso(reader, matches)
			if quitToMainMenu {
				return true
			}
		case "3":
			return true
		case "4":
			return false
		default:
			fmt.Println(Red + "Opcao invalida." + Reset)
		}
	}
}

func abrirPastasPassoAPasso(reader *bufio.Reader, matches []Match) bool {
	if len(matches) == 0 {
		fmt.Println(Yellow + "Nenhum resultado para abrir." + Reset)
		return false
	}

	// Extrair pastas únicas dos arquivos encontrados
	var dirs []string
	seen := make(map[string]bool)
	for _, m := range matches {
		dir := filepath.Dir(m.Path)
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}

	fmt.Printf("\n%s--- Modo: Abertura de Pastas (%d pastas encontradas) ---%s\n", Bold+ThemeCyan, len(dirs), Reset)
	for i, dir := range dirs {
		fmt.Printf("\n[%d/%d] Pasta atual: %s%s%s\n", i+1, len(dirs), ThemeCyan, dir, Reset)
		ans := prompt(reader, Bold+"Pressione Enter para abrir, ou 'q' para voltar ao menu principal: "+Reset)
		if strings.ToLower(ans) == "q" {
			fmt.Println(Yellow + "Retornando ao menu principal..." + Reset)
			return true
		}
		openFile(dir)
	}
	fmt.Println(Bold + ThemeGreen + "\nTodas as pastas foram abertas!" + Reset)
	return false
}

func openFile(path string) {
	var err error
	switch runtime.GOOS {
	case "windows":
		err = exec.Command("cmd", "/c", "start", "", path).Run()
	case "darwin":
		err = exec.Command("open", path).Run()
	default: // linux e outros
		err = exec.Command("xdg-open", path).Run()
	}

	if err != nil {
		fmt.Printf(Red+"Erro ao abrir o arquivo/pasta: %v\n"+Reset, err)
	}
}

func prompt(reader *bufio.Reader, label string) string {
	fmt.Print(label)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

func promptMode(reader *bufio.Reader) SearchMode {
	for {
		fmt.Println()
		fmt.Println(Bold + "Tipo de busca:" + Reset)
		fmt.Println("  1 - Nome do arquivo")
		fmt.Println("  2 - Conteudo do arquivo")
		fmt.Println("  3 - Ambas as opcoes")

		switch prompt(reader, Bold+"Escolha uma opcao (1/2/3): "+Reset) {
		case "1":
			return ModeName
		case "2":
			return ModeContent
		case "3":
			return ModeBoth
		default:
			fmt.Println(Red + "Opcao invalida." + Reset)
		}
	}
}

func promptTerms(reader *bufio.Reader) []string {
	raw := prompt(reader, Bold+"Informe os termos de busca separados por ';' (ex: erro;cliente): "+Reset)
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
		fmt.Println(Bold + "Filtro de extensao:" + Reset)
		fmt.Println("  1 - Buscar em todos os arquivos")
		fmt.Println("  2 - Filtrar por extensoes")

		switch prompt(reader, Bold+"Escolha uma opcao (1/2): "+Reset) {
		case "1":
			return true, nil
		case "2":
			raw := prompt(reader, Bold+"Informe as extensoes separadas por ';' (ex.: .txt;.go;.json): "+Reset)
			filter := parseExtensions(raw)
			if len(filter) == 0 {
				fmt.Println(Red + "Nenhuma extensao valida foi informada." + Reset)
				continue
			}
			return false, filter
		default:
			fmt.Println(Red + "Opcao invalida." + Reset)
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

	// Canal para indexação assíncrona no BoltDB durante a busca
	searchIndexChan := make(chan FileMeta, 10000)
	indexDone := make(chan struct{})
	go func() {
		defer close(indexDone)
		count := 0
		batchSize := 2000
		tx, err := db.Begin(true)
		if err != nil {
			return
		}
		b := tx.Bucket([]byte("Files"))

		for meta := range searchIndexChan {
			_ = putIndexHelper(b, meta.Path, meta.Size, meta.ModTime)
			count++
			if count%batchSize == 0 {
				_ = tx.Commit()
				tx, err = db.Begin(true)
				if err != nil {
					return
				}
				b = tx.Bucket([]byte("Files"))
			}
		}
		_ = tx.Commit()
	}()

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
			showFound(batch, config.Terms)
		}
	}()

	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for path := range jobs {
				fileMatches, err := processFile(path, config.Mode, config.Terms, searchIndexChan)
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
			fmt.Printf("\n%sIgnorando caminho com erro: %s (%v)%s\n", Red, path, walkErr, Reset)
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
	close(searchIndexChan)
	<-indexDone
	close(results)
	close(errorsCh)
	collector.Wait()
	close(progressDone)

	for msg := range errorsCh {
		fmt.Printf("\n%s%s%s\n", Red, msg, Reset)
	}

	totalScanned := int(scannedFiles.Load())
	fmt.Printf("\r%sAnalisando arquivos...%s %s%d%s | workers: %s%d%s\n",
		ThemeCyan, Reset, ThemeGreen, totalScanned, Reset, ThemeYellow, workerCount, Reset)

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

func processFile(path string, mode SearchMode, terms []string, indexChan chan<- FileMeta) ([]Match, error) {
	var matches []Match

	info, err := os.Stat(path)
	if err == nil && indexChan != nil {
		select {
		case indexChan <- FileMeta{
			Path:    path,
			Size:    info.Size(),
			ModTime: info.ModTime().Format(time.RFC3339Nano),
		}:
		default:
			// Não bloqueia caso o canal esteja momentaneamente cheio
		}
	}

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
	fileName := fmt.Sprintf("resultado_busca_%s.toml", timestamp)
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

	fmt.Fprintf(writer, "[metadados]\n")
	fmt.Fprintf(writer, "data_inicio = %q\n", time.Now().Format("2006-01-02 15:04:05.000"))
	fmt.Fprintf(writer, "pasta_base = %q\n", config.BaseDir)
	fmt.Fprintf(writer, "modo = %q\n", modeLabel(config.Mode))
	fmt.Fprintf(writer, "termos = [%s]\n", formatTOMLStringList(config.Terms))

	if config.SearchAllFiles {
		fmt.Fprintf(writer, "extensoes = \"todas\"\n")
	} else {
		exts := extensionList(config.ExtensionFilter)
		fmt.Fprintf(writer, "extensoes = [%s]\n", formatTOMLStringList(exts))
	}
	fmt.Fprintf(writer, "\n")

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
		fmt.Fprintf(r.Writer, "[[resultados]]\n")
		fmt.Fprintf(r.Writer, "arquivo = %q\n", match.Path)
		fmt.Fprintf(r.Writer, "tipo = %q\n", match.Kind)
		if match.Kind == "conteudo" {
			fmt.Fprintf(r.Writer, "linha = %d\n", match.Line)
			fmt.Fprintf(r.Writer, "trecho = %q\n", match.Text)
		}
		fmt.Fprintf(r.Writer, "\n")
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
	if _, err := r.Writer.WriteString("# Nenhuma ocorrencia encontrada.\nresultados = []\n"); err != nil {
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

func formatMatch(match Match, terms []string) string {
	highlightedPath := highlightTerms(match.Path, terms)
	line := fmt.Sprintf("%sArquivo:%s %s", ThemeCyan, Reset, highlightedPath)
	if match.Kind == "conteudo" {
		highlightedText := highlightTerms(match.Text, terms)
		return fmt.Sprintf("%s | %sLinha:%s %d | %sTrecho:%s %s", line, ThemeYellow, Reset, match.Line, ThemeYellow, Reset, highlightedText)
	}
	return fmt.Sprintf("%s | %s[Correspondencia no nome]%s", line, Bold+ThemeGreen, Reset)
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

func showFound(batch []Match, terms []string) {
	for _, match := range batch {
		fmt.Printf("\n%sEncontrado:%s %s\n", Bold+ThemeGreen, Reset, formatMatch(match, terms))
	}
}

func showReport(reportPath string) {
	content, err := os.ReadFile(reportPath)
	if err != nil {
		fmt.Printf(Red+"Nao foi possivel exibir o relatorio: %v\n"+Reset, err)
		return
	}

	fmt.Println()
	fmt.Println(Bold + "Relatorio gerado:" + Reset)
	fmt.Println(string(content))
}

func calculateWorkerCount(mode SearchMode) int {
	cpuCount := runtime.NumCPU()
	if cpuCount < 1 {
		return 1
	}

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
			fmt.Printf("\r%sAnalisando arquivos...%s %s%d%s | workers: %s%d%s",
				ThemeCyan, Reset, ThemeGreen, scannedFiles.Load(), Reset, ThemeYellow, workerCount, Reset)
		}
	}
}

func exitWithMessage(message string) {
	fmt.Println(message)
	os.Exit(1)
}

func formatTOMLStringList(list []string) string {
	if len(list) == 0 {
		return ""
	}
	quoted := make([]string, len(list))
	for i, s := range list {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(quoted, ", ")
}
