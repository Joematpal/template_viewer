package main

import (
	"bytes"
	"encoding/json"
	"errors"

	"fmt"
	"html/template"
	"net/http"
	"os"
	"regexp"
	"strings"
)

var (
	re = regexp.MustCompile(`\d%`)
)

type accounts struct {
	Accounts []account
	ToEmail  string
}
type account struct {
	Username string
	URL      string
}

func main() {
	srvr := http.NewServeMux()

	srvr.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {

		vt, err := os.ReadFile("template_viewer.html")
		if err != nil {
			http.Redirect(w, r, `/?error="`+err.Error()+`"`, http.StatusOK)
			return
		}

		query := r.URL.Query()

		filePath, ok := query["filePath"]
		if !ok {
			serveViewer(w, vt, "", errors.New("please provide filePath in query params"))

			return
		}

		jsonData, ok := query["data"]
		if !ok {
			serveViewer(w, vt, "", errors.New("please provide data in query params"))

			return
		}

		b, err := os.ReadFile(strings.Trim(strings.TrimSpace(filePath[0]), "\""))
		if err != nil {
			serveViewer(w, vt, "", err)

			return
		}

		tmplt, err := template.New("live_template").Parse(string(b))
		if err != nil {
			serveViewer(w, vt, "", err)

			return
		}

		out := bytes.NewBuffer([]byte{})

		data := map[string]interface{}{}

		if err := json.Unmarshal([]byte(jsonData[0]), &data); err != nil {
			serveViewer(w, vt, "", err)

			return
		}

		if err := tmplt.Lookup("content").Execute(out, data); err != nil {
			serveViewer(w, vt, "", err)

			return
		}

		serveViewer(w, vt, out.String(), nil)

	})

	http.ListenAndServe(":8080", srvr)
}

func serveViewer(w http.ResponseWriter, viewer []byte, out string, err error) {
	outTemplate := re.ReplaceAllFunc(viewer, percentEscape)

	errorBox := ""
	if err != nil {
		errorBox = fmt.Sprintf(`<p id="error-box" style="color: red; border: red solid 2px;">%s</p>`, err.Error())
	}

	w.Write([]byte(fmt.Sprintf(string(outTemplate), errorBox, out)))
}

func percentEscape(b []byte) []byte {
	return []byte(string(b) + "%")
}
