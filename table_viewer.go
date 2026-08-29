package main

import (
	"bufio"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed web/index.html
var tableViewerHTML string

//go:embed icon.png
var appIconPNG []byte

// Estruturas de dados para a API Web
type ReportListItem struct {
	FileName         string    `json:"filename"`
	Path             string    `json:"path"`
	SizeBytes        int64     `json:"size_bytes"`
	SizeFormatted    string    `json:"size_formatted"`
	ModTime          time.Time `json:"mod_time"`
	ModTimeFormatted string    `json:"mod_time_formatted"`
	DataInicio       string    `json:"data_inicio"`
	PastaBase        string    `json:"pasta_base"`
	Modo             string    `json:"modo"`
	ModoBusca        string    `json:"modo_busca"`
	Alvo             string    `json:"alvo"`
	Termos           []string  `json:"termos"`
	FiltrosPositivos string    `json:"filtros_positivos"`
	FiltrosNegativos string    `json:"filtros_negativos"`
	TotalLinhas      int       `json:"total_linhas"`
}

type ReportMetadata struct {
	DataInicio       string   `json:"data_inicio"`
	PastaBase        string   `json:"pasta_base"`
	Modo             string   `json:"modo"`
	ModoBusca        string   `json:"modo_busca"`
	Alvo             string   `json:"alvo"`
	Termos           []string `json:"termos"`
	FiltrosPositivos string   `json:"filtros_positivos"`
	FiltrosNegativos string   `json:"filtros_negativos"`
	TotalLinhas      int      `json:"total_linhas"`
	ArquivoRelatorio string   `json:"arquivo_relatorio"`
	CaminhoCompleto  string   `json:"caminho_completo"`
	TamanhoBytes     int64    `json:"tamanho_bytes"`
	TamanhoFormatado string   `json:"tamanho_formatado"`
}

type ReportTableRow struct {
	ID               int    `json:"id"`
	Arquivo          string `json:"arquivo"`
	NomeArquivo      string `json:"nome_arquivo"`
	Pasta            string `json:"pasta"`
	Tipo             string `json:"tipo"`
	Linha            string `json:"linha"`
	LinhaNum         int    `json:"linha_num"`
	Trecho           string `json:"trecho"`
	TamanhoBytes     int64  `json:"tamanho_bytes"`
	TamanhoFormatado string `json:"tamanho_formatado"`
	DataModificacao  string `json:"data_modificacao"`
}

type ReportDataResponse struct {
	Metadata ReportMetadata   `json:"metadata"`
	Rows     []ReportTableRow `json:"rows"`
	Error    string           `json:"error,omitempty"`
}

type ActionRequest struct {
	Path string `json:"path"`
}

type ActionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Estruturas para iniciar busca pela Web
type WebSearchRequest struct {
	Type         string   `json:"type"` // "disk" ou "db"
	BaseDir      string   `json:"baseDir"`
	Terms        []string `json:"terms"`
	Mode         int      `json:"mode"`       // 1: Nome, 2: Conteudo, 3: Ambos
	TargetType   int      `json:"targetType"` // 0: Arquivos, 1: Diretorios
	PosFilter    []string `json:"posFilter"`
	NegFilter    []string `json:"negFilter"`
	MatchingMode string   `json:"matchingMode"`
}

type WebSearchStatus struct {
	Active       bool   `json:"active"`
	Finished     bool   `json:"finished"`
	Type         string `json:"type"`
	ScannedFiles int64  `json:"scannedFiles"`
	MatchesCount int    `json:"matchesCount"`
	ElapsedMs    int64  `json:"elapsedMs"`
	ReportFile   string `json:"reportFile,omitempty"`
	Error        string `json:"error,omitempty"`
}

// Estrutura para visualização / preview de arquivo
type FilePreviewResponse struct {
	Path             string   `json:"path"`
	FileName         string   `json:"fileName"`
	Extension        string   `json:"extension"`
	SizeBytes        int64    `json:"sizeBytes"`
	SizeFormatted    string   `json:"sizeFormatted"`
	ModTime          string   `json:"modTime"`
	Category         string   `json:"category"` // "code", "text", "image", "audio", "video", "pdf", "binary"
	Content          string   `json:"content,omitempty"`
	Lines            []string `json:"lines,omitempty"`
	HighlightLine    int      `json:"highlightLine,omitempty"`
	RawURL           string   `json:"rawUrl,omitempty"`
	MimeType         string   `json:"mimeType,omitempty"`
	Error            string   `json:"error,omitempty"`
	IsLargeTruncated bool     `json:"isLargeTruncated,omitempty"`
}

var (
	tableViewerServerPort int
	tableViewerServerOnce sync.Once
	tableViewerServerURL  string
	tableViewerMutex      sync.Mutex

	// Monitor de busca ativa em segundo plano
	activeSearchMutex     sync.Mutex
	activeSearchStatus    WebSearchStatus
	activeSearchCancel    chan struct{}
	activeSearchStartTime time.Time
	activeSearchScanned   atomic.Int64
	activeSearchMatches   atomic.Int64
)

// openFolderInExplorer abre a pasta no Windows Explorer destacando o arquivo se aplicável
func openFolderInExplorer(path string) error {
	cleanPath := filepath.Clean(path)
	switch runtime.GOOS {
	case "windows":
		fi, err := os.Stat(cleanPath)
		if err == nil && fi.IsDir() {
			return exec.Command("cmd", "/c", "start", "", cleanPath).Run()
		}
		// Se for arquivo ou caminho completo, usa /select para destacar o item no Explorer
		return exec.Command("explorer.exe", fmt.Sprintf("/select,%s", cleanPath)).Run()
	case "darwin":
		return exec.Command("open", "-R", cleanPath).Run()
	default: // linux e outros
		dir := cleanPath
		if fi, err := os.Stat(cleanPath); err == nil && !fi.IsDir() {
			dir = filepath.Dir(cleanPath)
		}
		return exec.Command("xdg-open", dir).Run()
	}
}

// parseCSVReport lê o arquivo CSV do disco sob demanda e extrai metadados e linhas
func parseCSVReport(filePath string) (*ReportDataResponse, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	file, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("nao foi possivel abrir o arquivo CSV: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	var fileSizeBytes int64
	if err == nil {
		fileSizeBytes = fileInfo.Size()
	}

	metadata := ReportMetadata{
		ArquivoRelatorio: filepath.Base(absPath),
		CaminhoCompleto:  absPath,
		TamanhoBytes:     fileSizeBytes,
		TamanhoFormatado: formatSize(fileSizeBytes),
		FiltrosPositivos: "todos",
		FiltrosNegativos: "nenhum",
	}

	var rows []ReportTableRow
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var csvDataLines []string
	isMetadataHeader := true

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if isMetadataHeader {
			if strings.HasPrefix(trimmed, "#") {
				content := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
				if strings.HasPrefix(content, "Data Inicio:") {
					metadata.DataInicio = strings.TrimSpace(strings.TrimPrefix(content, "Data Inicio:"))
				} else if strings.HasPrefix(content, "Pasta Base:") {
					metadata.PastaBase = strings.TrimSpace(strings.TrimPrefix(content, "Pasta Base:"))
				} else if strings.HasPrefix(content, "Modo:") {
					metadata.Modo = strings.TrimSpace(strings.TrimPrefix(content, "Modo:"))
				} else if strings.HasPrefix(content, "Modo Busca:") {
					metadata.ModoBusca = strings.TrimSpace(strings.TrimPrefix(content, "Modo Busca:"))
				} else if strings.HasPrefix(content, "Alvo:") {
					metadata.Alvo = strings.TrimSpace(strings.TrimPrefix(content, "Alvo:"))
				} else if strings.HasPrefix(content, "Termos:") {
					termsStr := strings.TrimSpace(strings.TrimPrefix(content, "Termos:"))
					parts := strings.Split(termsStr, ",")
					for _, p := range parts {
						term := strings.TrimSpace(p)
						if term != "" {
							metadata.Termos = append(metadata.Termos, term)
						}
					}
				} else if strings.HasPrefix(content, "Filtros Positivos:") {
					metadata.FiltrosPositivos = strings.TrimSpace(strings.TrimPrefix(content, "Filtros Positivos:"))
				} else if strings.HasPrefix(content, "Filtros Negativos:") {
					metadata.FiltrosNegativos = strings.TrimSpace(strings.TrimPrefix(content, "Filtros Negativos:"))
				}
				continue
			}

			if trimmed == "" {
				continue
			}

			isMetadataHeader = false
			continue
		}

		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			csvDataLines = append(csvDataLines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("erro ao ler linhas do CSV: %w", err)
	}

	csvContent := strings.Join(csvDataLines, "\n")
	if len(csvDataLines) > 0 {
		csvReader := csv.NewReader(strings.NewReader(csvContent))
		csvReader.FieldsPerRecord = -1
		records, err := csvReader.ReadAll()
		if err == nil {
			rowID := 1
			for _, rec := range records {
				if len(rec) == 0 {
					continue
				}
				row := ReportTableRow{
					ID: rowID,
				}
				if len(rec) > 0 {
					row.Arquivo = rec[0]
					row.NomeArquivo = filepath.Base(rec[0])
					row.Pasta = filepath.Dir(rec[0])
				}
				if len(rec) > 1 {
					row.Tipo = rec[1]
				}
				if len(rec) > 2 {
					row.Linha = rec[2]
					if lNum, err := strconv.Atoi(rec[2]); err == nil {
						row.LinhaNum = lNum
					}
				}
				if len(rec) > 3 {
					row.Trecho = rec[3]
				}
				if len(rec) > 4 {
					if sizeBytes, err := strconv.ParseInt(rec[4], 10, 64); err == nil {
						row.TamanhoBytes = sizeBytes
						row.TamanhoFormatado = formatSize(sizeBytes)
					} else {
						row.TamanhoFormatado = rec[4]
					}
				}
				if len(rec) > 5 {
					row.DataModificacao = rec[5]
				}

				rows = append(rows, row)
				rowID++
			}
		}
	}

	metadata.TotalLinhas = len(rows)

	return &ReportDataResponse{
		Metadata: metadata,
		Rows:     rows,
	}, nil
}

// listCSVReports varre o diretório de relatórios e extrai dados resumidos de cada .csv
func listCSVReports(reportDir string) ([]ReportListItem, error) {
	if _, err := os.Stat(reportDir); os.IsNotExist(err) {
		return []ReportListItem{}, nil
	}

	entries, err := os.ReadDir(reportDir)
	if err != nil {
		return nil, fmt.Errorf("falha ao ler diretorio %s: %w", reportDir, err)
	}

	var reports []ReportListItem

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".csv") {
			continue
		}

		filePath := filepath.Join(reportDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		item := ReportListItem{
			FileName:         entry.Name(),
			Path:             filePath,
			SizeBytes:        info.Size(),
			SizeFormatted:    formatSize(info.Size()),
			ModTime:          info.ModTime(),
			ModTimeFormatted: info.ModTime().Format("2006-01-02 15:04:05"),
		}

		if f, err := os.Open(filePath); err == nil {
			scanner := bufio.NewScanner(f)
			lineCount := 0
			dataCount := 0
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if strings.HasPrefix(line, "#") {
					content := strings.TrimSpace(strings.TrimPrefix(line, "#"))
					if strings.HasPrefix(content, "Data Inicio:") {
						item.DataInicio = strings.TrimSpace(strings.TrimPrefix(content, "Data Inicio:"))
					} else if strings.HasPrefix(content, "Pasta Base:") {
						item.PastaBase = strings.TrimSpace(strings.TrimPrefix(content, "Pasta Base:"))
					} else if strings.HasPrefix(content, "Modo:") {
						item.Modo = strings.TrimSpace(strings.TrimPrefix(content, "Modo:"))
					} else if strings.HasPrefix(content, "Modo Busca:") {
						item.ModoBusca = strings.TrimSpace(strings.TrimPrefix(content, "Modo Busca:"))
					} else if strings.HasPrefix(content, "Alvo:") {
						item.Alvo = strings.TrimSpace(strings.TrimPrefix(content, "Alvo:"))
					} else if strings.HasPrefix(content, "Termos:") {
						termsStr := strings.TrimSpace(strings.TrimPrefix(content, "Termos:"))
						parts := strings.Split(termsStr, ",")
						for _, p := range parts {
							term := strings.TrimSpace(p)
							if term != "" {
								item.Termos = append(item.Termos, term)
							}
						}
					} else if strings.HasPrefix(content, "Filtros Positivos:") {
						item.FiltrosPositivos = strings.TrimSpace(strings.TrimPrefix(content, "Filtros Positivos:"))
					} else if strings.HasPrefix(content, "Filtros Negativos:") {
						item.FiltrosNegativos = strings.TrimSpace(strings.TrimPrefix(content, "Filtros Negativos:"))
					}
				} else if line != "" {
					dataCount++
				}
				lineCount++
			}
			f.Close()
			if dataCount > 1 {
				item.TotalLinhas = dataCount - 1
			}
		}

		reports = append(reports, item)
	}

	sort.Slice(reports, func(i, j int) bool {
		return reports[i].ModTime.After(reports[j].ModTime)
	})

	return reports, nil
}

