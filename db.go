package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.etcd.io/bbolt"
)

var db *bbolt.DB

// FileMeta corresponde à estrutura encontrada no buscatextual.db
type FileMeta struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

func initDB() error {
	var err error
	dbPath := "buscatextual.db"
	db, err = bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return err
	}

	return db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte("Files")); err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("DiskConfig"))
		return err
	})
}

// searchFilenamesInDB realiza busca apenas pelos nomes no banco de dados
func searchFilenamesInDB(terms []string) []Match {
	var matches []Match
	_ = db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Files"))
		if b == nil {
			return nil
		}

		return b.ForEach(func(k, v []byte) error {
			path := string(k)
			pathLower := strings.ToLower(path)
			for _, term := range terms {
				if strings.Contains(pathLower, strings.ToLower(term)) {
					matches = append(matches, Match{
						Path: path,
						Kind: "nome (banco)",
					})
					break
				}
			}
			return nil
		})
	})
	return matches
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
