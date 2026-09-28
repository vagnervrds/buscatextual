//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// launchNativeFolderViewer abre o Windows Explorer garantindo que argumentos com espaços
// não quebrem o parâmetro /select, evitando que o Explorer abra indevidamente a pasta "Meus Documentos".
func launchNativeFolderViewer(targetPath string) error {
	cleanPath := filepath.Clean(targetPath)
	fi, err := os.Stat(cleanPath)

	var cmdLine string
	if err == nil && fi.IsDir() {
		// É diretório existente: abre o Explorer na pasta
		cmdLine = fmt.Sprintf(`explorer.exe "%s"`, cleanPath)
	} else if err == nil && !fi.IsDir() {
		// É arquivo existente: abre o Explorer e destaca o arquivo (/select,"<caminho>")
		cmdLine = fmt.Sprintf(`explorer.exe /select,"%s"`, cleanPath)
	} else {
		// O arquivo específico não foi encontrado, mas verifica se o diretório pai existe
		parentDir := filepath.Dir(cleanPath)
		if pfi, perr := os.Stat(parentDir); perr == nil && pfi.IsDir() {
			cmdLine = fmt.Sprintf(`explorer.exe "%s"`, parentDir)
		} else {
			return fmt.Errorf("caminho não encontrado no sistema: %s", cleanPath)
		}
	}

	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdLine}
	return cmd.Start()
}
