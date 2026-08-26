package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultMaxFileSize     int64 = 1 * 1024 * 1024 // 1 MB
	defaultMaxRotatedFiles int   = 3               // Até 3 arquivos rotacionados
	defaultLogChannelCap   int   = 4096            // Buffer assíncrono para zero overhead
)

type ErrorLogger struct {
	logDir          string
	activeLogFile   string
	maxFileSize     int64
	maxRotatedFiles int

	file        *os.File
	currentSize int64

	msgChan chan string
	wg      sync.WaitGroup
	closed  atomic.Bool
	mu      sync.Mutex
}

var globalLogger *ErrorLogger

// InitLogger inicializa o logger global assíncrono apontando para a pasta especificada (default: "log")
func InitLogger(dir string) error {
	if dir == "" {
		dir = "log"
	}

	logger, err := newErrorLogger(dir, defaultMaxFileSize, defaultMaxRotatedFiles)
	if err != nil {
		return err
	}

	globalLogger = logger
	return nil
}

func newErrorLogger(dir string, maxFileSize int64, maxRotatedFiles int) (*ErrorLogger, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("falha ao criar pasta de logs '%s': %w", dir, err)
	}

	activePath := filepath.Join(dir, "log.log")
	file, err := os.OpenFile(activePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("falha ao abrir arquivo de log '%s': %w", activePath, err)
	}

	stat, err := file.Stat()
	var currentSize int64
	if err == nil {
		currentSize = stat.Size()
	}

	l := &ErrorLogger{
		logDir:          dir,
		activeLogFile:   activePath,
		maxFileSize:     maxFileSize,
		maxRotatedFiles: maxRotatedFiles,
		file:            file,
		currentSize:     currentSize,
		msgChan:         make(chan string, defaultLogChannelCap),
	}

	l.wg.Add(1)
	go l.worker()

	return l, nil
}

// LogError registra um erro assincronamente com o stack trace completo
func LogError(msg string, err error) {
	if globalLogger == nil || globalLogger.closed.Load() {
		return
	}
	globalLogger.log(msg, err, debug.Stack())
}

// LogErrorf registra um erro formatado assincronamente com o stack trace completo
func LogErrorf(err error, format string, args ...any) {
	if globalLogger == nil || globalLogger.closed.Load() {
		return
	}
	msg := fmt.Sprintf(format, args...)
	globalLogger.log(msg, err, debug.Stack())
}

// LogRecover captura um panic assincronamente com stack trace
func LogRecover(r any) {
	if globalLogger == nil || globalLogger.closed.Load() {
		return
	}
	var err error
	if e, ok := r.(error); ok {
		err = e
	} else {
		err = fmt.Errorf("%v", r)
	}
	globalLogger.log("PANIC INESPERADO RECUPERADO", err, debug.Stack())
}

func (l *ErrorLogger) log(msg string, err error, stack []byte) {
	if l.closed.Load() {
		return
	}

	now := time.Now()
	timestamp := now.Format("2006-01-02 15:04:05.000")

	errStr := "<nil>"
	if err != nil {
		errStr = err.Error()
	}

	var sb strings.Builder
	sb.WriteString("================================================================================\n")
	sb.WriteString(fmt.Sprintf("[%s] [ERROR] %s\n", timestamp, msg))
	sb.WriteString(fmt.Sprintf("Detalhes do Erro: %s\n", errStr))
	if len(stack) > 0 {
		sb.WriteString("Stack Trace Completo:\n")
		sb.Write(stack)
		if !strings.HasSuffix(sb.String(), "\n") {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("================================================================================\n\n")

	entry := sb.String()

	// Envio não-bloqueante para garantir velocidade máxima e zero overhead nas buscas/indexação
	select {
	case l.msgChan <- entry:
	default:
		// Se o canal estiver saturado, evita travar threads de busca/indexação
	}
}

func (l *ErrorLogger) worker() {
	defer l.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			_ = r
		}
	}()

	for entry := range l.msgChan {
		l.writeEntry(entry)
	}
}

