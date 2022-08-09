package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"

	"fmt"
	"html/template"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/osteele/liquid"
	cli "github.com/urfave/cli/v2"
)

var (
	re                 = regexp.MustCompile(`\d%`)
	ErrWrongConstraint = errors.New("wrong constraint")
)

type FactoryTemplate interface {
	Execute(io.Writer, any) error
	Parse(string) FactoryTemplate
	Lookup(string) FactoryTemplate
}

type templateEngines interface {
	*liquid.Engine | *template.Template
}

type Template[T templateEngines] struct {
	t    T
	body []byte
}

func (t *Template[T]) Execute(wr io.Writer, in any) error {
	if v, ok := any(t.t).(*template.Template); ok {
		if err := v.Execute(wr, in); err != nil {
			return err
		}

		return nil
	}

	if v, ok := any(t.t).(*liquid.Engine); ok {

		b, err := v.ParseAndRender(t.body, in.(map[string]interface{}))
		if err != nil {
			return err
		}

		if _, err := io.Copy(wr, bytes.NewReader(b)); err != nil {
			return err
		}
		return nil
	}

	return ErrWrongConstraint
}

func (t *Template[T]) Parse(body string) error {

	_, ok := any(t.t).(*template.Template)
	if ok {
		te := template.New("parser")
		_, err := te.Parse(body)
		if err != nil {
			return err
		}

		t.t = any(te).(T)
		return nil
	}

	in := bytes.NewBuffer(t.body)
	if _, err := io.Copy(in, strings.NewReader(body)); err != nil {
		return err
	}
	t.body = in.Bytes()

	return nil
}

func (t *Template[T]) Lookup(name string) *Template[T] {
	if v, ok := any(t.t).(*template.Template); ok {
		te := v.Lookup(name)
		if te == nil {
			return t
		}

		return NewTemplate(any(te).(T))
	}

	return t
}

func NewTemplate[T templateEngines](t T) *Template[T] {
	return &Template[T]{
		t: t,
	}
}

func NewApp() *cli.App {
	return &cli.App{
		Name: "template-viewer",
		Commands: []*cli.Command{
			{
				Name: "start",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:        "engine",
						DefaultText: "empty or `liquid`; default go template or liquid",
					},
					&cli.StringFlag{
						Name:  "port",
						Value: "8080",
					},
					&cli.StringFlag{
						Name:  "host",
						Value: "0.0.0.0",
					},
				},
				Action: func(ctx *cli.Context) error {

					if ctx.String("engine") == "liquid" {
						te := NewTemplate(liquid.NewEngine())
						return runServer(ctx, te)

					} else {

						te := NewTemplate(template.New("live_template"))
						return runServer(ctx, te)
					}
				},
			},
		},
	}
}

func main() {

	app := NewApp()

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}

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

func runServer[T templateEngines](ctx *cli.Context, templateEngine *Template[T]) error {
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
			serveViewer(w, vt, "", fmt.Errorf("read file: %v", err))

			return
		}

		if err := templateEngine.Parse(bytes.NewBuffer(b).String()); err != nil {
			serveViewer(w, vt, "", fmt.Errorf("parse: %v", err))

			return
		}

		out := bytes.NewBuffer([]byte{})

		data := map[string]interface{}{}

		if err := json.Unmarshal([]byte(jsonData[0]), &data); err != nil {
			serveViewer(w, vt, "", fmt.Errorf("unmarshal: %v", err))

			return
		}

		te := templateEngine.Lookup("content")

		if err := te.Execute(out, data); err != nil {
			serveViewer(w, vt, "", fmt.Errorf("excute: %v", err))

			return
		}

		serveViewer(w, vt, out.String(), nil)

	})
	addr := fmt.Sprintf("%s:%s", ctx.String("host"), ctx.String("port"))

	fmt.Println("listening on:", addr)

	return http.ListenAndServe(addr, srvr)
}