// detectFileCategory determina o tipo e categoria do arquivo para exibição no navegador
func detectFileCategory(path string) (category string, mimeType string) {
	ext := strings.ToLower(filepath.Ext(path))

	// Extensões de Código e Texto
	codeExts := map[string]string{
		".go": "text/x-go", ".py": "text/x-python", ".js": "text/javascript", ".ts": "text/typescript",
		".html": "text/html", ".htm": "text/html", ".css": "text/css", ".json": "application/json",
		".yaml": "text/yaml", ".yml": "text/yaml", ".toml": "text/toml", ".xml": "text/xml",
		".txt": "text/plain", ".md": "text/markdown", ".log": "text/plain", ".csv": "text/csv",
		".sql": "text/x-sql", ".sh": "text/x-shellscript", ".bat": "text/plain", ".cmd": "text/plain",
		".ps1": "text/plain", ".c": "text/x-c", ".cpp": "text/x-c++", ".h": "text/x-c",
		".java": "text/x-java", ".php": "text/x-php", ".rb": "text/x-ruby", ".rs": "text/x-rust",
		".dart": "text/x-dart", ".ini": "text/plain", ".env": "text/plain", ".cfg": "text/plain",
		".conf": "text/plain", ".lua": "text/x-lua", ".swift": "text/x-swift", ".kt": "text/x-kotlin",
	}

	if m, ok := codeExts[ext]; ok {
		return "code", m
	}

	// Imagens
	imgExts := map[string]string{
		".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
		".webp": "image/webp", ".svg": "image/svg+xml", ".bmp": "image/bmp", ".ico": "image/x-icon",
	}
	if m, ok := imgExts[ext]; ok {
		return "image", m
	}

	// Áudio
	audioExts := map[string]string{
		".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg", ".m4a": "audio/mp4", ".flac": "audio/flac",
	}
	if m, ok := audioExts[ext]; ok {
		return "audio", m
	}

	// Vídeo
	videoExts := map[string]string{
		".mp4": "video/mp4", ".webm": "video/webm", ".ogv": "video/ogg", ".mov": "video/quicktime",
	}
	if m, ok := videoExts[ext]; ok {
		return "video", m
	}

	// PDF
	if ext == ".pdf" {
		return "pdf", "application/pdf"
	}

	m := mime.TypeByExtension(ext)
	if strings.HasPrefix(m, "text/") {
		return "text", m
	}

	return "binary", "application/octet-stream"
}

