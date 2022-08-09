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

	_ "embed"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
	"github.com/osteele/liquid"
	cli "github.com/urfave/cli/v2"
)

//go:embed template_viewer.html
var bt []byte

var (
	re                 = regexp.MustCompile(`\d%`)
	ErrWrongConstraint = errors.New("wrong constraint")
)

type FactoryWatcher interface {
	Add(name string) error
	Close() error
	Remove(name string) error
	WatchList() []string
	Events() chan fsnotify.Event
	Errors() chan error
}

type Watcher struct {
	w *fsnotify.Watcher
}

func (w *Watcher) Add(name string) error {

	return w.w.Add(name)
}

func (w *Watcher) Close() error {
	return w.w.Close()
}

func (w *Watcher) Remove(name string) error {
	return w.w.Remove(name)
}

func (w *Watcher) WatchList() []string {
	return w.w.WatchList()
}

func (w *Watcher) Events() chan fsnotify.Event {
	return w.w.Events
}

func (w *Watcher) Errors() chan error {
	return w.w.Errors
}

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
		te := template.New("")
		_, err := te.Parse(body)
		if err != nil {
			return err
		}

		t.t = any(te).(T)
		return nil
	}

	in := bytes.NewBuffer([]byte{})
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
					&cli.StringFlag{
						Name: "base-template",
					},
				},
				Action: func(ctx *cli.Context) error {
					wfs, err := fsnotify.NewWatcher()
					if err != nil {
						return err
					}

					watcher := Watcher{w: wfs}
					if ctx.String("engine") == "liquid" {
						te := NewTemplate(liquid.NewEngine())
						return runServer(ctx, te, watcher)

					} else {
						te := NewTemplate(template.New(""))
						return runServer(ctx, te, watcher)
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

func runServer[T templateEngines](ctx *cli.Context, templateEngine *Template[T], watcher Watcher) error {
	defer watcher.Close()
	srvr := http.NewServeMux()

	srvr.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(watcher, w, r)
	})
	srvr.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		vt := bt
		var err error
		if s := ctx.String("base-template"); s != "" {

			vt, err = os.ReadFile(s)
			if err != nil {
				http.Redirect(w, r, `/?error="`+err.Error()+`"`, http.StatusOK)
				return
			}
		}
		if err := watcher.Add("template_viewer.html"); err != nil {
			log.Fatal(fmt.Errorf("watch template viewer"))
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

		templateFileName := strings.Trim(strings.TrimSpace(filePath[0]), "\"")
		if err := watcher.Add(templateFileName); err != nil {
			log.Fatal(fmt.Errorf("watch template viewer: %s", templateFileName))
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

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// serveWs handles websocket requests from the peer.
func serveWs(watcher Watcher, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	done := make(chan bool)
	go func() {
		defer close(done)

		for {
			select {
			case event, ok := <-watcher.Events():
				if !ok {
					return
				}
				log.Printf("%s %s\n", event.Name, event.Op)
				if err := conn.WriteMessage(websocket.TextMessage, []byte(event.Op.String())); err != nil {
					log.Println(err)
					return
				}
			case err, ok := <-watcher.Errors():
				if !ok {
					return
				}
				log.Println("error:", err)
			}
		}

	}()

}
