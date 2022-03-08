package main

import (
	"crypto/md5"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/BurntSushi/toml"
	_ "modernc.org/sqlite"
)

type BasedPaste struct {
	Url          string
	Host         string
	Port         int
	IndexPath    string
	DbPath       string
	UploadsDir   string
	MaxFileBytes int64
	MaxFileMiB   int64
	RequireAuth  bool
}

func (bp *BasedPaste) router(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		switch r.Method {
		case "GET":
			t, err := template.ParseFiles(bp.IndexPath)
			if err != nil {
				log.Fatal(err)
				http.Error(w, "500 Internal Server Error", 500)
			}
			t.Execute(w, bp)
		case "POST":
			bp.add(w, r)
		default:
			http.Error(w, "403 Method Not Allowed", 403)
		}
	case "/robots.txt":
		switch r.Method {
		case "GET":
			fmt.Fprintf(w, "User-agent: *\nDisallow: /\n")

		default:
			http.Error(w, "403 Method Not Allowed", 403)
		}
	default:
		switch r.Method {
		case "GET":
			bp.get(w, r)
		default:
			http.Error(w, "403 Method Not Allowed", 403)
		}
	}
}

func (bp *BasedPaste) add(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(0)
	if err != nil {
		http.Error(w, "400 Bad Request", 400)
		return
	}

	if bp.RequireAuth {
		key := r.FormValue("auth")
		valid, err := bp.validAuth(key)
		if err != nil {
			log.Println(err)
			http.Error(w, "500 Internal Server Error", 500)
			return
		}
		if !valid {
			log.Printf("Failed authentication with key=\"%s\"\n", key)
			http.Error(w, "401 Unauthorized", 401)
			return
		}
	}

	url := r.FormValue("url")
	if url != "" {
		key, err := bp.addUrl(url)
		if err != nil {
			log.Println(err)
			http.Error(w, "500 Internal Server Error", 500)
			return
		}

		fmt.Fprintf(w, "%s/%s", bp.Url, key)
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "400 Bad Request", 400)
		return
	}

	if fileHeader.Size > bp.MaxFileBytes {
		http.Error(w, "413 Payload Too Large", 413)
		return
	}

	key, err := bp.addFile(file)
	if err != nil {
		log.Println(err)
		http.Error(w, "500 Internal Server Error", 500)
		return
	}

	fmt.Fprintf(w, "%s/%s", bp.Url, key)
}

