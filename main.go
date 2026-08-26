package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
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

type SortMode int

const (
	SortByFolder SortMode = iota + 1
	SortBySizeDesc
	SortBySizeAsc
	SortByDateDesc
	SortByDateAsc
)

type Match struct {
	Path    string
	Kind    string
	Line    int
	Text    string
	Size    int64
	ModTime time.Time
}

type TargetType int

const (
	TargetFiles TargetType = iota // padrao
	TargetDirectories
)

type SearchConfig struct {
	BaseDir        string
	Mode           SearchMode
	Terms          []string
	PositiveFilter []string
	NegativeFilter []string
	TargetType     TargetType
	SortMode       SortMode
	ReportFormat   string
	MatchingMode   string

	// Campos pré-normalizados para alta performance
	normTerms     []string
	normPosFilter []string
	normNegFilter []string
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
func highlightTerms(text string, terms []string, mode string) string {
	if len(terms) == 0 || text == "" {
		return text
	}
	if mode == "" {
		mode = "ampla"
	}
	intervals := FindMatchIntervals(text, terms, mode)
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
		if inter.start > len(text) || inter.end > len(text) || inter.start > inter.end {
			continue
		}
		sb.WriteString(text[lastIdx:inter.start])
		sb.WriteString("\033[1;31m") // Bold Red
		sb.WriteString(text[inter.start:inter.end])
		sb.WriteString("\033[0m")
		lastIdx = inter.end
	}
	if lastIdx < len(text) {
		sb.WriteString(text[lastIdx:])
	}
	return sb.String()
}

func main() {
	_ = InitLogger("log")
	defer CloseLogger()
	defer func() {
		if r := recover(); r != nil {
			LogRecover(r)
			panic(r)
		}
	}()

	cleanupOldBinary()
	initConsole()
	reader := bufio.NewReader(os.Stdin)

	if err := initDB(); err != nil {
		if err == errDatabaseLocked {
			fmt.Println(Bold + Red + "\nErro: Ja existe outra instancia do BuscaTextual aberta!" + Reset)
			fmt.Println(Yellow + "O banco de dados 'buscatextual.db' esta bloqueado pelo outro processo." + Reset)
		} else {
			fmt.Printf(Red+"\nErro ao inicializar o banco de dados 'buscatextual.db': %v\n"+Reset, err)
		}
		LogError("Erro critico ao inicializar banco de dados 'buscatextual.db'", err)
		fmt.Println("\nPressione Enter para fechar...")
		_, _ = reader.ReadString('\n')
		os.Exit(1)
	}
	defer closeDB()

	for {
		fmt.Println()
		fmt.Println(Bold + ThemeCyan + "==================================================" + Reset)
		fmt.Printf("   "+Bold+ThemeCyan+"Busca Textual"+Reset+" - Versao/Build: "+Bold+ThemeGreen+"%s\n"+Reset, BuildVersion)
		fmt.Println(Bold + ThemeCyan + "==================================================" + Reset)
		fmt.Println(Bold + " Menu Principal:" + Reset)
		fmt.Printf("  "+ThemeYellow+"1"+Reset+" - Busca Rapida (%sBanco de Dados%s - Somente Nomes)\n", Bold, Reset)
		fmt.Printf("  "+ThemeYellow+"2"+Reset+" - Buscar (%sDisco%s - Nome e Conteudo)\n", Bold, Reset)
		fmt.Printf("  "+ThemeYellow+"3"+Reset+" - Indexar Pasta (Atualizar Banco)\n")
		fmt.Printf("  "+ThemeYellow+"4"+Reset+" - Checar Atualizacoes (GitHub)\n")
		fmt.Printf("  "+ThemeYellow+"5"+Reset+" - Resetar Benchmarks de Concorrencia\n")
		fmt.Printf("  "+ThemeYellow+"6"+Reset+" - Configuracoes\n")
		fmt.Printf("  "+ThemeYellow+"7"+Reset+" - Sair\n")
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
			realizarChecagemAtualizacao(reader)
		case "5":
			realizarResetBenchmarks(reader)
		case "6":
			realizarConfiguracoes(reader)
		case "7":
			fmt.Println(Bold + ThemeGreen + "\nObrigado por usar o BuscaTextual! Ate logo." + Reset)
			return
		default:
			fmt.Println(Red + "Opcao invalida. Tente novamente." + Reset)
		}
	}
}

func cleanupOldBinary() {
	execPath, err := os.Executable()
	if err != nil {
		return
	}
	oldPath := execPath + ".old"
	if _, err := os.Stat(oldPath); err == nil {
		_ = os.Remove(oldPath)
	}
}

type RemoteBuildInfo struct {
	Build int `json:"build"`
}

func getLocalBuildNumber() int {
	cleanVer := strings.TrimSpace(BuildVersion)
	num, err := strconv.Atoi(cleanVer)
	if err != nil {
		return 0
	}
	return num
}

func checkUpdate() (hasUpdate bool, remoteBuild int, err error) {
	client := &http.Client{Timeout: 10 * time.Second}
	urls := []string{
		"https://raw.githubusercontent.com/vagnervrds/buscatextual/master/build.json",
		"https://raw.githubusercontent.com/vagnervrds/buscatextual/main/build.json",
	}

	var resp *http.Response
	var lastErr error
	for _, url := range urls {
		r, err := client.Get(url)
		if err == nil && r.StatusCode == http.StatusOK {
			resp = r
			break
		}
		if r != nil {
			r.Body.Close()
		}
		if err != nil {
			lastErr = err
		} else if r != nil {
			lastErr = fmt.Errorf("resposta invalida do GitHub (HTTP %d)", r.StatusCode)
		}
	}

	if resp == nil {
		if lastErr != nil {
			return false, 0, fmt.Errorf("falha ao conectar ao GitHub: %v", lastErr)
		}
		return false, 0, fmt.Errorf("nao foi possivel obter o arquivo de versao do GitHub")
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, 0, fmt.Errorf("falha ao ler resposta do GitHub: %v", err)
	}

	// Remove UTF-8 BOM (Byte Order Mark) se presente
	bodyBytes = bytes.TrimPrefix(bodyBytes, []byte{0xEF, 0xBB, 0xBF})

	var remoteInfo RemoteBuildInfo
	if err := json.Unmarshal(bodyBytes, &remoteInfo); err != nil {
		return false, 0, fmt.Errorf("falha ao decodificar JSON de versao: %v", err)
	}

	localBuild := getLocalBuildNumber()
	if remoteInfo.Build > localBuild {
		return true, remoteInfo.Build, nil
	}

	return false, remoteInfo.Build, nil
}

type progressWriter struct {
	total      int64
	downloaded int64
	lastPct    int
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n := len(p)
	pw.downloaded += int64(n)
	if pw.total > 0 {
		pct := int((float64(pw.downloaded) / float64(pw.total)) * 100)
		if pct >= pw.lastPct+5 || pct == 100 {
			pw.lastPct = pct
			fmt.Printf("\rBaixando atualizacao: %d%% (%d / %d bytes)...", pct, pw.downloaded, pw.total)
		}
	} else {
		fmt.Printf("\rBaixando atualizacao: %d bytes...", pw.downloaded)
	}
	return n, nil
}

