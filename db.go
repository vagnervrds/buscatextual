package main

import (
	"encoding/json"
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
		_, err := tx.CreateBucketIfNotExists([]byte("Files"))
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

func closeDB() {
	if db != nil {
		db.Close()
	}
}
