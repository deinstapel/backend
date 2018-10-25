package qbin

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"

	"crypto/sha256"
)

const MaxFilesize = 1024 * 1024 // 1MB

// Document specifies the content and metadata of a piece of code that is hosted on qbin.
type Document struct {
	// ID is set on Store()
	ID      string
	Content string
	Syntax  string
	// Upload is set on Store()
	Upload     time.Time
	Expiration time.Time
	Views      int
	Custom     string
}

// Store a document object in the database.
func Store(document *Document) error {
	// Generate a name that doesn't exist yet
	name, err := GenerateSafeName()
	if err != nil {
		return err
	}
	document.ID = name

	// Round the timestamps on the object. Won't affect the database, but we want consistency.
	document.Upload = time.Now().Round(time.Second)
	document.Expiration = document.Expiration.Round(time.Second)

	// Normalize new lines
	document.Content = strings.Trim(strings.Replace(strings.Replace(document.Content, "\r\n", "\n", -1), "\r", "\n", -1), "\n") + "\n"

	// Don't accept binary files
	if strings.Contains(document.Content, "\x00") {
		return errors.New("file contains 0x00 bytes")
	}

	contentHighlighted := ""
	if document.Custom == "" {
		if document.Syntax == "none" {
			document.Syntax = ""
		}
		contentHighlighted, err = Highlight(document.Content, document.Syntax)
		if err != nil {
			Log.Warningf("Skipped syntax highlighting for the following reason: %s", err)
		}
	} else {
		contentHighlighted = EscapeHTML(document.Content)
	}

	// Filter content for spam
	err = FilterSpam(document, &contentHighlighted)
	if err != nil {
		Log.Warningf("Spam filter hit for document: %s", err)
		return errors.New("spam: " + err.Error())
	}

	var expiration interface{}
	if (document.Expiration != time.Time{}) {
		expiration = document.Expiration.UTC().Format("2006-01-02 15:04:05")
	}

	// Server-Side Encryption
	key, err := scrypt.Key([]byte(document.ID), []byte(document.Upload.UTC().Format("2006-01-02 15:04:05")), 16384, 8, 1, 24)
	if err != nil {
		Log.Errorf("Invalid script parameters: %s", err)
	}
	data, err := encrypt([]byte(contentHighlighted), key)
	if err != nil {
		Log.Errorf("AES error: %s", err)
		return err
	}
	databaseID := sha256.Sum256([]byte(document.ID))

	// Write the document to the database
	_, err = db.Exec(
		"INSERT INTO documents (id, content, custom, syntax, upload, expiration, views) VALUES (?, ?, ?, ?, ?, ?, ?)",
		hex.EncodeToString(databaseID[:]),
		string(data),
		document.Custom,
		document.Syntax,
		document.Upload.UTC().Format("2006-01-02 15:04:05"),
		expiration,
		document.Views)
	if err != nil {
		return err
	}
	return nil
}

// Request a document from the database by its ID.
func Request(id string, raw bool) (Document, error) {
	doc := Document{ID: id}
	var views int
	var upload, expiration sql.NullString
	databaseID := sha256.Sum256([]byte(id))
	err := db.QueryRow("SELECT content, custom, syntax, upload, expiration, views FROM documents WHERE id = ?", hex.EncodeToString(databaseID[:])).
		Scan(&doc.Content, &doc.Custom, &doc.Syntax, &upload, &expiration, &views)
	if err != nil {
		if err.Error() != "sql: no rows in result set" {
			Log.Warningf("Error retrieving document: %s", err)
		}
		return Document{}, err
	}

	go db.Exec("UPDATE documents SET views = views + 1 WHERE id = ?", hex.EncodeToString(databaseID[:]))
	doc.Views = views

	doc.Upload, _ = time.Parse("2006-01-02 15:04:05", upload.String)

	// Server-Side Decryption
	key, err := scrypt.Key([]byte(id), []byte(doc.Upload.UTC().Format("2006-01-02 15:04:05")), 16384, 8, 1, 24)
	if err != nil {
		Log.Errorf("Invalid script parameters: %s", err)
		return Document{}, err
	}
	data, err := decrypt([]byte(doc.Content), key)
	if err != nil && !(err.Error() == "cipher: message authentication failed" && !strings.Contains(doc.Content, "\000")) {
		Log.Errorf("AES error: %s", err)
		return Document{}, err
	} else if err == nil {
		doc.Content = string(data)
	}

	if expiration.Valid {
		doc.Expiration, err = time.Parse("2006-01-02 15:04:05", expiration.String)
		if doc.Expiration.Before(time.Unix(0, 1)) {
			if doc.Views > 0 {
				// Volatile document
				_, err = db.Exec("DELETE FROM documents WHERE id = ?", id)
				if err != nil {
					Log.Errorf("Couldn't delete volatile document: %s", err)
				}
			}
		} else {
			if err != nil {
				return Document{}, err
			}
			if doc.Expiration.Before(time.Now()) {
				return Document{}, errors.New("the document has expired")
			}
		}
	}

	if raw {
		doc.Content = StripHTML(doc.Content)
	}
	return doc, nil
}