func downloadAndUpdate(remoteBuild int) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("nao foi possivel identificar o executavel atual: %v", err)
	}

	urls := []string{
		"https://raw.githubusercontent.com/vagnervrds/buscatextual/master/buscatextual.exe",
		"https://github.com/vagnervrds/buscatextual/raw/master/buscatextual.exe",
		"https://raw.githubusercontent.com/vagnervrds/buscatextual/main/buscatextual.exe",
		"https://github.com/vagnervrds/buscatextual/raw/main/buscatextual.exe",
		"https://github.com/vagnervrds/buscatextual/releases/latest/download/buscatextual.exe",
	}

	var resp *http.Response
	var downloadErr error
	client := &http.Client{Timeout: 5 * time.Minute}

	fmt.Println(Bold + ThemeYellow + "Buscando arquivo da atualizacao no GitHub..." + Reset)
	for _, downloadURL := range urls {
		req, err := http.NewRequest("GET", downloadURL, nil)
		if err != nil {
			continue
		}
		res, err := client.Do(req)
		if err == nil && res.StatusCode == http.StatusOK {
			resp = res
			break
		}
		if res != nil {
			res.Body.Close()
		}
		downloadErr = err
	}

	if resp == nil {
		if downloadErr != nil {
			return fmt.Errorf("falha ao baixar o arquivo executavel: %v", downloadErr)
		}
		return fmt.Errorf("executavel nao encontrado na release ou repositorio GitHub")
	}
	defer resp.Body.Close()

	newPath := execPath + ".new"
	oldPath := execPath + ".old"

	out, err := os.Create(newPath)
	if err != nil {
		return fmt.Errorf("falha ao criar arquivo temporario %s: %v", newPath, err)
	}

	pw := &progressWriter{total: resp.ContentLength}
	_, err = io.Copy(out, io.TeeReader(resp.Body, pw))
	out.Close()
	fmt.Println()

	if err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("falha durante o download do executavel: %v", err)
	}

	_ = os.Remove(oldPath)

	err = os.Rename(execPath, oldPath)
	if err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("falha ao renomear executavel atual: %v", err)
	}

	err = os.Rename(newPath, execPath)
	if err != nil {
		_ = os.Rename(oldPath, execPath)
		return fmt.Errorf("falha ao instalar novo executavel: %v", err)
	}

	fmt.Println(Bold + ThemeGreen + "\nAtualizacao para o build " + strconv.Itoa(remoteBuild) + " instalada com sucesso!" + Reset)
	fmt.Println(Yellow + "Reiniciando o BuscaTextual..." + Reset)
	time.Sleep(1 * time.Second)

	cmd := exec.Command(execPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		fmt.Printf(Red+"Erro ao reiniciar o aplicativo: %v. Por favor, inicie manualmente.\n"+Reset, err)
	}

	os.Exit(0)
	return nil
}

func realizarChecagemAtualizacao(reader *bufio.Reader) {
	fmt.Println()
	fmt.Println(Bold + ThemeCyan + "==================================================" + Reset)
	fmt.Println(Bold + " Checando Atualizacoes no GitHub..." + Reset)
	fmt.Println(Bold + ThemeCyan + "==================================================" + Reset)

	downloadURL := "https://raw.githubusercontent.com/vagnervrds/buscatextual/master/buscatextual.exe"

	localBuild := getLocalBuildNumber()
	fmt.Printf(" Versao/Build Local: %s%s%s (build %d)\n", Bold+ThemeGreen, BuildVersion, Reset, localBuild)

	hasUpdate, remoteBuild, err := checkUpdate()
	if err != nil {
		LogError("Erro ao verificar atualizacoes no GitHub", err)
		fmt.Printf(Red+"\nErro ao verificar atualizacoes: %v\n"+Reset, err)
		fmt.Printf("\n%sVoce pode baixar o executavel (.exe) diretamente em:%s\n%s%s%s\n", Bold+ThemeYellow, Reset, ThemeCyan, downloadURL, Reset)
		return
	}

	fmt.Printf(" Versao/Build no GitHub: %s%d%s\n", Bold+ThemeCyan, remoteBuild, Reset)
	fmt.Printf(" URL do Executavel (.exe): %s%s%s\n", ThemeCyan, downloadURL, Reset)

	if hasUpdate {
		fmt.Println(Bold + ThemeYellow + "\n[!] Nova versao disponivel!" + Reset)
		resp := prompt(reader, Bold+"Deseja baixar e atualizar o aplicativo agora? (s/N): "+Reset)
		resp = strings.ToLower(strings.TrimSpace(resp))
		if resp == "s" || resp == "sim" {
			if err := downloadAndUpdate(remoteBuild); err != nil {
				LogError("Falha ao baixar e aplicar atualizacao do BuscaTextual", err)
				fmt.Printf(Red+"\nFalha ao atualizar: %v\n"+Reset, err)
				fmt.Printf("%sLink direto para download manual:%s %s%s%s\n", Yellow, Reset, ThemeCyan, downloadURL, Reset)
			}
		} else {
			fmt.Println(Yellow + "Atualizacao cancelada." + Reset)
		}
	} else {
		fmt.Println(Bold + ThemeGreen + "\n[V] Voce ja esta utilizando a versao mais recente!" + Reset)
	}
}