// buildFilePreview gera a resposta estruturada para o modal de preview
func buildFilePreview(filePath string, lineNum int) (*FilePreviewResponse, error) {
	cleanPath := filepath.Clean(filePath)
	info, err := os.Stat(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("arquivo nao encontrado: %w", err)
	}

	cat, mimeType := detectFileCategory(cleanPath)
	resp := &FilePreviewResponse{
		Path:          cleanPath,
		FileName:      info.Name(),
		Extension:     filepath.Ext(cleanPath),
		SizeBytes:     info.Size(),
		SizeFormatted: formatSize(info.Size()),
		ModTime:       info.ModTime().Format("2006-01-02 15:04:05"),
		Category:      cat,
		MimeType:      mimeType,
		HighlightLine: lineNum,
		RawURL:        fmt.Sprintf("/api/raw-file?path=%s", url.QueryEscape(cleanPath)),
	}

	if cat == "code" || cat == "text" {
		const maxReadBytes = 4 * 1024 * 1024 // 4 MB máximo para visualização suave no navegador
		file, err := os.Open(cleanPath)
		if err != nil {
			return nil, err
		}
		defer file.Close()

		var lines []string
		scanner := bufio.NewScanner(file)
		buf := make([]byte, 1024*1024)
		scanner.Buffer(buf, 10*1024*1024)

		bytesRead := 0
		for scanner.Scan() {
			text := scanner.Text()
			lines = append(lines, text)
			bytesRead += len(text) + 1
			if bytesRead > maxReadBytes {
				resp.IsLargeTruncated = true
				break
			}
		}
		resp.Lines = lines
		resp.Content = strings.Join(lines, "\n")
	}

	return resp, nil
}

