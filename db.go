package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.etcd.io/bbolt"
)

var db *bbolt.DB

var errDatabaseLocked = fmt.Errorf("banco de dados bloqueado (provavelmente outra instancia esta aberta)")

// FileMeta corresponde à estrutura encontrada no buscatextual.db
type FileMeta struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

func initDB() error {
	var err error
	dbPath := "buscatextual.db"
	db, err = bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 500 * time.Millisecond})
	if err != nil {
		if err == bbolt.ErrTimeout {
			return errDatabaseLocked
		}
		return err
	}

	return db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte("Files")); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte("DiskConfig")); err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("AppConfig"))
		return err
	})
}

// searchFilenamesInDB realiza busca apenas pelos nomes no banco de dados com suporte a filtros positivos e negativos e modo amplo/exato
func searchFilenamesInDB(terms []string, posFilter []string, negFilter []string, mode string) []Match {
	if mode == "" {
		mode = getMatchingMode()
	}

	normTerms := NormalizeTerms(terms, mode)
	normPosFilter := NormalizeTerms(posFilter, mode)
	normNegFilter := NormalizeTerms(negFilter, mode)

	var matches []Match
	_ = db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Files"))
		if b == nil {
			return nil
		}

		return b.ForEach(func(k, v []byte) error {
			path := string(k)
			normPath := NormalizeText(path, mode)

			// Verifica Filtro Negativo
			for _, neg := range normNegFilter {
				if strings.Contains(normPath, neg) {
					return nil
				}
			}

			// Verifica Filtro Positivo
			if len(normPosFilter) > 0 {
				matched := false
				for _, pos := range normPosFilter {
					if strings.Contains(normPath, pos) {
						matched = true
						break
					}
				}
				if !matched {
					return nil
				}
			}

			// Verifica Termos de Busca
			for _, term := range normTerms {
				if strings.Contains(normPath, term) {
					var size int64
					var modTime time.Time
					var meta FileMeta
					if err := json.Unmarshal(v, &meta); err == nil {
						size = meta.Size
						if t, err := time.Parse(time.RFC3339Nano, meta.ModTime); err == nil {
							modTime = t
						}
					}
					matches = append(matches, Match{
						Path:    path,
						Kind:    "nome (banco)",
						Size:    size,
						ModTime: modTime,
					})
					break
				}
			}
			return nil
		})
	})
	return matches
}

func getMatchingMode() string {
	defaultMode := "ampla"
	if db == nil {
		return defaultMode
	}
	var mode string = defaultMode
	_ = db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("AppConfig"))
		if b == nil {
			return nil
		}
		v := b.Get([]byte("matching_mode"))
		if v != nil {
			var val string
			if err := json.Unmarshal(v, &val); err == nil && val != "" {
				valLower := strings.ToLower(strings.TrimSpace(val))
				if valLower == "ampla" || valLower == "exata" {
					mode = valLower
				}
			}
		}
		return nil
	})
	return mode
}

func saveMatchingMode(mode string) error {
	if db == nil {
		return fmt.Errorf("banco de dados nao inicializado")
	}
	modeLower := strings.ToLower(strings.TrimSpace(mode))
	if modeLower != "ampla" && modeLower != "exata" {
		return fmt.Errorf("modo invalido: %s (opcoes validas: 'ampla', 'exata')", mode)
	}
	return db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("AppConfig"))
		if err != nil {
			return err
		}
		buf, err := json.Marshal(modeLower)
		if err != nil {
			return err
		}
		return b.Put([]byte("matching_mode"), buf)
	})
}

func getMaxSearchHistory() int {
	defaultLimit := 10
	if db == nil {
		return defaultLimit
	}
	var limit int = defaultLimit
	_ = db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("AppConfig"))
		if b == nil {
			return nil
		}
		v := b.Get([]byte("max_search_history"))
		if v != nil {
			var val int
			if err := json.Unmarshal(v, &val); err == nil && val >= 0 {
				limit = val
			}
		}
		return nil
	})
	return limit
}

func saveMaxSearchHistory(limit int) error {
	if db == nil {
		return fmt.Errorf("banco de dados nao inicializado")
	}
	return db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("AppConfig"))
		if err != nil {
			return err
		}
		buf, err := json.Marshal(limit)
		if err != nil {
			return err
		}
		return b.Put([]byte("max_search_history"), buf)
	})
}

func getReportFormat() string {
	defaultFormat := "csv"
	if db == nil {
		return defaultFormat
	}
	var format string = defaultFormat
	_ = db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("AppConfig"))
		if b == nil {
			return nil
		}
		v := b.Get([]byte("report_format"))
		if v != nil {
			var val string
			if err := json.Unmarshal(v, &val); err == nil && val != "" {
				valLower := strings.ToLower(strings.TrimSpace(val))
				if valLower == "csv" || valLower == "json" || valLower == "toml" {
					format = valLower
				}
			}
		}
		return nil
	})
	return format
}

func saveReportFormat(format string) error {
	if db == nil {
		return fmt.Errorf("banco de dados nao inicializado")
	}
	formatLower := strings.ToLower(strings.TrimSpace(format))
	if formatLower != "csv" && formatLower != "json" && formatLower != "toml" {
		return fmt.Errorf("formato invalido: %s", format)
	}
	return db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("AppConfig"))
		if err != nil {
			return err
		}
		buf, err := json.Marshal(formatLower)
		if err != nil {
			return err
		}
		return b.Put([]byte("report_format"), buf)
	})
}

// putIndexHelper é uma função auxiliar para salvar metadados em um bucket aberto
func putIndexHelper(b *bbolt.Bucket, path string, size int64, modTimeStr string) error {
	v := b.Get([]byte(path))
	if v != nil {
		var meta FileMeta
		if err := json.Unmarshal(v, &meta); err == nil {
			if meta.ModTime == modTimeStr && meta.Size == size {
				return nil
			}
		}
	}

	meta := FileMeta{
		Path:    path,
		Size:    size,
		ModTime: modTimeStr,
	}

	buf, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	return b.Put([]byte(path), buf)
}

func getDiskOptimalThreads(diskID string) int {
	var threads int
	_ = db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("DiskConfig"))
		if b == nil {
			return nil
		}
		v := b.Get([]byte(diskID))
		if v != nil {
			var val int
			if err := json.Unmarshal(v, &val); err == nil {
				threads = val
			}
		}
		return nil
	})
	return threads
}

func saveDiskOptimalThreads(diskID string, threads int) error {
	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("DiskConfig"))
		if b == nil {
			return fmt.Errorf("bucket DiskConfig nao existe")
		}
		buf, err := json.Marshal(threads)
		if err != nil {
			return err
		}
		return b.Put([]byte(diskID), buf)
	})
}

func closeDB() {
	if db != nil {
		db.Close()
	}
}

func resetOptimalThreads() error {
	return db.Update(func(tx *bbolt.Tx) error {
		err := tx.DeleteBucket([]byte("DiskConfig"))
		if err != nil && err != bbolt.ErrBucketNotFound {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("DiskConfig"))
		return err
	})
}