func realizarResetBenchmarks(reader *bufio.Reader) {
	fmt.Println()
	conf := prompt(reader, Bold+"Tem certeza de que deseja resetar os benchmarks de concorrencia? (s/N): "+Reset)
	conf = strings.ToLower(strings.TrimSpace(conf))
	if conf != "s" && conf != "sim" {
		fmt.Println(Yellow + "Operacao cancelada." + Reset)
		return
	}

	if err := resetOptimalThreads(); err != nil {
		fmt.Printf(Red+"Erro ao resetar benchmarks: %v\n"+Reset, err)
	} else {
		fmt.Println(ThemeGreen + "Benchmarks de concorrencia resetados com sucesso!" + Reset)
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
	posFilter := promptPositiveFilter(reader)
	negFilter := promptNegativeFilter(reader)
	targetType := promptTargetType(reader)
	if len(terms) == 0 {
		fmt.Println(Red + "Nenhum termo de busca valido foi informado." + Reset)
		return false
	}

	sortMode := promptSortMode(reader)
	reportFormat := promptReportFormat(reader)

	fmt.Println()
	fmt.Println(Bold + ThemeYellow + "Iniciando busca no disco e atualizando banco assincronamente..." + Reset)

	matchingMode := getMatchingMode()
	start := time.Now()
	config := SearchConfig{
		BaseDir:        baseDir,
		Mode:           mode,
		Terms:          terms,
		PositiveFilter: posFilter,
		NegativeFilter: negFilter,
		TargetType:     targetType,
		SortMode:       sortMode,
		ReportFormat:   reportFormat,
		MatchingMode:   matchingMode,
	}

	result, err := runSearch(config)
	if err != nil {
		fmt.Printf(Red+"Erro durante a busca: %v\n"+Reset, err)
		return false
	}

	fmt.Println()
	fmt.Println(Bold + ThemeGreen + "Busca concluida." + Reset)
	fmt.Printf("Arquivos analisados: %s%d%s\n", ThemeCyan, result.ScannedFiles, Reset)
	if config.TargetType == TargetDirectories {
		fmt.Printf("Pastas unicas encontradas: %s%d%s\n", ThemeGreen, len(result.Matches), Reset)
	} else {
		fmt.Printf("Ocorrencias encontradas: %s%d%s\n", ThemeGreen, len(result.Matches), Reset)
	}
	fmt.Printf("Tempo total: %s%s%s\n", ThemeYellow, time.Since(start).Round(time.Millisecond), Reset)
	fmt.Printf("Relatorio salvo em: %s%s%s\n", ThemeCyan, result.ReportPath, Reset)

	return !postSearchMenu(reader, result.ReportPath, result.Matches)
}

func realizarBuscaRapida(reader *bufio.Reader) bool {
	terms := promptTerms(reader)
	posFilter := promptPositiveFilter(reader)
	negFilter := promptNegativeFilter(reader)
	targetType := promptTargetType(reader)
	if len(terms) == 0 {
		fmt.Println(Red + "Nenhum termo informado." + Reset)
		return false
	}

	sortMode := promptSortMode(reader)
	reportFormat := promptReportFormat(reader)
	matchingMode := getMatchingMode()

	fmt.Println(Bold + ThemeYellow + "Buscando no banco de dados..." + Reset)
	start := time.Now()
	matches := searchFilenamesInDB(terms, posFilter, negFilter, matchingMode)

	if targetType == TargetDirectories {
		matches = convertToUniqueDirectoryMatches(matches)
	}

	sortMatches(matches, sortMode)

	fmt.Printf("\nBusca concluida em %s%s%s.\n", ThemeYellow, time.Since(start).Round(time.Millisecond), Reset)
	if targetType == TargetDirectories {
		fmt.Printf("Pastas unicas encontradas no banco: %s%d%s\n", ThemeGreen, len(matches), Reset)
	} else {
		fmt.Printf("Ocorrencias encontradas no banco: %s%d%s\n", ThemeGreen, len(matches), Reset)
	}

	if len(matches) > 0 {
		config := SearchConfig{
			BaseDir:        "Banco de Dados",
			Terms:          terms,
			PositiveFilter: posFilter,
			NegativeFilter: negFilter,
			TargetType:     targetType,
			Mode:           ModeName,
			SortMode:       sortMode,
			ReportFormat:   reportFormat,
			MatchingMode:   matchingMode,
		}
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
		defer func() {
			if r := recover(); r != nil {
				LogRecover(r)
				dbDone <- 0
			}
		}()

		count := 0
		batchSize := 5000
		tx, err := db.Begin(true)
		if err != nil {
			LogError("Erro ao abrir transacao no banco durante indexacao", err)
			fmt.Printf("\n%sErro ao abrir transacao no banco: %v%s\n", Red, err, Reset)
			dbDone <- 0
			return
		}
		b, err := tx.CreateBucketIfNotExists([]byte("Files"))
		if err != nil {
			_ = tx.Rollback()
			LogError("Erro ao acessar bucket Files na indexacao", err)
			dbDone <- 0
			return
		}

		for meta := range fileChan {
			_ = putIndexHelper(b, meta.Path, meta.Size, meta.ModTime)
			count++

			if count%batchSize == 0 {
				_ = tx.Commit()
				tx, err = db.Begin(true)
				if err != nil {
					LogError("Erro ao continuar transacao no banco durante lote de indexacao", err)
					fmt.Printf("\n%sErro ao continuar transacao: %v%s\n", Red, err, Reset)
					dbDone <- count
					return
				}
				b, err = tx.CreateBucketIfNotExists([]byte("Files"))
				if err != nil {
					_ = tx.Rollback()
					LogError("Erro ao recriar bucket Files na indexacao", err)
					dbDone <- count
					return
				}
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
		defer func() {
			if r := recover(); r != nil {
				LogRecover(r)
			}
		}()

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
		defer func() {
			if r := recover(); r != nil {
				LogRecover(r)
			}
		}()

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
		fmt.Printf("  "+ThemeYellow+"3"+Reset+" - Ver resultados no terminal\n")
		fmt.Printf("  "+ThemeYellow+"4"+Reset+" - Voltar ao menu principal\n")
		fmt.Printf("  "+ThemeYellow+"5"+Reset+" - Sair\n")

		switch prompt(reader, Bold+"Escolha uma opcao (1/2/3/4/5): "+Reset) {
		case "1":
			openFile(reportPath)
		case "2":
			quitToMainMenu := abrirPastasPassoAPasso(reader, matches)
			if quitToMainMenu {
				return true
			}
		case "3":
			exibirResultadosTerminal(reader, matches)
		case "4":
			return true
		case "5":
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

	// Extrair pastas únicas dos resultados encontrados
	var dirs []string
	seen := make(map[string]bool)
	for _, m := range matches {
		var dir string
		if strings.HasPrefix(m.Kind, "diretorio") {
			dir = m.Path
		} else {
			dir = filepath.Dir(m.Path)
		}
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

func promptPositiveFilter(reader *bufio.Reader) []string {
	raw := prompt(reader, Bold+"Filtro POSITIVO no caminho/nome (separados por ';', ex: .jpg;2024 ou Enter para todos): "+Reset)
	return parseFilterTerms(raw)
}

func promptNegativeFilter(reader *bufio.Reader) []string {
	raw := prompt(reader, Bold+"Filtro NEGATIVO no caminho/nome (separados por ';', ex: tmp;backup ou Enter para nenhum): "+Reset)
	return parseFilterTerms(raw)
}

func promptTargetType(reader *bufio.Reader) TargetType {
	fmt.Println()
	fmt.Println(Bold + "Buscar por:" + Reset)
	fmt.Println("  1 - Arquivos [padrao]")
	fmt.Println("  2 - Apenas diretorios (pastas unicas)")

	ans := prompt(reader, Bold+"Escolha uma opcao (1/2 ou Enter para padrao [1]): "+Reset)
	ans = strings.TrimSpace(ans)
	if ans == "2" {
		return TargetDirectories
	}
	return TargetFiles
}

func targetLabel(target TargetType) string {
	if target == TargetDirectories {
		return "diretorios"
	}
	return "arquivos"
}

func convertToUniqueDirectoryMatches(matches []Match) []Match {
	seen := make(map[string]bool)
	var dirMatches []Match
	for _, m := range matches {
		var dir string
		if strings.HasPrefix(m.Kind, "diretorio") {
			dir = m.Path
		} else {
			dir = filepath.Dir(m.Path)
		}
		if !seen[dir] {
			seen[dir] = true
			var size int64
			var modTime time.Time
			if info, err := os.Stat(dir); err == nil {
				size = info.Size()
				modTime = info.ModTime()
			} else {
				size = m.Size
				modTime = m.ModTime
			}
			kind := "diretorio"
			if strings.Contains(m.Kind, "banco") {
				kind = "diretorio (banco)"
			}
			dirMatches = append(dirMatches, Match{
				Path:    dir,
				Kind:    kind,
				Line:    0,
				Text:    "",
				Size:    size,
				ModTime: modTime,
			})
		}
	}
	return dirMatches
}

func parseFilterTerms(raw string) []string {
	rawNormalized := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(raw, ";", " "), ",", " "), "|", " ")
	parts := strings.Fields(rawNormalized)
	var terms []string
	for _, part := range parts {
		term := strings.TrimSpace(strings.ToLower(part))
		if term != "" {
			terms = append(terms, term)
		}
	}
	return terms
}

func realizarConfiguracoes(reader *bufio.Reader) {
	for {
		currentLimit := getMaxSearchHistory()
		currentFormat := getReportFormat()
		currentMatchingMode := getMatchingMode()

		execPath, err := os.Executable()
		if err != nil {
			execPath = "Desconhecido"
		}

		dbSizeStr := "0 B"
		if info, err := os.Stat("buscatextual.db"); err == nil {
			dbSizeStr = formatSize(info.Size())
		}

		modeDesc := "Ampla (ignora acentos e maiusculas) [padrao]"
		if currentMatchingMode == "exata" {
			modeDesc = "Exata (busca literal/sensivel)"
		}

		fmt.Println()
		fmt.Println(Bold + ThemeCyan + "==================================================" + Reset)
		fmt.Println(Bold + " Configuracoes" + Reset)
		fmt.Println(Bold + ThemeCyan + "==================================================" + Reset)
		fmt.Printf(" Local de instalacao (.exe): %s%s%s\n", Bold+ThemeCyan, execPath, Reset)
		fmt.Printf(" Tamanho do banco de dados:  %s%s%s (buscatextual.db)\n", Bold+ThemeGreen, dbSizeStr, Reset)
		fmt.Printf(" Limite de arquivos historico: %s%d%s (/resultados_busca)\n", Bold+ThemeGreen, currentLimit, Reset)
		fmt.Printf(" Formato padrao do relatorio: %s%s%s\n", Bold+ThemeGreen, strings.ToUpper(currentFormat), Reset)
		fmt.Printf(" Modo de busca (correspondencia): %s%s%s\n\n", Bold+ThemeGreen, modeDesc, Reset)
		fmt.Printf("  "+ThemeYellow+"1"+Reset+" - Alterar limite de arquivos de historico\n")
		fmt.Printf("  "+ThemeYellow+"2"+Reset+" - Alterar formato do relatorio (csv, json, toml)\n")
		fmt.Printf("  "+ThemeYellow+"3"+Reset+" - Alterar modo de busca (ampla / exata)\n")
		fmt.Printf("  "+ThemeYellow+"4"+Reset+" - Voltar ao menu principal\n")
		fmt.Println(Bold + ThemeCyan + "--------------------------------------------------" + Reset)

		opcao := prompt(reader, Bold+"Escolha uma opcao: "+Reset)
		switch opcao {
		case "1":
			valStr := prompt(reader, Bold+"Digite a nova quantidade limite de arquivos de historico (padrao: 10): "+Reset)
			valStr = strings.TrimSpace(valStr)
			if valStr == "" {
				valStr = "10"
			}
			newLimit, err := strconv.Atoi(valStr)
			if err != nil || newLimit < 0 {
				fmt.Println(Red + "Valor invalido! Por favor insira um numero inteiro maior ou igual a 0." + Reset)
			} else {
				if err := saveMaxSearchHistory(newLimit); err != nil {
					fmt.Printf(Red+"Erro ao salvar configuracao no banco: %v\n"+Reset, err)
				} else {
					fmt.Printf(ThemeGreen+"Limite alterado para %d arquivos com sucesso!\n"+Reset, newLimit)
					cleanOldSearchHistory("resultados_busca")
				}
			}
		case "2":
			newFmt := promptReportFormat(reader)
			fmt.Printf(ThemeGreen+"Formato do relatorio salvo como %s com sucesso!\n"+Reset, strings.ToUpper(newFmt))
		case "3":
			newMode := promptMatchingMode(reader)
			modeLabel := "Ampla (ignora acentos e maiusculas)"
			if newMode == "exata" {
				modeLabel = "Exata"
			}
			fmt.Printf(ThemeGreen+"Modo de busca alterado para '%s' com sucesso!\n"+Reset, modeLabel)
		case "4":
			return
		default:
			fmt.Println(Red + "Opcao invalida. Tente novamente." + Reset)
		}
	}
}

func promptMatchingMode(reader *bufio.Reader) string {
	currentMode := getMatchingMode()
	fmt.Println()
	fmt.Println(Bold + "Modo de correspondencia da busca:" + Reset)
	fmt.Printf("  1 - Ampla (ignora acentos e maiusculas/minusculas) %s\n", formatSuffix("ampla", currentMode))
	fmt.Printf("  2 - Exata (busca literal/sensivel) %s\n", formatSuffix("exata", currentMode))

	opcao := prompt(reader, fmt.Sprintf(Bold+"Escolha uma opcao (1-2 ou Enter para '%s'): "+Reset, currentMode))
	opcao = strings.TrimSpace(strings.ToLower(opcao))

	selected := currentMode
	switch opcao {
	case "1", "ampla", "amplo":
		selected = "ampla"
	case "2", "exata", "exato":
		selected = "exata"
	case "":
		selected = currentMode
	default:
		fmt.Printf(Yellow+"Opcao invalida. Mantendo modo '%s'.\n"+Reset, currentMode)
		selected = currentMode
	}

	_ = saveMatchingMode(selected)
	return selected
}

func promptReportFormat(reader *bufio.Reader) string {
	currentFormat := getReportFormat()
	fmt.Println()
	fmt.Println(Bold + "Formato de saida do relatorio:" + Reset)
	fmt.Printf("  1 - CSV %s\n", formatSuffix("csv", currentFormat))
	fmt.Printf("  2 - JSON %s\n", formatSuffix("json", currentFormat))
	fmt.Printf("  3 - TOML %s\n", formatSuffix("toml", currentFormat))

	opcao := prompt(reader, fmt.Sprintf(Bold+"Escolha uma opcao (1-3 ou Enter para '%s'): "+Reset, currentFormat))
	opcao = strings.TrimSpace(strings.ToLower(opcao))

	selected := currentFormat
	switch opcao {
	case "1", "csv":
		selected = "csv"
	case "2", "json":
		selected = "json"
	case "3", "toml":
		selected = "toml"
	case "":
		selected = currentFormat
	default:
		fmt.Printf(Yellow+"Opcao invalida. Mantendo formato '%s'.\n"+Reset, currentFormat)
		selected = currentFormat
	}

	_ = saveReportFormat(selected)
	return selected
}

func formatSuffix(fmtOption string, current string) string {
	if fmtOption == current {
		return Bold + ThemeGreen + "[padrao/atual]" + Reset
	}
	return ""
}

func promptSortMode(reader *bufio.Reader) SortMode {
	for {
		fmt.Println()
		fmt.Println(Bold + "Ordenacao dos resultados da busca:" + Reset)
		fmt.Println("  1 - Por pasta/caminho [padrao]")
		fmt.Println("  2 - Por tamanho (maior para o menor)")
		fmt.Println("  3 - Por tamanho (menor para o maior)")
		fmt.Println("  4 - Por data de modificacao (mais recente)")
		fmt.Println("  5 - Por data de modificacao (mais antiga)")

		opcao := prompt(reader, Bold+"Escolha uma opcao (1-5 ou Enter para padrao): "+Reset)
		if opcao == "" {
			return SortByFolder
		}
		switch opcao {
		case "1":
			return SortByFolder
		case "2":
			return SortBySizeDesc
		case "3":
			return SortBySizeAsc
		case "4":
			return SortByDateDesc
		case "5":
			return SortByDateAsc
		default:
			fmt.Println(Red + "Opcao invalida." + Reset)
		}
	}
}

func sortMatches(matches []Match, mode SortMode) {
	switch mode {
	case SortByFolder:
		sort.Slice(matches, func(i, j int) bool {
			if matches[i].Path != matches[j].Path {
				return matches[i].Path < matches[j].Path
			}
			return matches[i].Line < matches[j].Line
		})
	case SortBySizeDesc:
		sort.Slice(matches, func(i, j int) bool {
			if matches[i].Size != matches[j].Size {
				return matches[i].Size > matches[j].Size
			}
			if matches[i].Path != matches[j].Path {
				return matches[i].Path < matches[j].Path
			}
			return matches[i].Line < matches[j].Line
		})
	case SortBySizeAsc:
		sort.Slice(matches, func(i, j int) bool {
			if matches[i].Size != matches[j].Size {
				return matches[i].Size < matches[j].Size
			}
			if matches[i].Path != matches[j].Path {
				return matches[i].Path < matches[j].Path
			}
			return matches[i].Line < matches[j].Line
		})
	case SortByDateDesc:
		sort.Slice(matches, func(i, j int) bool {
			if !matches[i].ModTime.Equal(matches[j].ModTime) {
				return matches[i].ModTime.After(matches[j].ModTime)
			}
			if matches[i].Path != matches[j].Path {
				return matches[i].Path < matches[j].Path
			}
			return matches[i].Line < matches[j].Line
		})
	case SortByDateAsc:
		sort.Slice(matches, func(i, j int) bool {
			if !matches[i].ModTime.Equal(matches[j].ModTime) {
				return matches[i].ModTime.Before(matches[j].ModTime)
			}
			if matches[i].Path != matches[j].Path {
				return matches[i].Path < matches[j].Path
			}
			return matches[i].Line < matches[j].Line
		})
	}
}


func runSearch(config SearchConfig) (SearchResult, error) {
	if config.MatchingMode == "" {
		config.MatchingMode = getMatchingMode()
	}

	// Pré-normaliza termos e filtros uma única vez antes de iniciar os workers
	config.normTerms = NormalizeTerms(config.Terms, config.MatchingMode)
	config.normPosFilter = NormalizeTerms(config.PositiveFilter, config.MatchingMode)
	config.normNegFilter = NormalizeTerms(config.NegativeFilter, config.MatchingMode)

	workerCount := calculateWorkerCount(config.Mode, config.BaseDir)
	jobs := make(chan string, workerCount*4)
	results := make(chan []Match, workerCount)
	reporter, err := createReport(config)
	if err != nil {
		return SearchResult{}, err
	}
	defer reporter.Close()

	// Acumulador de erros assíncrono e thread-safe para evitar qualquer deadlock
	var errMessages []string
	var errMu sync.Mutex

	// Canal para indexação assíncrona no BoltDB durante a busca
	searchIndexChan := make(chan FileMeta, 10000)
	indexDone := make(chan struct{})
	go func() {
		defer close(indexDone)
		defer func() {
			if r := recover(); r != nil {
				LogRecover(r)
			}
		}()

		if db == nil {
			for range searchIndexChan {
			}
			return
		}

		count := 0
		batchSize := 2000
		tx, err := db.Begin(true)
		if err != nil {
			LogError("Erro ao iniciar transacao assincrona no banco durante busca", err)
			for range searchIndexChan {
			}
			return
		}
		b, err := tx.CreateBucketIfNotExists([]byte("Files"))
		if err != nil {
			_ = tx.Rollback()
			LogError("Erro ao acessar bucket Files durante indexacao na busca", err)
			for range searchIndexChan {
			}
			return
		}

		for meta := range searchIndexChan {
			_ = putIndexHelper(b, meta.Path, meta.Size, meta.ModTime)
			count++
			if count%batchSize == 0 {
				_ = tx.Commit()
				tx, err = db.Begin(true)
				if err != nil {
					LogError("Erro ao renovar transacao durante indexacao na busca", err)
					for range searchIndexChan {
					}
					return
				}
				b, err = tx.CreateBucketIfNotExists([]byte("Files"))
				if err != nil {
					_ = tx.Rollback()
					LogError("Erro ao acessar bucket Files ao renovar transacao", err)
					for range searchIndexChan {
					}
					return
				}
			}
		}
		_ = tx.Commit()
	}()

	var scannedFiles atomic.Int64
	var workers sync.WaitGroup
	var collector sync.WaitGroup
	var matches []Match
	var matchMu sync.Mutex
	var liveDirSeen sync.Map

	collector.Add(1)
	go func() {
		defer collector.Done()
		defer func() {
			if r := recover(); r != nil {
				LogRecover(r)
			}
		}()

		for batch := range results {
			if len(batch) == 0 {
				continue
			}
			matchMu.Lock()
			matches = append(matches, batch...)
			matchMu.Unlock()

			if config.TargetType == TargetDirectories {
				var dirBatch []Match
				for _, m := range batch {
					dir := filepath.Dir(m.Path)
					if _, loaded := liveDirSeen.LoadOrStore(dir, true); !loaded {
						var size int64
						var modTime time.Time
						if info, err := os.Stat(dir); err == nil {
							size = info.Size()
							modTime = info.ModTime()
						} else {
							size = m.Size
							modTime = m.ModTime
						}
						dirBatch = append(dirBatch, Match{
							Path:    dir,
							Kind:    "diretorio",
							Line:    0,
							Text:    "",
							Size:    size,
							ModTime: modTime,
						})
					}
				}
				if len(dirBatch) > 0 {
					showFound(dirBatch, config.Terms, config.MatchingMode)
				}
			} else {
				showFound(batch, config.Terms, config.MatchingMode)
			}
		}
	}()

	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() {
				if r := recover(); r != nil {
					LogRecover(r)
				}
			}()

			for path := range jobs {
				func(p string) {
					defer func() {
						if r := recover(); r != nil {
							LogRecover(r)
						}
					}()
					fileMatches, err := processFile(p, config.Mode, config.normTerms, config.MatchingMode, searchIndexChan)
					if err != nil {
						LogError(fmt.Sprintf("Falha ao processar arquivo %s", p), err)
						errMu.Lock()
						if len(errMessages) < 50 {
							errMessages = append(errMessages, fmt.Sprintf("Falha ao ler arquivo %s: %v", p, err))
						}
						errMu.Unlock()
						return
					}
					if len(fileMatches) > 0 {
						results <- fileMatches
					}
				}(path)
			}
		}()
	}

	progressDone := make(chan struct{})
	go showProgress(&scannedFiles, workerCount, progressDone)

	walkErr := filepath.WalkDir(config.BaseDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			LogError(fmt.Sprintf("Erro ao percorrer caminho durante busca: %s", path), walkErr)
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
	collector.Wait()
	close(progressDone)

	errMu.Lock()
	for _, msg := range errMessages {
		fmt.Printf("\n%s%s%s\n", Red, msg, Reset)
	}
	errMu.Unlock()

	totalScanned := int(scannedFiles.Load())
	fmt.Printf("\r%sAnalisando arquivos...%s %s%d%s | workers: %s%d%s\n",
		ThemeCyan, Reset, ThemeGreen, totalScanned, Reset, ThemeYellow, workerCount, Reset)

	if config.TargetType == TargetDirectories {
		matches = convertToUniqueDirectoryMatches(matches)
	}

	sortMatches(matches, config.SortMode)

	if len(matches) == 0 {
		if err := reporter.WriteNoMatches(); err != nil {
			LogError("Erro ao escrever no_matches no relatorio", err)
			return SearchResult{}, err
		}
	} else {
		if err := reporter.Append(matches); err != nil {
			LogError("Falha ao salvar ocorrencias no relatorio", err)
			fmt.Printf("\n%sFalha ao salvar no relatorio: %v%s\n", Red, err, Reset)
		}
	}
	return SearchResult{
		Matches:      matches,
		ScannedFiles: totalScanned,
		ReportPath:   reporter.Path,
	}, walkErr
}

func shouldProcessFile(path string, config SearchConfig) bool {
	normPath := NormalizeText(path, config.MatchingMode)

	// Filtro Negativo: se contiver qualquer termo negativo, ignora
	for _, neg := range config.normNegFilter {
		if strings.Contains(normPath, neg) {
			return false
		}
	}

	// Filtro Positivo: se houver termos positivos, deve conter pelo menos um
	if len(config.normPosFilter) > 0 {
		matched := false
		for _, pos := range config.normPosFilter {
			if strings.Contains(normPath, pos) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

type reportFileInfo struct {
	path    string
	modTime time.Time
}

func cleanOldSearchHistory(reportDir string) {
	maxFiles := getMaxSearchHistory()
	if maxFiles <= 0 {
		return
	}

	entries, err := os.ReadDir(reportDir)
	if err != nil {
		return
	}

	var files []reportFileInfo
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "resultado_busca_") && (ext == ".toml" || ext == ".csv" || ext == ".json") {
			info, err := entry.Info()
			if err == nil {
				files = append(files, reportFileInfo{
					path:    filepath.Join(reportDir, entry.Name()),
					modTime: info.ModTime(),
				})
			}
		}
	}

	if len(files) < maxFiles {
		return
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	// Se a contagem atingiu ou excedeu o limite, remove os mais antigos para abrir espaço para o novo (ou após a criação)
	numToRemove := (len(files) - maxFiles) + 1
	if numToRemove > len(files) {
		numToRemove = len(files)
	}
	for i := 0; i < numToRemove; i++ {
		_ = os.Remove(files[i].path)
	}
}

func processFile(path string, mode SearchMode, normTerms []string, matchingMode string, indexChan chan<- FileMeta) ([]Match, error) {
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
		nameMatches := searchInFileName(path, normTerms, matchingMode)
		matches = append(matches, nameMatches...)
	}

	if mode == ModeContent || mode == ModeBoth {
		contentMatches, err := searchInFile(path, normTerms, matchingMode)
		if err != nil {
			return matches, err
		}
		matches = append(matches, contentMatches...)
	}

	return matches, nil
}

func searchInFileName(path string, normTerms []string, mode string) []Match {
	normName := NormalizeText(filepath.Base(path), mode)
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	for _, term := range normTerms {
		if strings.Contains(normName, term) {
			var size int64
			var modTime time.Time
			if info, err := os.Stat(absPath); err == nil {
				size = info.Size()
				modTime = info.ModTime()
			}
			return []Match{{
				Path:    absPath,
				Kind:    "nome",
				Size:    size,
				ModTime: modTime,
			}}
		}
	}

	return nil
}

func isBinaryBuffer(data []byte) bool {
	for _, b := range data {
		if b == 0x00 {
			return true
		}
	}
	return false
}

func searchInFile(path string, normTerms []string, mode string) ([]Match, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Detecção rápida de binário: lê os primeiros 512 bytes
	head := make([]byte, 512)
	n, err := file.Read(head)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if n > 0 && isBinaryBuffer(head[:n]) {
		// Arquivo binário (.exe, .dll, .db, imagens, etc): não processa busca de texto no conteúdo
		return nil, nil
	}

	// Reposiciona o cursor no início para leitura do texto
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	var size int64
	var modTime time.Time
	if info, err := file.Stat(); err == nil {
		size = info.Size()
		modTime = info.ModTime()
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	reader := bufio.NewReaderSize(file, 64*1024)
	var matches []Match
	lineNumber := 0

	for {
		lineBytes, isPrefix, err := reader.ReadLine()
		if err != nil {
			if err == io.EOF {
				break
			}
			return matches, err
		}

		lineNumber++
		matchedInLine := false
		lineSnippet := ""

		for {
			chunkStr := string(lineBytes)
			normChunk := NormalizeText(chunkStr, mode)

			if !matchedInLine {
				for _, term := range normTerms {
					if strings.Contains(normChunk, term) {
						matchedInLine = true
						if len(chunkStr) > 200 {
							lineSnippet = chunkStr[:200] + "..."
						} else {
							lineSnippet = chunkStr
						}
						break
					}
				}
			}

			if !isPrefix {
				break
			}

			lineBytes, isPrefix, err = reader.ReadLine()
			if err != nil {
				break
			}
		}

		if matchedInLine {
			matches = append(matches, Match{
				Path:    absPath,
				Kind:    "conteudo",
				Line:    lineNumber,
				Text:    lineSnippet,
				Size:    size,
				ModTime: modTime,
			})
		}
	}

	return matches, nil
}

type ReportWriter struct {
	File     *os.File
	Writer   *bufio.Writer
	Path     string
	Format   string
	Config   SearchConfig
	matches  []Match
	mu       sync.Mutex
	hasMatch bool
	closed   bool
}

func normalizeFormat(fmtStr string) string {
	fmtLower := strings.ToLower(strings.TrimSpace(fmtStr))
	if fmtLower == "csv" || fmtLower == "json" || fmtLower == "toml" {
		return fmtLower
	}
	return "csv"
}

func createReport(config SearchConfig) (*ReportWriter, error) {
	reportDir := "resultados_busca"
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		LogError("Falha ao criar diretorio de relatorios de busca", err)
		return nil, err
	}

	cleanOldSearchHistory(reportDir)

	format := normalizeFormat(config.ReportFormat)
	if config.ReportFormat == "" {
		format = getReportFormat()
	}

	timestamp := strings.ReplaceAll(time.Now().Format("20060102_150405.000"), ".", "_")
	fileName := fmt.Sprintf("resultado_busca_%s.%s", timestamp, format)
	filePath, err := filepath.Abs(filepath.Join(reportDir, fileName))
	if err != nil {
		LogError("Falha ao resolver caminho absoluto do relatorio", err)
		return nil, err
	}

	file, err := os.Create(filePath)
	if err != nil {
		LogError(fmt.Sprintf("Falha ao criar arquivo de relatorio %s", filePath), err)
		return nil, err
	}

	writer := bufio.NewWriter(file)
	reporter := &ReportWriter{
		File:   file,
		Writer: writer,
		Path:   filePath,
		Format: format,
		Config: config,
	}

	switch format {
	case "toml":
		fmt.Fprintf(writer, "[metadados]\n")
		fmt.Fprintf(writer, "data_inicio = %q\n", time.Now().Format("2006-01-02 15:04:05.000"))
		fmt.Fprintf(writer, "pasta_base = %q\n", config.BaseDir)
		fmt.Fprintf(writer, "modo = %q\n", modeLabel(config.Mode))
		fmt.Fprintf(writer, "modo_busca = %q\n", config.MatchingMode)
		fmt.Fprintf(writer, "alvo = %q\n", targetLabel(config.TargetType))
		fmt.Fprintf(writer, "termos = [%s]\n", formatTOMLStringList(config.Terms))

		if len(config.PositiveFilter) == 0 {
			fmt.Fprintf(writer, "filtros_positivos = \"todos\"\n")
		} else {
			fmt.Fprintf(writer, "filtros_positivos = [%s]\n", formatTOMLStringList(config.PositiveFilter))
		}
		if len(config.NegativeFilter) == 0 {
			fmt.Fprintf(writer, "filtros_negativos = \"nenhum\"\n")
		} else {
			fmt.Fprintf(writer, "filtros_negativos = [%s]\n", formatTOMLStringList(config.NegativeFilter))
		}
		fmt.Fprintf(writer, "\n")
		_ = writer.Flush()

	case "csv":
		fmt.Fprintf(writer, "# Metadados\n")
		fmt.Fprintf(writer, "# Data Inicio: %s\n", time.Now().Format("2006-01-02 15:04:05.000"))
		fmt.Fprintf(writer, "# Pasta Base: %s\n", config.BaseDir)
		fmt.Fprintf(writer, "# Modo: %s\n", modeLabel(config.Mode))
		fmt.Fprintf(writer, "# Modo Busca: %s\n", config.MatchingMode)
		fmt.Fprintf(writer, "# Alvo: %s\n", targetLabel(config.TargetType))
		fmt.Fprintf(writer, "# Termos: %s\n", strings.Join(config.Terms, ", "))

		if len(config.PositiveFilter) == 0 {
			fmt.Fprintf(writer, "# Filtros Positivos: todos\n")
		} else {
			fmt.Fprintf(writer, "# Filtros Positivos: %s\n", strings.Join(config.PositiveFilter, ", "))
		}
		if len(config.NegativeFilter) == 0 {
			fmt.Fprintf(writer, "# Filtros Negativos: nenhum\n")
		} else {
			fmt.Fprintf(writer, "# Filtros Negativos: %s\n", strings.Join(config.NegativeFilter, ", "))
		}
		fmt.Fprintf(writer, "\n")
		_ = writer.Flush()

		csvWriter := csv.NewWriter(writer)
		_ = csvWriter.Write([]string{"Arquivo", "Tipo", "Linha", "Trecho", "Tamanho_Bytes", "Data_Modificacao"})
		csvWriter.Flush()
		_ = writer.Flush()

	case "json":
		// Para JSON, acumulamos os registros em memoria e escrevemos a estrutura completa no Close().
	}

	return reporter, nil
}

func (r *ReportWriter) Append(matches []Match) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.matches = append(r.matches, matches...)
	if len(matches) > 0 {
		r.hasMatch = true
	}

	switch r.Format {
	case "toml":
		for _, match := range matches {
			fmt.Fprintf(r.Writer, "[[resultados]]\n")
			fmt.Fprintf(r.Writer, "arquivo = %q\n", match.Path)
			fmt.Fprintf(r.Writer, "tipo = %q\n", match.Kind)
			if match.Kind == "conteudo" {
				fmt.Fprintf(r.Writer, "linha = %d\n", match.Line)
				fmt.Fprintf(r.Writer, "trecho = %q\n", match.Text)
			}
			fmt.Fprintf(r.Writer, "tamanho_bytes = %d\n", match.Size)
			if !match.ModTime.IsZero() {
				fmt.Fprintf(r.Writer, "data_modificacao = %q\n", match.ModTime.Format("2006-01-02 15:04:05"))
			} else {
				fmt.Fprintf(r.Writer, "data_modificacao = %q\n", "")
			}
			fmt.Fprintf(r.Writer, "\n")
		}
		return r.Writer.Flush()

	case "csv":
		csvWriter := csv.NewWriter(r.Writer)
		for _, match := range matches {
			lineStr := ""
			if match.Kind == "conteudo" {
				lineStr = strconv.Itoa(match.Line)
			}
			dateStr := ""
			if !match.ModTime.IsZero() {
				dateStr = match.ModTime.Format("2006-01-02 15:04:05")
			}
			rec := []string{
				match.Path,
				match.Kind,
				lineStr,
				match.Text,
				strconv.FormatInt(match.Size, 10),
				dateStr,
			}
			if err := csvWriter.Write(rec); err != nil {
				return err
			}
		}
		csvWriter.Flush()
		return r.Writer.Flush()

	case "json":
		return nil
	}

	return nil
}

func (r *ReportWriter) WriteNoMatches() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.hasMatch {
		return nil
	}

	switch r.Format {
	case "toml":
		if _, err := r.Writer.WriteString("# Nenhuma ocorrencia encontrada.\nresultados = []\n"); err != nil {
			return err
		}
		return r.Writer.Flush()

	case "csv":
		if _, err := r.Writer.WriteString("# Nenhuma ocorrencia encontrada.\n"); err != nil {
			return err
		}
		return r.Writer.Flush()

	case "json":
		return nil
	}

	return nil
}

type ReportMetadataJSON struct {
	DataInicio       string   `json:"data_inicio"`
	PastaBase        string   `json:"pasta_base"`
	Modo             string   `json:"modo"`
	ModoBusca        string   `json:"modo_busca"`
	Alvo             string   `json:"alvo"`
	Termos           []string `json:"termos"`
	FiltrosPositivos any      `json:"filtros_positivos"`
	FiltrosNegativos any      `json:"filtros_negativos"`
}

type ReportMatchJSON struct {
	Arquivo         string  `json:"arquivo"`
	Tipo            string  `json:"tipo"`
	Linha           *int    `json:"linha,omitempty"`
	Trecho          *string `json:"trecho,omitempty"`
	TamanhoBytes    int64   `json:"tamanho_bytes"`
	DataModificacao string  `json:"data_modificacao"`
}

type ReportJSON struct {
	Metadados  ReportMetadataJSON `json:"metadados"`
	Resultados []ReportMatchJSON  `json:"resultados"`
}

func (r *ReportWriter) writeJSON() error {
	var posFilter any = "todos"
	if len(r.Config.PositiveFilter) > 0 {
		posFilter = r.Config.PositiveFilter
	}

	var negFilter any = "nenhum"
	if len(r.Config.NegativeFilter) > 0 {
		negFilter = r.Config.NegativeFilter
	}

	meta := ReportMetadataJSON{
		DataInicio:       time.Now().Format("2006-01-02 15:04:05.000"),
		PastaBase:        r.Config.BaseDir,
		Modo:             modeLabel(r.Config.Mode),
		ModoBusca:        r.Config.MatchingMode,
		Alvo:             targetLabel(r.Config.TargetType),
		Termos:           r.Config.Terms,
		FiltrosPositivos: posFilter,
		FiltrosNegativos: negFilter,
	}

	resultados := make([]ReportMatchJSON, 0, len(r.matches))
	for _, m := range r.matches {
		var linePtr *int
		var textPtr *string
		if m.Kind == "conteudo" {
			lineVal := m.Line
			textVal := m.Text
			linePtr = &lineVal
			textPtr = &textVal
		}

		modTimeStr := ""
		if !m.ModTime.IsZero() {
			modTimeStr = m.ModTime.Format("2006-01-02 15:04:05")
		}

		resultados = append(resultados, ReportMatchJSON{
			Arquivo:         m.Path,
			Tipo:            m.Kind,
			Linha:           linePtr,
			Trecho:          textPtr,
			TamanhoBytes:    m.Size,
			DataModificacao: modTimeStr,
		})
	}

	rep := ReportJSON{
		Metadados:  meta,
		Resultados: resultados,
	}

	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		LogError("Falha ao serializar relatorio JSON", err)
		return err
	}

	_, err = r.Writer.Write(data)
	if err != nil {
		LogError("Falha ao escrever relatorio JSON em disco", err)
		return err
	}
	_, err = r.Writer.WriteString("\n")
	return err
}

func (r *ReportWriter) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true

	if r.Format == "json" {
		if err := r.writeJSON(); err != nil {
			_ = r.Writer.Flush()
			_ = r.File.Close()
			return err
		}
	}

	if err := r.Writer.Flush(); err != nil {
		_ = r.File.Close()
		return err
	}
	return r.File.Close()
}

func formatMatch(match Match, terms []string, mode string) string {
	highlightedPath := highlightTerms(match.Path, terms, mode)
	if strings.HasPrefix(match.Kind, "diretorio") {
		return fmt.Sprintf("%sPasta:%s %s | %s[Diretorio]%s", ThemeCyan, Reset, highlightedPath, Bold+ThemeGreen, Reset)
	}
	line := fmt.Sprintf("%sArquivo:%s %s", ThemeCyan, Reset, highlightedPath)
	if match.Kind == "conteudo" {
		highlightedText := highlightTerms(match.Text, terms, mode)
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

func showFound(batch []Match, terms []string, mode string) {
	for _, match := range batch {
		fmt.Printf("\n%sEncontrado:%s %s\n", Bold+ThemeGreen, Reset, formatMatch(match, terms, mode))
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

func calculateWorkerCount(mode SearchMode, baseDir string) int {
	cpuCount := runtime.NumCPU()
	if cpuCount < 1 {
		cpuCount = 1
	}

	diskID := getDiskID(baseDir)
	optimalThreads := getDiskOptimalThreads(diskID)

	if optimalThreads > 0 {
		// Se o benchmark de disco já determinou a taxa ótima de I/O
		if mode == ModeContent || mode == ModeBoth {
			// Para conteúdo, balanceia a carga entre I/O e CPU
			if optimalThreads > cpuCount {
				return cpuCount
			}
			return optimalThreads
		}
		// Para busca de nome / metadata
		return optimalThreads
	}

	// Fallback inteligente caso ainda não haja benchmark calibrado
	if mode == ModeContent || mode == ModeBoth {
		if cpuCount <= 2 {
			return cpuCount
		}
		if cpuCount > 8 {
			return 8
		}
		return cpuCount
	}

	if cpuCount > 16 {
		return 16
	}
	return cpuCount
}

func showProgress(scannedFiles *atomic.Int64, workerCount int, done <-chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			LogRecover(r)
		}
	}()

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

func exibirResultadosTerminal(reader *bufio.Reader, matches []Match) {
	if len(matches) == 0 {
		fmt.Println(Yellow + "Nenhum resultado para exibir." + Reset)
		return
	}

	fmt.Println()
	fmt.Println(Bold + "Formato de exibicao dos resultados:" + Reset)
	fmt.Printf("  "+ThemeYellow+"1"+Reset+" - Simplificado (uma linha por arquivo/pasta, sem metadados) [%sPadrao%s]\n", Bold+ThemeGreen, Reset)
	fmt.Printf("  "+ThemeYellow+"2"+Reset+" - Tabela completa (tipo, linha, tamanho, data, trecho)\n")
	formato := strings.TrimSpace(prompt(reader, Bold+"Escolha o formato (1/2) [padrao: 1]: "+Reset))

	qtdStr := strings.TrimSpace(prompt(reader, Bold+"Quantidade de linhas a exibir (padrao: 10, maximo 100): "+Reset))
	limit := 10
	if qtdStr != "" {
		if val, err := strconv.Atoi(qtdStr); err == nil {
			if val <= 0 {
				fmt.Println(Yellow + "Quantidade invalida ou zero. Exibicao cancelada." + Reset)
				return
			}
			limit = val
		}
	}

	if limit > 100 {
		limit = 100
	}

	if formato == "2" || strings.ToLower(formato) == "tabela" {
		exibirTabelaResultados(matches, limit)
	} else {
		exibirResultadosSimplificados(matches, limit)
	}
}

func exibirResultadosSimplificados(matches []Match, limit int) {
	if len(matches) == 0 {
		return
	}

	if limit > len(matches) {
		limit = len(matches)
	}

	fmt.Println()
	fmt.Printf("Exibindo os primeiros %d de %d resultados (simplificado):\n", limit, len(matches))
	fmt.Println(Bold + ThemeCyan + "--------------------------------------------------" + Reset)
	for i := 0; i < limit; i++ {
		fmt.Println(matches[i].Path)
	}
	fmt.Println(Bold + ThemeCyan + "--------------------------------------------------" + Reset)
}

func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	kb := float64(bytes) / 1024.0
	if kb < 1024 {
		return fmt.Sprintf("%.1f KB", kb)
	}
	mb := kb / 1024.0
	return fmt.Sprintf("%.1f MB", mb)
}

func exibirTabelaResultados(matches []Match, limit int) {
	if len(matches) == 0 {
		return
	}

	if limit > len(matches) {
		limit = len(matches)
	}

	fmt.Println()
	fmt.Println(Bold + Yellow + "[Dica] E recomendavel maximizar o terminal para caber mais informacoes sem quebra de linha!" + Reset)
	fmt.Printf("\nExibindo os primeiros %d de %d resultados:\n", limit, len(matches))
	
	// Define largura dinamica da coluna de caminho para nao cortar o endereco do arquivo
	pathHeader := "Caminho do Arquivo / Pasta"
	pathWidth := len(pathHeader)
	for i := 0; i < limit; i++ {
		if len(matches[i].Path) > pathWidth {
			pathWidth = len(matches[i].Path)
		}
	}

	kindWidth := 8
	lineWidth := 6
	sizeWidth := 9
	dateWidth := 19
	textWidth := 30
	
	header := fmt.Sprintf("%-*s | %-*s | %-*s | %-*s | %-*s | %-s",
		pathWidth, pathHeader,
		kindWidth, "Tipo",
		lineWidth, "Linha",
		sizeWidth, "Tamanho",
		dateWidth, "Data Modificacao",
		"Trecho (Snippet)")
	
	totalWidth := len(header)
	
	fmt.Println(Bold + ThemeCyan + strings.Repeat("-", totalWidth) + Reset)
	fmt.Println(header)
	fmt.Println(Bold + ThemeCyan + strings.Repeat("-", totalWidth) + Reset)

	for i := 0; i < limit; i++ {
		m := matches[i]
		pathStr := m.Path
		
		var lineStr string
		if m.Kind == "conteudo" {
			lineStr = fmt.Sprintf("%d", m.Line)
		} else {
			lineStr = "-"
		}
		
		sizeStr := formatSize(m.Size)
		dateStr := ""
		if !m.ModTime.IsZero() {
			dateStr = m.ModTime.Format("2006-01-02 15:04:05")
		} else {
			dateStr = "-"
		}

		kindLabel := m.Kind
		if kindLabel == "nome (banco)" {
			kindLabel = "nome"
		} else if kindLabel == "diretorio (banco)" {
			kindLabel = "diretorio"
		}

		snippet := strings.TrimSpace(m.Text)
		if len(snippet) > textWidth {
			snippet = snippet[:textWidth-3] + "..."
		}
		if snippet == "" {
			snippet = "-"
		}

		fmt.Printf("%-*s | %-*s | %-*s | %-*s | %-*s | %-s\n",
			pathWidth, pathStr,
			kindWidth, kindLabel,
			lineWidth, lineStr,
			sizeWidth, sizeStr,
			dateWidth, dateStr,
			snippet)
	}
	fmt.Println(Bold + ThemeCyan + strings.Repeat("-", totalWidth) + Reset)
}