// startTableViewerServer inicia o servidor HTTP local caso ainda não esteja rodando
func startTableViewerServer() (string, error) {
	tableViewerMutex.Lock()
	defer tableViewerMutex.Unlock()

	if tableViewerServerURL != "" {
		return tableViewerServerURL, nil
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("falha ao iniciar listener local: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	tableViewerServerPort = port
	tableViewerServerURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	mux := http.NewServeMux()

	// Rota principal: Single Page Application
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(tableViewerHTML))
	})

	// API: Ícone da aplicação (favicon e navbar)
	mux.HandleFunc("/api/icon", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(appIconPNG)
	})

	// API: Listar todos os relatórios disponíveis
	mux.HandleFunc("/api/reports", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		reports, err := listCSVReports("resultados_busca")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(reports)
	})

	// API: Obter dados de um relatório específico
	mux.HandleFunc("/api/report", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fileName := r.URL.Query().Get("file")
		if fileName == "" {
			reports, err := listCSVReports("resultados_busca")
			if err != nil || len(reports) == 0 {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Nenhum relatorio CSV encontrado na pasta resultados_busca"})
				return
			}
			fileName = reports[0].FileName
		}

		var targetPath string
		if filepath.IsAbs(fileName) {
			targetPath = fileName
		} else {
			targetPath = filepath.Join("resultados_busca", filepath.Base(fileName))
		}

		data, err := parseCSVReport(targetPath)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		_ = json.NewEncoder(w).Encode(data)
	})

	// API: Abrir arquivo no programa padrão do sistema
	mux.HandleFunc("/api/open-file", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(ActionResponse{Success: false, Error: "Metodo nao permitido"})
			return
		}

		var req ActionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Path) == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(ActionResponse{Success: false, Error: "Caminho do arquivo invalido"})
			return
		}

		go openFile(req.Path)
		_ = json.NewEncoder(w).Encode(ActionResponse{Success: true, Message: fmt.Sprintf("Arquivo aberto: %s", req.Path)})
	})

	// API: Abrir pasta no Explorer / gerenciador de arquivos
	mux.HandleFunc("/api/open-folder", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(ActionResponse{Success: false, Error: "Metodo nao permitido"})
			return
		}

		var req ActionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Path) == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(ActionResponse{Success: false, Error: "Caminho invalido"})
			return
		}

		go func() {
			_ = openFolderInExplorer(req.Path)
		}()
		_ = json.NewEncoder(w).Encode(ActionResponse{Success: true, Message: fmt.Sprintf("Pasta aberta: %s", req.Path)})
	})

	// API: Visualizador / Preview estruturado de arquivos
	mux.HandleFunc("/api/preview", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		filePath := r.URL.Query().Get("path")
		if filePath == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Caminho do arquivo nao informado"})
			return
		}

		lineStr := r.URL.Query().Get("line")
		lineNum := 0
		if l, err := strconv.Atoi(lineStr); err == nil {
			lineNum = l
		}

		preview, err := buildFilePreview(filePath, lineNum)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		_ = json.NewEncoder(w).Encode(preview)
	})

	// API: Stream de arquivos brutos (imagens, áudio, vídeo, PDFs)
	mux.HandleFunc("/api/raw-file", func(w http.ResponseWriter, r *http.Request) {
		filePath := r.URL.Query().Get("path")
		if filePath == "" {
			http.Error(w, "Arquivo nao informado", http.StatusBadRequest)
			return
		}

		cleanPath := filepath.Clean(filePath)
		if _, err := os.Stat(cleanPath); err != nil {
			http.Error(w, "Arquivo nao encontrado", http.StatusNotFound)
			return
		}

		_, mimeType := detectFileCategory(cleanPath)
		w.Header().Set("Content-Type", mimeType)
		http.ServeFile(w, r, cleanPath)
	})

	// API: Iniciar busca pela Web
	mux.HandleFunc("/api/search/start", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Metodo nao permitido"})
			return
		}

		activeSearchMutex.Lock()
		if activeSearchStatus.Active {
			activeSearchMutex.Unlock()
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Ja existe uma busca em andamento. Aguarde ou cancele."})
			return
		}

		var req WebSearchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			activeSearchMutex.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Requisicao invalida: " + err.Error()})
			return
		}

		if len(req.Terms) == 0 {
			activeSearchMutex.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Informe ao menos um termo de busca"})
			return
		}

		activeSearchStatus = WebSearchStatus{
			Active:   true,
			Finished: false,
			Type:     req.Type,
		}
		activeSearchStartTime = time.Now()
		activeSearchScanned.Store(0)
		activeSearchMatches.Store(0)
		activeSearchCancel = make(chan struct{})
		activeSearchMutex.Unlock()

		go executeWebSearch(req)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"message": "Busca iniciada com sucesso",
		})
	})

	// API: Status da busca em tempo real
	mux.HandleFunc("/api/search/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		activeSearchMutex.Lock()
		defer activeSearchMutex.Unlock()

		status := activeSearchStatus
		status.ScannedFiles = activeSearchScanned.Load()
		status.MatchesCount = int(activeSearchMatches.Load())
		if status.Active {
			status.ElapsedMs = time.Since(activeSearchStartTime).Milliseconds()
		}

		_ = json.NewEncoder(w).Encode(status)
	})

	// API: Cancelar busca em andamento
	mux.HandleFunc("/api/search/cancel", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		activeSearchMutex.Lock()
		defer activeSearchMutex.Unlock()

		if !activeSearchStatus.Active {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "Nenhuma busca ativa"})
			return
		}

		select {
		case <-activeSearchCancel:
		default:
			close(activeSearchCancel)
		}

		activeSearchStatus.Active = false
		activeSearchStatus.Finished = true
		activeSearchStatus.Error = "Busca cancelada pelo usuario"

		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "Busca cancelada"})
	})

	// API: Download direto do CSV
	mux.HandleFunc("/api/download", func(w http.ResponseWriter, r *http.Request) {
		fileName := r.URL.Query().Get("file")
		if fileName == "" {
			http.Error(w, "Arquivo nao especificado", http.StatusBadRequest)
			return
		}
		targetPath := filepath.Join("resultados_busca", filepath.Base(fileName))
		if !strings.HasSuffix(strings.ToLower(targetPath), ".csv") {
			http.Error(w, "Tipo de arquivo invalido", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(targetPath)))
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		http.ServeFile(w, r, targetPath)
	})

	server := &http.Server{
		Handler: mux,
	}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			LogError("Erro no servidor HTTP do visualizador de tabelas", err)
		}
	}()

	return tableViewerServerURL, nil
}