func (bp *BasedPaste) addUrl(url string) (string, error) {
	db, err := sql.Open("sqlite", bp.DbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()

	var key string
	row := db.QueryRow(`SELECT key FROM basedpaste WHERE value=?`, url)
	err = row.Scan(&key)
	switch {
	case err == nil:
		return key, nil
	case err != sql.ErrNoRows:
		return "", err
	}

	key = ""
	digest := hashUrl(url)
	for i := 1; i <= len(digest); i++ {
		key = digest[:i]
		var count int
		row = db.QueryRow(`SELECT COUNT(key) FROM basedpaste WHERE key=?`, key)
		err = row.Scan(&count)
		if err != nil {
			return "", err
		}
		if count == 0 {
			break
		}

	}

	_, err = db.Exec(`INSERT INTO basedpaste (time, type, key, value)
		VALUES (?, ?, ?, ?)`, time.Now().Unix(), "url", key, url)

	return key, err
}

func (bp *BasedPaste) addFile(file multipart.File) (string, error) {
	_, err := file.Seek(0, io.SeekStart)
	if err != nil {
		return "", err
	}
	digest := hashFile(file)

	db, err := sql.Open("sqlite", bp.DbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()

	var key string
	row := db.QueryRow(`SELECT key FROM basedpaste WHERE value=?`, digest)
	err = row.Scan(&key)
	switch {
	case err == nil:
		return key, nil
	case err != sql.ErrNoRows:
		return "", err
	}

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return "", err
	}
	fileSaved, err := os.Create(filepath.Join(bp.UploadsDir, digest))
	if err != nil {
		return "", err
	}
	defer fileSaved.Close()
	if _, err := io.Copy(fileSaved, file); err != nil {
		return "", err
	}

	key = ""
	for i := 1; i <= len(digest); i++ {
		key = digest[:i]
		var count int
		row = db.QueryRow(`SELECT COUNT(key) FROM basedpaste WHERE key=?`, key)
		err = row.Scan(&count)
		if err != nil {
			return "", err
		}
		if count == 0 {
			break
		}

	}

	_, err = db.Exec(`INSERT INTO basedpaste (time, type, key, value)
		VALUES (?, ?, ?, ?)`, time.Now().Unix(), "file", key, digest)

	return key, err
}

func (bp *BasedPaste) get(w http.ResponseWriter, r *http.Request) {
	db, err := sql.Open("sqlite", bp.DbPath)
	if err != nil {
		log.Println(err)
		http.Error(w, "500 Internal Server Error", 500)
		return
	}
	defer db.Close()

	key := r.URL.Path[1:]
	var _type string
	var value string
	row := db.QueryRow(`SELECT type, value FROM basedpaste WHERE key=?`, key)
	err = row.Scan(&_type, &value)
	switch {
	case err == sql.ErrNoRows:
		http.Error(w, "404 Not Found", 404)
		return
	case err != nil:
		log.Println(err)
		http.Error(w, "500 Internal Server Error", 500)
		return
	}

	switch {
	case _type == "url":
		http.Redirect(w, r, value, 307)
	case _type == "file":
		http.ServeFile(w, r, filepath.Join(bp.UploadsDir, value))
	default:
		log.Printf("key %s has unexpected type %s\n", key, _type)
		http.Error(w, "500 Internal Server Error", 500)
		return
	}
}

func (bp *BasedPaste) dbInit() error {
	db, err := sql.Open("sqlite", bp.DbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS basedpaste (
				id	INTEGER PRIMARY KEY AUTOINCREMENT,
				time	INTEGER NOT NULL,
				type	TEXT NOT NULL,
				key	TEXT NOT NULL UNIQUE,
				value	TEXT NOT NULL
					)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS auth (
				id	INTEGER PRIMARY KEY AUTOINCREMENT,
				key	TEXT NOT NULL UNIQUE,
				comment	TEXT
					)`)
	if err != nil {
		return err
	}

	return nil
}

func (bp *BasedPaste) validAuth(key string) (bool, error) {
	if key == "" {
		return false, nil
	}

	db, err := sql.Open("sqlite", bp.DbPath)
	if err != nil {
		return false, err
	}
	defer db.Close()

	var count int
	row := db.QueryRow(`SELECT COUNT(key) FROM auth WHERE key=?`, key)
	err = row.Scan(&count)
	if err != nil {
		return false, err
	}
	valid := !(count == 0)

	return valid, nil
}

func hashUrl(url string) string {
	digest := fmt.Sprintf("%x", md5.Sum([]byte(url)))
	return digest
}

func hashFile(file multipart.File) string {
	h := md5.New()
	if _, err := io.Copy(h, file); err != nil {
		log.Fatal(err)
	}

	digest := fmt.Sprintf("%x", h.Sum(nil))
	return digest
}

func newBasedPaste(configPath string) (*BasedPaste, error) {
	var bp BasedPaste
	_, err := toml.DecodeFile(configPath, &bp)

	return &bp, err
}

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	configDefault := filepath.Join(homeDir, ".config/basedpaste/config.toml")
	configFlag := flag.String("c", configDefault, "Specify alternative config file.")
	flag.Parse()
	configPath := *configFlag

	bp, err := newBasedPaste(configPath)
	if err != nil {
		log.Fatal(err)
	}

	if bp.Url == "" {
		bp.Url = "http://example.com"
		log.Printf("Url field not specified, using %s\n", bp.Url)
	}
	if bp.Host == "" {
		bp.Host = "localhost"
		log.Printf("Host field not specified, using %s\n", bp.Host)
	}
	if bp.Port == 0 {
		bp.Port = 8080
		log.Printf("Port field not specified, using %s\n", bp.Port)
	}
	if bp.IndexPath == "" {
		bp.IndexPath = filepath.Join(homeDir, ".local/share/basedpaste/index.html")
		log.Printf("IndexPath field not specified, using %s\n", bp.IndexPath)
	}
	if bp.DbPath == "" {
		bp.DbPath = filepath.Join(homeDir, ".local/share/basedpaste/basedpaste.db")
		log.Printf("DbPath field not specified, using %s\n", bp.DbPath)
	}
	if bp.UploadsDir == "" {
		bp.UploadsDir = filepath.Join(homeDir, ".local/share/basedpaste/uploads")
		log.Printf("UploadsDir field not specified, using %s\n", bp.UploadsDir)
	}
	if bp.MaxFileBytes == 0 {
		bp.MaxFileBytes = 33554432
		log.Printf("MaxFileBytes field not specified, using %s\n", bp.MaxFileBytes)
	}
	bp.MaxFileMiB = bp.MaxFileBytes / (1 << 20)

	err = os.MkdirAll(filepath.Dir(bp.IndexPath), 0755)
	if err != nil {
		log.Fatal(err)
	}

	err = os.MkdirAll(filepath.Dir(bp.DbPath), 0755)
	if err != nil {
		log.Fatal(err)
	}

	err = bp.dbInit()
	if err != nil {
		log.Fatal(err)
	}

	err = os.MkdirAll(bp.UploadsDir, 0755)
	if err != nil {
		log.Fatal(err)
	}

	rand.Seed(time.Now().UnixNano())

	http.HandleFunc("/", bp.router)
	addr := fmt.Sprintf("%s:%d", bp.Host, bp.Port)
	log.Fatal(http.ListenAndServe(addr, nil))
}
