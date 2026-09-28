//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// launchNativeFolderViewer abre o gerenciador de arquivos padrão no macOS / Linux
func launchNativeFolderViewer(targetPath string) error {
	cleanPath := filepath.Clean(targetPath)
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", cleanPath).Start()
	default:
		dir := cleanPath
		if fi, err := os.Stat(cleanPath); err == nil && !fi.IsDir() {
			dir = filepath.Dir(cleanPath)
		} else if _, perr := os.Stat(filepath.Dir(cleanPath)); perr == nil {
			dir = filepath.Dir(cleanPath)
		} else {
			return fmt.Errorf("caminho não encontrado no sistema: %s", cleanPath)
		}
		return exec.Command("xdg-open", dir).Start()
	}
}