func (l *ErrorLogger) writeEntry(entry string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entryBytes := []byte(entry)
	entryLen := int64(len(entryBytes))

	// Verifica se ultrapassou o limite de tamanho para rotação
	if l.file != nil && (l.currentSize+entryLen >= l.maxFileSize) {
		l.rotate()
	}

	if l.file == nil {
		var err error
		l.file, err = os.OpenFile(l.activeLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return
		}
		if stat, err := l.file.Stat(); err == nil {
			l.currentSize = stat.Size()
		} else {
			l.currentSize = 0
		}
	}

	n, err := l.file.Write(entryBytes)
	if err == nil {
		l.currentSize += int64(n)
	}
}

func (l *ErrorLogger) rotate() {
	if l.file != nil {
		_ = l.file.Sync()
		_ = l.file.Close()
		l.file = nil
	}

	// Formata o nome do arquivo rotacionado: log_YYYY-MM-DD_HH-mm-ss-SSS.log
	now := time.Now()
	timeFormatted := now.Format("2006-01-02_15-04-05-000")
	rotatedName := fmt.Sprintf("log_%s.log", timeFormatted)
	rotatedPath := filepath.Join(l.logDir, rotatedName)

	// Garante nome único se houver rotações no mesmo milissegundo
	counter := 1
	for {
		if _, err := os.Stat(rotatedPath); os.IsNotExist(err) {
			break
		}
		rotatedPath = filepath.Join(l.logDir, fmt.Sprintf("log_%s_%d.log", timeFormatted, counter))
		counter++
	}

	// Renomeia o log.log atual para o arquivo rotacionado
	if _, err := os.Stat(l.activeLogFile); err == nil {
		_ = os.Rename(l.activeLogFile, rotatedPath)
	}

	// Limpa arquivos rotacionados mais antigos caso exceda maxRotatedFiles (3)
	l.cleanupOldRotatedLogs()

	// Abre um novo log.log limpo
	newFile, err := os.OpenFile(l.activeLogFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err == nil {
		l.file = newFile
		l.currentSize = 0
	}
}

func (l *ErrorLogger) cleanupOldRotatedLogs() {
	entries, err := os.ReadDir(l.logDir)
	if err != nil {
		return
	}

	type fileWithTime struct {
		name    string
		path    string
		modTime time.Time
	}

	var rotatedFiles []fileWithTime
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Reconhece apenas arquivos rotacionados no formato log_*.log
		if strings.HasPrefix(name, "log_") && strings.HasSuffix(name, ".log") {
			info, err := entry.Info()
			if err == nil {
				rotatedFiles = append(rotatedFiles, fileWithTime{
					name:    name,
					path:    filepath.Join(l.logDir, name),
					modTime: info.ModTime(),
				})
			}
		}
	}

	// Se houver mais que o limite configurado (3 arquivos)
	if len(rotatedFiles) > l.maxRotatedFiles {
		// Ordena os arquivos do mais antigo para o mais recente (pelo nome e pela data de modificação)
		sort.Slice(rotatedFiles, func(i, j int) bool {
			if rotatedFiles[i].name != rotatedFiles[j].name {
				return rotatedFiles[i].name < rotatedFiles[j].name
			}
			return rotatedFiles[i].modTime.Before(rotatedFiles[j].modTime)
		})

		excess := len(rotatedFiles) - l.maxRotatedFiles
		for i := 0; i < excess; i++ {
			_ = os.Remove(rotatedFiles[i].path)
		}
	}
}

// CloseLogger finaliza o worker e descarrega todos os logs pendentes
func CloseLogger() {
	if globalLogger != nil {
		globalLogger.Close()
		globalLogger = nil
	}
}

func (l *ErrorLogger) Close() {
	if l.closed.CompareAndSwap(false, true) {
		close(l.msgChan)
		l.wg.Wait()

		l.mu.Lock()
		defer l.mu.Unlock()
		if l.file != nil {
			_ = l.file.Sync()
			_ = l.file.Close()
			l.file = nil
		}
	}
}
