package errs

import (
	"errors"
	"net/http"
)

// ErrFileNotExists is a targetted error when image does not exist on storage
var ErrFileNotExists = errors.New("File does not exist")

// ErrKeyNotExists is a targetted error when image does not exist on storage
var ErrKeyNotExists = errors.New("Key does not exist")

var ErrClientHasImage = errors.New("Client already has image")

// Handle returns the proper http code based on an error
func Handle(err error, response http.ResponseWriter) {
	if err == ErrFileNotExists || err == ErrKeyNotExists {
		response.WriteHeader(http.StatusNotFound)
		return
	}
	if err == ErrClientHasImage {
		response.WriteHeader(http.StatusNotModified)
		return
	}
	panic(err)
}