// executeWebSearch executa a busca solicitada pela interface web em background
func executeWebSearch(req WebSearchRequest) {
	defer func() {
		if r := recover(); r != nil {
			LogRecover(r)
			activeSearchMutex.Lock()
			activeSearchStatus.Active = false
			activeSearchStatus.Finished = true
			activeSearchStatus.Error = fmt.Sprintf("Erro inesperado na busca: %v", r)
			activeSearchMutex.Unlock()
		}
	}()

	matchingMode := req.MatchingMode
	if matchingMode == "" {
		matchingMode = getMatchingMode()
	}

	targetType := TargetFiles
	if req.TargetType == 1 {
		targetType = TargetDirectories
	}

	searchMode := SearchMode(req.Mode)
	if searchMode < ModeName || searchMode > ModeBoth {
		searchMode = ModeBoth
	}

	if req.Type == "db" {
		// Busca Rápida no Banco
		matches := searchFilenamesInDB(req.Terms, req.PosFilter, req.NegFilter, matchingMode)
		if targetType == TargetDirectories {
			matches = convertToUniqueDirectoryMatches(matches)
		}
		sortMatches(matches, SortByFolder)

		activeSearchMatches.Store(int64(len(matches)))
		activeSearchScanned.Store(int64(len(matches)))

		config := SearchConfig{
			BaseDir:        "Banco de Dados",
			Terms:          req.Terms,
			PositiveFilter: req.PosFilter,
			NegativeFilter: req.NegFilter,
			TargetType:     targetType,
			Mode:           ModeName,
			SortMode:       SortByFolder,
			ReportFormat:   "csv",
			MatchingMode:   matchingMode,
		}

		reporter, err := createReport(config)
		var reportFileName string
		if err == nil {
			if len(matches) > 0 {
				_ = reporter.Append(matches)
			} else {
				_ = reporter.WriteNoMatches()
			}
			_ = reporter.Close()
			reportFileName = filepath.Base(reporter.Path)
		}

		activeSearchMutex.Lock()
		activeSearchStatus.Active = false
		activeSearchStatus.Finished = true
		activeSearchStatus.ReportFile = reportFileName
		activeSearchStatus.ElapsedMs = time.Since(activeSearchStartTime).Milliseconds()
		activeSearchMutex.Unlock()
		return
	}

	// Busca no Disco
	baseDir := req.BaseDir
	if baseDir == "" {
		baseDir = "."
	}

	config := SearchConfig{
		BaseDir:        baseDir,
		Mode:           searchMode,
		Terms:          req.Terms,
		PositiveFilter: req.PosFilter,
		NegativeFilter: req.NegFilter,
		TargetType:     targetType,
		SortMode:       SortByFolder,
		ReportFormat:   "csv",
		MatchingMode:   matchingMode,
	}

	result, err := runSearch(config)

	activeSearchMutex.Lock()
	activeSearchStatus.Active = false
	activeSearchStatus.Finished = true
	if err != nil {
		activeSearchStatus.Error = err.Error()
	} else {
		activeSearchScanned.Store(int64(result.ScannedFiles))
		activeSearchMatches.Store(int64(len(result.Matches)))
		activeSearchStatus.ReportFile = filepath.Base(result.ReportPath)
	}
	activeSearchStatus.ElapsedMs = time.Since(activeSearchStartTime).Milliseconds()
	activeSearchMutex.Unlock()
}

// openTableViewer inicia o servidor se necessário e abre o navegador no relatório indicado
func openTableViewer(reportPath string) {
	baseURL, err := startTableViewerServer()
	if err != nil {
		fmt.Printf(Red+"Falha ao iniciar o visualizador web: %v\n"+Reset, err)
		return
	}

	targetURL := baseURL
	if reportPath != "" {
		fileName := filepath.Base(reportPath)
		targetURL = fmt.Sprintf("%s/?file=%s", baseURL, url.QueryEscape(fileName))
	}

	fmt.Printf("\n%sInterface Web aberta no navegador:%s %s%s%s\n", Bold+ThemeGreen, Reset, ThemeCyan, targetURL, Reset)
	openFile(targetURL)
}
